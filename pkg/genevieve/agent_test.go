package genevieve

import (
	"context"
	"testing"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Execute(ctx context.Context, input AgentToolInput) (string, error) {
	return "mock result", nil
}

func TestRegisterTool_ValidTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool := &mockTool{name: "test-tool"}

	err := agent.RegisterTool(tool)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if _, exists := agent.tools["test-tool"]; !exists {
		t.Error("tool was not registered")
	}
}

func TestRegisterTool_NilTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)

	err := agent.RegisterTool(nil)
	if err == nil {
		t.Error("expected error for nil tool, got nil")
	}

	expectedMsg := "cannot register nil tool"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestRegisterTool_EmptyName(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool := &mockTool{name: ""}

	err := agent.RegisterTool(tool)
	if err == nil {
		t.Error("expected error for empty tool name, got nil")
	}

	expectedMsg := "tool name cannot be empty"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestRegisterTool_DuplicateTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool1 := &mockTool{name: "calculator"}
	tool2 := &mockTool{name: "calculator"}

	err := agent.RegisterTool(tool1)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err = agent.RegisterTool(tool2)
	if err == nil {
		t.Error("expected error for duplicate tool name, got nil")
	}

	expectedMsg := `tool "calculator" is already registered`
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestTryRegisterTool_ValidTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool := &mockTool{name: "test-tool"}

	agent.TryRegisterTool(tool)

	if _, exists := agent.tools["test-tool"]; !exists {
		t.Error("tool was not registered")
	}
}

func TestTryRegisterTool_NilTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)

	agent.TryRegisterTool(nil)

	if len(agent.tools) != 0 {
		t.Error("expected no tools to be registered")
	}
}

func TestTryRegisterTool_EmptyName(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool := &mockTool{name: ""}

	agent.TryRegisterTool(tool)

	if len(agent.tools) != 0 {
		t.Error("expected no tools to be registered")
	}
}

func TestTryRegisterTool_DuplicateTool(t *testing.T) {
	router := NewRouter()
	agent := NewAgent(router)
	tool1 := &mockTool{name: "calculator"}
	tool2 := &mockTool{name: "calculator"}

	agent.TryRegisterTool(tool1)
	agent.TryRegisterTool(tool2)

	if len(agent.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(agent.tools))
	}

	if _, exists := agent.tools["calculator"]; !exists {
		t.Error("calculator tool should be registered")
	}
}
