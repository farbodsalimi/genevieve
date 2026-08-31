// Package nodes adapts the LLM packages to the graph engine. Every export here
// is a graph.Node constructor; conversation state and its reducer live in
// pkg/graph/chat.
//
// This package imports llm, agent, and graph — never the reverse — so pkg/graph
// stays domain-free with no import cycle.
package nodes

import (
	"context"
	"encoding/json"

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
		resp, err := client.Generate(ctx, llm.GenerateRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt(state)}}})
		if err != nil {
			return zero, err
		}
		return wrap(resp.Text), nil
	})
}

// ToolNode executes an agent.AgentTool with input derived from state.
func ToolNode[T any, U any](
	tool agent.AgentTool,
	input func(T) json.RawMessage,
	wrap func(string) U,
) graph.Node[T, U] {
	return graph.NodeFunc[T, U](func(ctx context.Context, state T) (U, error) {
		var zero U
		resp, err := tool.Execute(ctx, input(state))
		if err != nil {
			return zero, err
		}
		return wrap(string(resp)), nil
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
		resp, err := a.Run(ctx, agent.RunRequest{Provider: provider, Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt(state)}}})
		if err != nil {
			return zero, err
		}
		return wrap(resp.Output), nil
	})
}
