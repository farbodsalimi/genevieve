package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

type testState struct{ Prompt string }
type testUpdate struct{ Response string }

type mockLLM struct { name string; responses []llm.GenerateResponse; err error }
func (m *mockLLM) Name() string { return m.name }
func (m *mockLLM) Generate(_ context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	if m.err != nil { return llm.GenerateResponse{}, m.err }
	response := m.responses[0]; m.responses = m.responses[1:]; return response, nil
}
func (m *mockLLM) Stream(ctx context.Context, req llm.GenerateRequest, emit llm.EventHandler) (llm.GenerateResponse, error) { return m.Generate(ctx, req) }

func routerWith(t *testing.T, model llm.LLM) *llm.Router {
	t.Helper(); router := llm.NewRouter(); if err := router.Register(model); err != nil { t.Fatal(err) }; return router
}

type mockTool struct { err error }
func (*mockTool) Name() string { return "echo" }
func (*mockTool) Description() string { return "echo" }
func (*mockTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*mockTool) Terminal() bool { return false }
func (m *mockTool) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	if m.err != nil { return nil, m.err }; return append(json.RawMessage("echo:"), input...), nil
}

func TestLLMNode(t *testing.T) {
	model := &mockLLM{name: "mock", responses: []llm.GenerateResponse{{Text: "world"}}}
	node := LLMNode(routerWith(t, model), "mock", func(s testState) string { return s.Prompt }, func(v string) testUpdate { return testUpdate{v} })
	update, err := node.Execute(context.Background(), testState{"hello"})
	if err != nil || update.Response != "world" { t.Fatalf("update=%+v err=%v", update, err) }
}

func TestLLMNodeErrors(t *testing.T) {
	boom := errors.New("boom")
	node := LLMNode(routerWith(t, &mockLLM{name: "mock", err: boom}), "mock", func(testState) string { return "x" }, func(string) testUpdate { return testUpdate{} })
	if _, err := node.Execute(context.Background(), testState{}); !errors.Is(err, boom) { t.Fatalf("got %v", err) }
	missing := LLMNode(llm.NewRouter(), "missing", func(testState) string { return "x" }, func(string) testUpdate { return testUpdate{} })
	var target *llm.ProviderNotFoundError
	if _, err := missing.Execute(context.Background(), testState{}); !errors.As(err, &target) { t.Fatalf("got %v", err) }
}

func TestToolNode(t *testing.T) {
	node := ToolNode(&mockTool{}, func(s testState) json.RawMessage { return json.RawMessage(s.Prompt) }, func(v string) testUpdate { return testUpdate{v} })
	update, err := node.Execute(context.Background(), testState{"hi"})
	if err != nil || update.Response != "echo:hi" { t.Fatalf("update=%+v err=%v", update, err) }
}

func TestToolNodeError(t *testing.T) {
	boom := errors.New("boom")
	node := ToolNode(&mockTool{err: boom}, func(testState) json.RawMessage { return nil }, func(string) testUpdate { return testUpdate{} })
	if _, err := node.Execute(context.Background(), testState{}); !errors.Is(err, boom) { t.Fatalf("got %v", err) }
}

func TestAgentNode(t *testing.T) {
	model := &mockLLM{name: "mock", responses: []llm.GenerateResponse{{Text: "answer"}}}
	a := agent.NewAgent(routerWith(t, model))
	node := AgentNode(a, "mock", func(s testState) string { return s.Prompt }, func(v string) testUpdate { return testUpdate{v} })
	update, err := node.Execute(context.Background(), testState{"question"})
	if err != nil || update.Response != "answer" { t.Fatalf("update=%+v err=%v", update, err) }
}
