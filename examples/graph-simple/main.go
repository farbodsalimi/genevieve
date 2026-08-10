// Command graph-simple is the smallest useful graph: two nodes in sequence,
// with no LLM involved. It exists to show the four moving parts — state, update,
// reducer, topology — without provider setup or prompts in the way.
//
// For a real workflow with an LLM and a bounded revision loop, see
// examples/graph.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/farbodsalimi/genevieve/pkg/graph"
)

// State is what the graph carries from node to node.
type State struct {
	Input string
	Steps []string
}

// Update is the partial change one node emits. A node fills in only the fields
// it owns and leaves the rest zero.
type Update struct {
	Step string
}

func main() {
	// Nodes receive state by value and return an update — never writing to
	// shared state directly, which is what makes parallel nodes safe.
	upper := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
		return Update{Step: strings.ToUpper(s.Input)}, nil
	})

	exclaim := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
		return Update{Step: s.Steps[len(s.Steps)-1] + "!"}, nil
	})

	// The reducer merges updates into state. Combinators declare one rule per
	// field, so there is no hand-written merge logic: Append grows the slice
	// copy-on-write, which is what keeps fan-in safe when nodes run in parallel.
	reducer := graph.Merge(
		graph.Append(
			func(s *State) *[]string { return &s.Steps },
			func(u Update) string { return u.Step },
		),
	)

	// Compile runs static analysis (dangling edges, unreachable nodes, dead
	// ends) once and returns an immutable Runner safe for concurrent reuse.
	runner, err := graph.NewBuilder(reducer).
		AddNode("upper", upper).
		AddNode("exclaim", exclaim).
		AddEdge("upper", "exclaim").
		SetEntryPoint("upper").
		SetTerminal("exclaim").
		Compile()
	if err != nil {
		log.Fatalf("compile: %v", err)
	}

	final, err := runner.Run(context.Background(), State{Input: "hello"})
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	fmt.Println(final.Steps) // [HELLO HELLO!]
}
