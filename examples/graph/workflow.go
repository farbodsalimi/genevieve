package main

// The workflow's data model: the state the graph carries, the partial update a
// node emits, and the rule for merging one into the other. Prompt text lives in
// prompts.go; the topology that wires it all together lives in main.go.

import (
	"slices"

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

// reducer merges an Update into State, one field at a time. A zero value means
// the node did not produce that field, so each string field is guarded — except
// Revisions, which is counted off the explicit Revised flag because a zero-value
// check cannot distinguish "no delta" from a real one.
func reducer() graph.Reducer[State, Update] {
	return graph.ReducerFunc[State, Update](func(s State, u Update) (State, error) {
		nextState := s
		// copy the critique slice so the incoming state is never mutated
		nextState.Critiques = slices.Clone(s.Critiques)
		if u.Draft != "" {
			nextState.Draft = u.Draft
		}
		if u.Critique != "" {
			nextState.Critiques = append(nextState.Critiques, u.Critique)
		}
		if u.Revised {
			nextState.Revisions = s.Revisions + 1
		}
		if u.Published != "" {
			nextState.Published = u.Published
		}
		return nextState, nil
	})
}

// lastCritique returns the most recent critique, or "" before the first one.
func (s State) lastCritique() string {
	if len(s.Critiques) == 0 {
		return ""
	}
	return s.Critiques[len(s.Critiques)-1]
}
