// Command graph demonstrates a bounded draft→critique→publish loop on the
// generic graph engine. This file is the topology; the supporting pieces are
// split out so the shape of the workflow is visible in one screen:
//
//	config.go    provider/router setup
//	workflow.go  State, Update, and the reducer that merges them
//	prompts.go   prompt construction and the APPROVE/REVISE check
package main

import (
	"context"
	"runtime/debug"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/graph/nodes"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic occurred:", r)
			log.Error("Stack trace:\n", string(debug.Stack()))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	router, provider, err := newProvider(ctx)
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	// draft: write the piece, or revise it against the newest critique.
	draft := nodes.LLMNode(
		router, provider,
		draftPrompt,
		func(resp string) Update { return Update{Draft: resp, Revised: true} },
	)

	// critique: judge the draft. Reply must start with APPROVE or REVISE.
	critique := nodes.LLMNode(
		router, provider,
		critiquePrompt,
		func(resp string) Update { return Update{Critique: resp} },
	)

	// publish: finalize the accepted draft.
	publish := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
		return Update{Published: s.Draft}, nil
	})

	// route: after a critique, loop back to draft or move on to publish.
	route := func(ctx context.Context, s State) (graph.NodeID, error) {
		if approved(s.lastCritique()) || s.Revisions >= maxRevisions {
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
		// Each revision costs a draft and a critique super-step; the +2 covers
		// the final publish step and one step of slack.
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
