// Package chat supplies a batteries-included state and reducer for the most
// common graph shape: a conversation accumulating llm.Message values.
//
// These are state and reducer types, not nodes; the node adapters that call a
// provider live in pkg/graph/nodes. A caller with a richer state type ignores
// this package and writes their own State/Update pair.
package chat

import (
	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// State is a default graph state holding conversation history.
type State struct {
	Messages []llm.Message
}

// Clone deep-copies the message slice so parallel nodes never share the backing
// array. Detected automatically by the runner.
func (s State) Clone() State {
	msgs := make([]llm.Message, len(s.Messages))
	copy(msgs, s.Messages)
	return State{Messages: msgs}
}

// Update is a partial update appending one message to the conversation.
type Update struct {
	Message llm.Message
}

// Reducer appends an update's message to the running conversation,
// copy-on-write so the previous state is never mutated.
func Reducer() graph.Reducer[State, Update] {
	return graph.ReducerFunc[State, Update](
		func(s State, u Update) (State, error) {
			msgs := make([]llm.Message, len(s.Messages), len(s.Messages)+1)
			copy(msgs, s.Messages)
			msgs = append(msgs, u.Message)
			return State{Messages: msgs}, nil
		},
	)
}
