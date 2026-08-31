package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

type mockTool struct {
	name string
	terminal bool
	execute func(json.RawMessage) (json.RawMessage, error)
}
func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Description() string { return "mock tool" }
func (m *mockTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`) }
func (m *mockTool) Terminal() bool { return m.terminal }
func (m *mockTool) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	if m.execute != nil { return m.execute(input) }
	return json.RawMessage(`{"ok":true}`), nil
}

type mockLLM struct { responses []llm.GenerateResponse; requests []llm.GenerateRequest; stream bool }
func (m *mockLLM) Name() string { return "mock" }
func (m *mockLLM) Generate(_ context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	m.requests = append(m.requests, req)
	response := m.responses[0]; m.responses = m.responses[1:]
	return response, nil
}
func (m *mockLLM) Stream(ctx context.Context, req llm.GenerateRequest, emit llm.EventHandler) (llm.GenerateResponse, error) {
	m.stream = true
	response, err := m.Generate(ctx, req)
	if err != nil { return response, err }
	if response.Text != "" { if err := emit(llm.Event{Type: llm.EventTextDelta, Text: response.Text}); err != nil { return response, err } }
	for i := range response.ToolCalls { call := response.ToolCalls[i]; if err := emit(llm.Event{Type: llm.EventToolCall, ToolCall: &call}); err != nil { return response, err } }
	return response, nil
}

func newTestAgent(t *testing.T, model llm.LLM, tools ...AgentTool) *Agent {
	t.Helper()
	router := llm.NewRouter()
	if err := router.Register(model); err != nil { t.Fatal(err) }
	a := NewAgent(router)
	for _, tool := range tools { if err := a.RegisterTool(tool); err != nil { t.Fatal(err) } }
	return a
}

func TestRegisterToolValidation(t *testing.T) {
	a := NewAgent(llm.NewRouter())
	if err := a.RegisterTool(nil); err == nil { t.Fatal("expected nil-tool error") }
	if err := a.RegisterTool(&mockTool{}); err == nil { t.Fatal("expected empty-name error") }
	tool := &mockTool{name: "echo"}
	if err := a.RegisterTool(tool); err != nil { t.Fatal(err) }
	if err := a.RegisterTool(tool); err == nil { t.Fatal("expected duplicate error") }
}

func TestRun_MultiTurnToolLoopPassesSchemaAndUsage(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{
		{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "echo", Input: json.RawMessage(`{"value":"hello"}`)}}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 3}},
		{Text: "done", Usage: llm.Usage{InputTokens: 15, OutputTokens: 2}},
	}}
	var gotInput json.RawMessage
	tool := &mockTool{name: "echo", execute: func(input json.RawMessage) (json.RawMessage, error) { gotInput = slices.Clone(input); return json.RawMessage(`{"echo":"hello"}`), nil }}
	a := newTestAgent(t, model, tool)
	result, err := a.Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "say hello"}}})
	if err != nil { t.Fatal(err) }
	if result.Output != "done" || result.Turns != 2 || result.Usage != (llm.Usage{InputTokens: 25, OutputTokens: 5}) { t.Fatalf("unexpected result: %+v", result) }
	if string(gotInput) != `{"value":"hello"}` { t.Fatalf("input = %s", gotInput) }
	if len(model.requests[0].Tools) != 1 || !json.Valid(model.requests[0].Tools[0].InputSchema) { t.Fatalf("tool schema not passed: %+v", model.requests[0].Tools) }
	last := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "call-1" { t.Fatalf("tool result not correlated: %+v", last) }
}

func TestRun_TerminalToolReturnsStructuredOutput(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{{ToolCalls: []llm.ToolCall{{ID: "1", Name: "report_observation", Input: json.RawMessage(`{"finding":"x"}`)}}}}}
	tool := &mockTool{name: "report_observation", terminal: true, execute: func(input json.RawMessage) (json.RawMessage, error) { return input, nil }}
	result, err := newTestAgent(t, model, tool).Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "inspect"}}})
	if err != nil { t.Fatal(err) }
	if result.StopReason != StopTerminalTool || result.TerminalTool != "report_observation" || string(result.TerminalOutput) != `{"finding":"x"}` { t.Fatalf("unexpected result: %+v", result) }
}

func TestRun_BudgetWrapUp(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{
		{ToolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Input: json.RawMessage(`{}`)}}, Usage: llm.Usage{InputTokens: 70, OutputTokens: 10}},
		{Text: "partial summary", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}},
	}}
	result, err := newTestAgent(t, model, &mockTool{name: "echo"}).Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "work"}}, Budget: Budget{MaxTotalTokens: 100, WrapUpReserve: 20}})
	if err != nil { t.Fatal(err) }
	if result.StopReason != StopTokenBudget || result.Output != "partial summary" || len(model.requests[1].Tools) != 0 { t.Fatalf("unexpected wrap-up: %+v", result) }
}

func TestRun_StreamingAndMiddleware(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{{Text: "secret"}}}
	a := newTestAgent(t, model)
	a.UseEvents(func(next llm.EventHandler) llm.EventHandler { return func(event llm.Event) error { if event.Type == llm.EventTextDelta { event.Text = "tokenized" }; return next(event) } })
	var text string
	result, err := a.Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}, Stream: true, OnEvent: func(event llm.Event) error { text += event.Text; return nil }})
	if err != nil { t.Fatal(err) }
	if !model.stream || text != "tokenized" || result.Output != "secret" { t.Fatalf("stream=%v text=%q result=%+v", model.stream, text, result) }
}

func TestRun_ToolMiddleware(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{{ToolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Input: json.RawMessage(`{}`)}}}, {Text: "done"}}}
	a := newTestAgent(t, model, &mockTool{name: "echo"})
	called := false
	a.UseTool(func(next ToolExecutor) ToolExecutor { return func(ctx context.Context, call llm.ToolCall) (json.RawMessage, error) { called = true; return next(ctx, call) } })
	_, err := a.Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	if err != nil || !called { t.Fatalf("err=%v called=%v", err, called) }
}

func TestRun_TurnLimit(t *testing.T) {
	model := &mockLLM{responses: []llm.GenerateResponse{{ToolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Input: json.RawMessage(`{}`)}}}}}
	_, err := newTestAgent(t, model, &mockTool{name: "echo"}).Run(context.Background(), RunRequest{Provider: "mock", Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}, MaxTurns: 1})
	var target *TurnLimitError
	if !errors.As(err, &target) { t.Fatalf("expected TurnLimitError, got %v", err) }
}
