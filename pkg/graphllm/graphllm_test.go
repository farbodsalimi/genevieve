package graphllm

import (
	"context"
	"errors"
	"testing"

	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// mockLLM is a hand-written LLM implementing the llm.LLM interface.
type mockLLM struct {
	name       string
	completeFn func(ctx context.Context, prompt string) (string, error)
	chatFn     func(ctx context.Context, msgs []llm.Message) (string, error)
}

func (m *mockLLM) Name() string { return m.name }
func (m *mockLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return m.completeFn(ctx, prompt)
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
		func(s ChatState) string { return "hello" },
		func(resp string) ChatUpdate {
			return ChatUpdate{Message: llm.Message{Role: llm.RoleAssistant, Content: resp}}
		},
	)
	u, err := node.Execute(context.Background(), ChatState{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if u.Message.Content != "world" {
		t.Fatalf("got %q", u.Message.Content)
	}
}

func TestLLMNode_ProviderNotFound(t *testing.T) {
	router := llm.NewRouter()
	node := LLMNode(
		router, "missing",
		func(s ChatState) string { return "x" },
		func(resp string) ChatUpdate { return ChatUpdate{} },
	)
	_, err := node.Execute(context.Background(), ChatState{})
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
		func(s ChatState) string { return "x" },
		func(resp string) ChatUpdate { return ChatUpdate{} },
	)
	_, err := node.Execute(context.Background(), ChatState{})
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
	tool := &mockTool{name: "echo", fn: func(ctx context.Context, in agent.AgentToolInput) (string, error) {
		return "echo:" + in.ToolInput, nil
	}}
	node := ToolNode(
		tool,
		func(s ChatState) agent.AgentToolInput {
			return agent.AgentToolInput{ToolName: "echo", ToolInput: "hi"}
		},
		func(resp string) ChatUpdate {
			return ChatUpdate{Message: llm.Message{Role: llm.RoleAssistant, Content: resp}}
		},
	)
	u, err := node.Execute(context.Background(), ChatState{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if u.Message.Content != "echo:hi" {
		t.Fatalf("got %q", u.Message.Content)
	}
}

func TestChatReducer_AppendsCopyOnWrite(t *testing.T) {
	reducer := ChatReducer()
	base := ChatState{Messages: []llm.Message{{Role: llm.RoleUser, Content: "one"}}}
	next, err := reducer.Reduce(base, ChatUpdate{Message: llm.Message{Role: llm.RoleAssistant, Content: "two"}})
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if len(next.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(next.Messages))
	}
	// base must be untouched (copy-on-write).
	if len(base.Messages) != 1 {
		t.Fatalf("base mutated: %d messages", len(base.Messages))
	}
}

// End-to-end: a two-node chat graph using ChatState/ChatReducer.
func TestChatGraph_EndToEnd(t *testing.T) {
	m := &mockLLM{name: "mock", completeFn: func(ctx context.Context, prompt string) (string, error) {
		return "reply", nil
	}}
	router := newNodeRouter(t, m)
	node := LLMNode(
		router, "mock",
		func(s ChatState) string {
			if len(s.Messages) == 0 {
				return ""
			}
			return s.Messages[len(s.Messages)-1].Content
		},
		func(resp string) ChatUpdate {
			return ChatUpdate{Message: llm.Message{Role: llm.RoleAssistant, Content: resp}}
		},
	)
	runner, err := graph.NewBuilder(ChatReducer()).
		AddNode("chat", node).
		SetEntryPoint("chat").
		SetTerminal("chat").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	init := ChatState{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}
	final, err := runner.Run(context.Background(), init)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(final.Messages) != 2 || final.Messages[1].Content != "reply" {
		t.Fatalf("unexpected final state: %+v", final.Messages)
	}
}
