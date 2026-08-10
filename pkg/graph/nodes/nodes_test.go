package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// testState and testUpdate keep these adapter tests independent of any
// particular state type, including pkg/graph/chat.
type testState struct {
	Prompt string
}

type testUpdate struct {
	Response string
}

func testReducer() graph.Reducer[testState, testUpdate] {
	return graph.ReducerFunc[testState, testUpdate](
		func(s testState, u testUpdate) (testState, error) {
			return testState{Prompt: u.Response}, nil
		},
	)
}

// mockLLM is a hand-written LLM implementing the llm.LLM interface.
type mockLLM struct {
	name       string
	completeFn func(ctx context.Context, prompt string) (string, error)
	chatFn     func(ctx context.Context, msgs []llm.Message) (string, error)
}

func (m *mockLLM) Name() string { return m.name }
func (m *mockLLM) Complete(ctx context.Context, prompt string) (string, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, prompt)
	}
	return "", nil
}
func (m *mockLLM) Chat(ctx context.Context, msgs []llm.Message) (string, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, msgs)
	}
	return "", nil
}

func newNodeRouter(t *testing.T, provider llm.LLM) *llm.Router {
	t.Helper()
	r := llm.NewRouter()
	if err := r.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}
	return r
}

func TestLLMNode_Success(t *testing.T) {
	m := &mockLLM{
		name: "mock",
		completeFn: func(ctx context.Context, prompt string) (string, error) {
			if prompt != "hello" {
				t.Errorf("unexpected prompt %q", prompt)
			}
			return "world", nil
		},
	}
	router := newNodeRouter(t, m)
	node := LLMNode(
		router, "mock",
		func(s testState) string { return s.Prompt },
		func(resp string) testUpdate { return testUpdate{Response: resp} },
	)
	u, err := node.Execute(context.Background(), testState{Prompt: "hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if u.Response != "world" {
		t.Fatalf("got %q", u.Response)
	}
}

func TestLLMNode_ProviderNotFound(t *testing.T) {
	router := llm.NewRouter()
	node := LLMNode(
		router, "missing",
		func(s testState) string { return "x" },
		func(resp string) testUpdate { return testUpdate{} },
	)
	_, err := node.Execute(context.Background(), testState{})
	var target *llm.ProviderNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected ProviderNotFoundError, got %v", err)
	}
}

func TestLLMNode_CompleteError(t *testing.T) {
	boom := errors.New("provider boom")
	m := &mockLLM{name: "mock", completeFn: func(ctx context.Context, p string) (string, error) {
		return "", boom
	}}
	router := newNodeRouter(t, m)
	node := LLMNode(
		router, "mock",
		func(s testState) string { return "x" },
		func(resp string) testUpdate { return testUpdate{} },
	)
	_, err := node.Execute(context.Background(), testState{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

// mockTool is a hand-written AgentTool for the ToolNode adapter test.
type mockTool struct {
	name string
	fn   func(ctx context.Context, in agent.AgentToolInput) (string, error)
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Execute(ctx context.Context, in agent.AgentToolInput) (string, error) {
	return m.fn(ctx, in)
}

func TestToolNode_Success(t *testing.T) {
	tool := &mockTool{
		name: "echo",
		fn: func(ctx context.Context, in agent.AgentToolInput) (string, error) {
			return "echo:" + in.ToolInput, nil
		},
	}
	node := ToolNode(
		tool,
		func(s testState) agent.AgentToolInput {
			return agent.AgentToolInput{ToolName: "echo", ToolInput: s.Prompt}
		},
		func(resp string) testUpdate { return testUpdate{Response: resp} },
	)
	u, err := node.Execute(context.Background(), testState{Prompt: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if u.Response != "echo:hi" {
		t.Fatalf("got %q", u.Response)
	}
}

func TestToolNode_ExecuteError(t *testing.T) {
	boom := errors.New("tool boom")
	tool := &mockTool{
		name: "echo",
		fn: func(ctx context.Context, in agent.AgentToolInput) (string, error) {
			return "", boom
		},
	}
	node := ToolNode(
		tool,
		func(s testState) agent.AgentToolInput {
			return agent.AgentToolInput{ToolName: "echo", ToolInput: "hi"}
		},
		func(resp string) testUpdate { return testUpdate{Response: resp} },
	)
	_, err := node.Execute(context.Background(), testState{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

// newAgentWithTool wires an Agent whose LLM always selects the named tool.
func newAgentWithTool(t *testing.T, tool agent.AgentTool) *agent.Agent {
	t.Helper()
	m := &mockLLM{
		name: "mock",
		chatFn: func(ctx context.Context, msgs []llm.Message) (string, error) {
			return `{"tool":"` + tool.Name() + `","input":"hi"}`, nil
		},
	}
	a := agent.NewAgent(newNodeRouter(t, m))
	if err := a.RegisterTool(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	return a
}

func TestAgentNode_Success(t *testing.T) {
	tool := &mockTool{
		name: "echo",
		fn: func(ctx context.Context, in agent.AgentToolInput) (string, error) {
			return "echo:" + in.ToolInput, nil
		},
	}
	node := AgentNode(
		newAgentWithTool(t, tool), "mock",
		func(s testState) string { return s.Prompt },
		func(resp string) testUpdate { return testUpdate{Response: resp} },
	)
	u, err := node.Execute(context.Background(), testState{Prompt: "question"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if u.Response != "echo:hi" {
		t.Fatalf("got %q", u.Response)
	}
}

func TestAgentNode_HandleError(t *testing.T) {
	boom := errors.New("tool boom")
	tool := &mockTool{
		name: "echo",
		fn: func(ctx context.Context, in agent.AgentToolInput) (string, error) {
			return "", boom
		},
	}
	node := AgentNode(
		newAgentWithTool(t, tool), "mock",
		func(s testState) string { return "x" },
		func(resp string) testUpdate { return testUpdate{} },
	)
	_, err := node.Execute(context.Background(), testState{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}

func TestAgentNode_ProviderNotFound(t *testing.T) {
	a := agent.NewAgent(llm.NewRouter())
	node := AgentNode(
		a, "missing",
		func(s testState) string { return "x" },
		func(resp string) testUpdate { return testUpdate{} },
	)
	_, err := node.Execute(context.Background(), testState{})
	var target *llm.ProviderNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected ProviderNotFoundError, got %v", err)
	}
}

// End-to-end: a one-node graph driven through the runner.
func TestLLMNode_EndToEnd(t *testing.T) {
	m := &mockLLM{
		name: "mock",
		completeFn: func(ctx context.Context, prompt string) (string, error) {
			return "reply", nil
		},
	}
	router := newNodeRouter(t, m)
	node := LLMNode(
		router, "mock",
		func(s testState) string { return s.Prompt },
		func(resp string) testUpdate { return testUpdate{Response: resp} },
	)
	runner, err := graph.NewBuilder(testReducer()).
		AddNode("chat", node).
		SetEntryPoint("chat").
		SetTerminal("chat").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{Prompt: "hi"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final.Prompt != "reply" {
		t.Fatalf("unexpected final state: %+v", final)
	}
}
