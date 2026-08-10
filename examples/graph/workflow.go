package main

// The workflow's data model: the state the graph carries, the partial update a
// node emits, and the rule for merging one into the other. Prompt text lives in
// prompts.go; the topology that wires it all together lives in main.go.

import (
	"github.com/farbodsalimi/genevieve/pkg/graph"
)

// maxRevisions bounds the draft→critique loop. The critique router enforces it
// against State.Revisions; graph.WithRecursionLimit is only the engine-level
// backstop for a router that never converges.
const maxRevisions = 3

// State is the workflow's typed state — no map[string]any in sight.
type State struct {
	Topic     string
	Draft     string
	Critiques []string
	Revisions int
	Published string
}

// Update is a partial change produced by a node. A node fills in only the
// fields it is responsible for and leaves the rest zero.
type Update struct {
	Draft     string
	Critique  string
	Published string
	Revised   bool
}

// reducer merges an Update into State. Each rule names one field and how it
// combines, so there is no hand-written chain of zero-value checks: SetIf skips
// a field the node did not produce, AppendIf grows the critique history
// copy-on-write, and Add counts revisions where a zero-value check could not.
func reducer() graph.Reducer[State, Update] {
	return graph.Merge(
		graph.SetIf(
			func(s *State) *string { return &s.Draft },
			func(u Update) string { return u.Draft },
			graph.NonZero[string],
		),
		graph.AppendIf(
			func(s *State) *[]string { return &s.Critiques },
			func(u Update) string { return u.Critique },
			graph.NonZero[string],
		),
		graph.Add(
			func(s *State) *int { return &s.Revisions },
			func(u Update) int {
				if u.Revised {
					return 1
				}
				return 0
			},
		),
		graph.SetIf(
			func(s *State) *string { return &s.Published },
			func(u Update) string { return u.Published },
			graph.NonZero[string],
		),
	)
}

// lastCritique returns the most recent critique, or "" before the first one.
func (s State) lastCritique() string {
	if len(s.Critiques) == 0 {
		return ""
	}
	return s.Critiques[len(s.Critiques)-1]
}
