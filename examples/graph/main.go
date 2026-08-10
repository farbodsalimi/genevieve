package main

import (
	"context"
	"runtime/debug"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	log "github.com/sirupsen/logrus"

	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/graphllm"
	"github.com/farbodsalimi/genevieve/pkg/llm"
	"github.com/farbodsalimi/genevieve/pkg/providers/openai"
)

const (
	app          = "genevieve"
	maxRevisions = 3
)

type Config struct {
	Debug        bool   `required:"false" default:"false"`
	OpenAIAPIKey string `required:"true"`
}

// State is the workflow's typed state — no map[string]any in sight.
type State struct {
	Topic     string
	Draft     string
	Critiques []string
	Revisions int
	Published string
}

// Update is a partial change produced by a node.
type Update struct {
	Draft     string
	Critique  string
	Published string
	Revised   bool
}

func reducer() graph.Reducer[State, Update] {
	return graph.ReducerFunc[State, Update](func(s State, u Update) (State, error) {
		out := s
		// copy-on-write the critique slice so prior state is never mutated
		out.Critiques = append([]string(nil), s.Critiques...)
		if u.Draft != "" {
			out.Draft = u.Draft
		}
		if u.Critique != "" {
			out.Critiques = append(out.Critiques, u.Critique)
		}
		if u.Revised {
			out.Revisions = s.Revisions + 1
		}
		if u.Published != "" {
			out.Published = u.Published
		}
		return out, nil
	})
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic occurred:", r)
			log.Error("Stack trace:\n", string(debug.Stack()))
		}
	}()

	godotenv.Load()

	var config Config
	if err := envconfig.Process(app, &config); err != nil {
		log.Fatal(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	openaiClient, err := openai.NewClient(ctx, config.OpenAIAPIKey, llm.WithModel("gpt-4o"))
	if err != nil {
		log.Fatalf("openai client: %v", err)
	}
	router := llm.NewRouter()
	err = router.Register(openaiClient)
	if err != nil {
		log.Fatalf("register: %v", err)
	}
	provider := openaiClient.Name()

	// draft: write or revise the piece given the topic and any critiques.
	draft := graphllm.LLMNode(
		router,
		provider,
		func(s State) string {
			var b strings.Builder
			b.WriteString("Write a short paragraph about: " + s.Topic + ".\n")
			if len(s.Critiques) > 0 {
				b.WriteString("Revise the previous draft addressing this critique:\n")
				b.WriteString(s.Critiques[len(s.Critiques)-1] + "\n")
				b.WriteString("Previous draft:\n" + s.Draft + "\n")
			}
			return b.String()
		},
		func(resp string) Update { return Update{Draft: resp, Revised: true} },
	)

	// critique: judge the draft. Reply must start with APPROVE or REVISE.
	critique := graphllm.LLMNode(
		router, provider,
		func(s State) string {
			return "Critique this paragraph. If it is good enough to publish, reply " +
				"beginning with the single word APPROVE. Otherwise reply beginning with " +
				"REVISE and one concrete suggestion.\n\nParagraph:\n" + s.Draft
		},
		func(resp string) Update { return Update{Critique: resp} },
	)

	// publish: finalize.
	publish := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
		return Update{Published: s.Draft}, nil
	})

	// route: after a critique, loop back to draft or move to publish.
	route := func(ctx context.Context, s State) (graph.NodeID, error) {
		if len(s.Critiques) == 0 {
			return "publish", nil
		}
		last := strings.ToUpper(strings.TrimSpace(s.Critiques[len(s.Critiques)-1]))
		approved := strings.HasPrefix(last, "APPROVE")
		if approved || s.Revisions >= maxRevisions {
			return "publish", nil
		}
		return "draft", nil
	}

	runner, err := graph.NewBuilder(reducer()).
		AddNode("draft", draft).
		AddNode("critique", critique).
		AddNode("publish", publish).
		AddEdge("draft", "critique").
		AddConditionalEdge("critique", route).
		SetEntryPoint("draft").
		SetTerminal("publish").
		Compile(graph.WithRecursionLimit(2*maxRevisions + 2))
	if err != nil {
		log.Fatalf("compile: %v", err)
	}

	final, err := runner.Run(
		ctx,
		State{Topic: "why bounded loops beat unbounded agent while-loops"},
	)
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	log.Infof("Revisions: %d", final.Revisions)
	for i, c := range final.Critiques {
		log.Infof("Critique %d: %s", i+1, c)
	}
	log.Infof("Published:\n%s", final.Published)
}
