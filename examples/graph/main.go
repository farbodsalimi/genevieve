// Command graph demonstrates an intake-gated, parallel, failure-tolerant
// research workflow with agent tool loops and structured synthesis.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/graph/nodes"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	router, provider, err := newProvider(ctx)
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	researcher := agent.NewAgent(router)
	if err := researcher.RegisterTool(evidenceLookupTool{}); err != nil {
		log.Fatalf("register evidence tool: %v", err)
	}
	synthesizer := agent.NewAgent(router)
	if err := synthesizer.RegisterTool(reportObservationTool{}); err != nil {
		log.Fatalf("register report tool: %v", err)
	}

	intake := nodes.LLMNode(router, provider, intakePrompt, parseIntake)
	specialist := func(role string) graph.Node[State, Update] {
		return graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
			result, err := researcher.Run(ctx, agent.RunRequest{
				Provider:  provider,
				Messages:  []llm.Message{{Role: llm.RoleUser, Content: specialistPrompt(role, s)}},
				MaxTurns:  4,
				MaxTokens: 700,
				Budget:    agent.Budget{MaxTotalTokens: 2_500, WrapUpReserve: 250},
			})
			if err != nil {
				return Update{}, err
			}
			return Update{
				Report: &SpecialistReport{Specialist: role, Report: result.Output},
				Usage:  result.Usage,
			}, nil
		})
	}

	synthesize := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
		var streamed string
		result, err := synthesizer.Run(ctx, agent.RunRequest{
			Provider:       provider,
			Messages:       []llm.Message{{Role: llm.RoleUser, Content: synthesisPrompt(s)}},
			MaxTurns:       3,
			MaxTokens:      1_200,
			ThinkingEffort: llm.ThinkingHigh,
			Budget:         agent.Budget{MaxTotalTokens: 4_000, WrapUpReserve: 400},
			Stream:         true,
			Callbacks: agent.Callbacks{
				OnText: func(delta string) error {
					streamed += delta
					fmt.Print(delta)
					return nil
				},
				OnToolCall: func(call llm.ToolCall) error {
					log.Infof("synthesizer called %s", call.Name)
					return nil
				},
			},
		})
		if err != nil {
			return Update{}, err
		}
		if result.TerminalTool != "report_observation" {
			return Update{}, fmt.Errorf("synthesizer ended without report_observation")
		}
		report, err := decodeObservation(result.TerminalOutput)
		if err != nil {
			return Update{}, err
		}
		return Update{
			Draft:     &report,
			DraftJSON: string(result.TerminalOutput),
			Revised:   true,
			Usage:     result.Usage,
			Streamed:  streamed,
		}, nil
	})

	review := nodes.LLMNode(router, provider, reviewPrompt, func(response string) Update {
		return Update{Review: response}
	})
	reject := graph.NodeFunc[State, Update](func(context.Context, State) (Update, error) {
		return Update{}, nil
	})
	publish := graph.NodeFunc[State, Update](func(context.Context, State) (Update, error) {
		return Update{}, nil
	})

	runner, err := graph.NewBuilder(reducer()).
		AddNode("intake", intake).
		AddNode("evidence", specialist("evidence analyst")).
		AddNode("risk", specialist("risk and tradeoff analyst")).
		AddNode("skeptic", specialist("skeptical reviewer")).
		AddNode("synthesize", synthesize).
		AddNode("review", review).
		AddNode("reject", reject).
		AddNode("publish", publish).
		AddFanEdge("intake", func(_ context.Context, s State) ([]graph.NodeID, error) {
			if !s.Accepted {
				return []graph.NodeID{"reject"}, nil
			}
			return []graph.NodeID{"evidence", "risk", "skeptic"}, nil
		}).
		AddEdge("evidence", "synthesize").
		AddEdge("risk", "synthesize").
		AddEdge("skeptic", "synthesize").
		AddEdge("synthesize", "review").
		AddConditionalEdge("review", func(_ context.Context, s State) (graph.NodeID, error) {
			if approved(s.Review) || s.Revisions >= maxRevisions {
				return "publish", nil
			}
			return "synthesize", nil
		}).
		SetEntryPoint("intake").
		SetTerminal("reject", "publish").
		Compile(
			graph.WithMaxParallel(3),
			graph.WithRecursionLimit(10),
			graph.WithNodeErrorHandler(func(id graph.NodeID, nodeErr error) (Update, error) {
				switch id {
				case "evidence", "risk", "skeptic":
					return Update{Failure: &NodeFailure{Node: string(id), Error: nodeErr.Error()}}, nil
				default:
					return Update{}, nodeErr
				}
			}),
		)
	if err != nil {
		log.Fatalf("compile: %v", err)
	}

	query := "Should a production agent workflow continue when one parallel specialist fails?"
	if len(os.Args) > 1 {
		query = os.Args[1]
	}
	final, err := runner.Run(ctx, State{Query: query})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	if !final.Accepted {
		log.Infof("Request rejected: %s", final.IntakeNote)
		return
	}

	log.Infof("specialists=%d failures=%d revisions=%d agent_tokens=%d", len(final.Reports), len(final.Failures), final.Revisions, final.Usage.Total())
	fmt.Printf("\n\n%s\n\n%s\n", final.Draft.Title, final.Draft.Summary)
	for _, finding := range final.Draft.Findings {
		fmt.Printf("- %s\n", finding)
	}
}
