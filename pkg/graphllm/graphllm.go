// Package graphllm adapts genevieve's LLM primitives (pkg/llm, pkg/agent) to the
// generic graph engine (pkg/graph). The dependency runs one way — graphllm
// imports llm, agent, and graph — so pkg/graph stays domain-free with no import
// cycle.
package graphllm

import (
	"context"

	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// LLMNode calls a provider with a prompt derived from state and wraps the
// response as a state update. Prompt construction is ordinary typed Go, checked
// at compile time; a caller who wants templating can call text/template inside
// their own prompt closure.
func LLMNode[T any, U any](
	r *llm.Router,
	provider string,
	prompt func(T) string,
	wrap func(string) U,
) graph.Node[T, U] {
	return graph.NodeFunc[T, U](func(ctx context.Context, state T) (U, error) {
		var zero U
		client, ok := r.Get(provider)
		if !ok {
			return zero, llm.NewProviderNotFoundError(provider)
		}
		resp, err := client.Complete(ctx, prompt(state))
		if err != nil {
			return zero, err
		}
		return wrap(resp), nil
	})
}

// ToolNode executes an agent.AgentTool with input derived from state.
func ToolNode[T any, U any](
	tool agent.AgentTool,
	input func(T) agent.AgentToolInput,
	wrap func(string) U,
) graph.Node[T, U] {
	return graph.NodeFunc[T, U](func(ctx context.Context, state T) (U, error) {
		var zero U
		resp, err := tool.Execute(ctx, input(state))
		if err != nil {
			return zero, err
		}
		return wrap(resp), nil
	})
}

// AgentNode delegates to agent.Agent.Handle, letting one node do LLM
// tool-selection.
func AgentNode[T any, U any](
	a *agent.Agent,
	provider string,
	prompt func(T) string,
	wrap func(string) U,
) graph.Node[T, U] {
	return graph.NodeFunc[T, U](func(ctx context.Context, state T) (U, error) {
		var zero U
		resp, err := a.Handle(ctx, provider, prompt(state))
		if err != nil {
			return zero, err
		}
		return wrap(resp), nil
	})
}

// ChatState is a batteries-included default state for the common case:
// conversation history as a slice of llm.Message.
type ChatState struct {
	Messages []llm.Message
}

// Clone deep-copies the message slice so parallel nodes never share the backing
// array. Detected automatically by the runner.
func (c ChatState) Clone() ChatState {
	msgs := make([]llm.Message, len(c.Messages))
	copy(msgs, c.Messages)
	return ChatState{Messages: msgs}
}

// ChatUpdate is a partial update appending one message to the conversation.
type ChatUpdate struct {
	Message llm.Message
}

// ChatReducer appends an update's message to the running conversation,
// copy-on-write so the previous state is never mutated.
func ChatReducer() graph.Reducer[ChatState, ChatUpdate] {
	return graph.ReducerFunc[ChatState, ChatUpdate](func(s ChatState, u ChatUpdate) (ChatState, error) {
		msgs := make([]llm.Message, len(s.Messages), len(s.Messages)+1)
		copy(msgs, s.Messages)
		msgs = append(msgs, u.Message)
		return ChatState{Messages: msgs}, nil
	})
}
