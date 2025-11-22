package genevieve

import (
	"context"
	"encoding/json"
	"fmt"
)

type AgentTool interface {
	Name() string
	Execute(ctx context.Context, input AgentToolInput) (string, error)
}

type AgentToolInput struct {
	ToolName  string `json:"tool"`
	ToolInput string `json:"input"`
}

type Agent struct {
	router *Router
	tools  map[string]AgentTool
}

func NewAgent(router *Router) *Agent {
	return &Agent{router: router, tools: make(map[string]AgentTool)}
}

func (a *Agent) RegisterTool(tool AgentTool) error {
	if tool == nil {
		return fmt.Errorf("cannot register nil tool")
	}

	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	if _, exists := a.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}

	a.tools[name] = tool
	return nil
}

func (a *Agent) TryRegisterTool(tool AgentTool) {
	if tool == nil {
		return
	}

	name := tool.Name()
	if name == "" {
		return
	}

	if _, exists := a.tools[name]; exists {
		return
	}

	a.tools[name] = tool
}

// TODO: Add support for tool chaining - agents can only use one tool per request
// TODO: Add structured logging for agent operations
func (a *Agent) Handle(ctx context.Context, provider string, prompt string) (string, error) {
	llm, ok := a.router.Get(provider)
	if !ok {
		// TODO: Use structured error types instead of fmt.Errorf
		return "", fmt.Errorf("provider %s not found", provider)
	}

	toolNames := make([]string, 0, len(a.tools))
	for name := range a.tools {
		toolNames = append(toolNames, name)
	}

	toolInput, err := llm.ChooseTool(ctx, prompt, toolNames)
	if err != nil {
		return "", err
	}

	tool, ok := a.tools[toolInput.ToolName]
	if !ok {
		// TODO: Use structured error types instead of fmt.Errorf
		return "", fmt.Errorf("Unknown tool: %s", toolInput.ToolName)
	}

	return tool.Execute(ctx, toolInput)
}

func JSONToToolExecutionInput(jsonData string) (AgentToolInput, error) {
	var ati AgentToolInput
	err := json.Unmarshal([]byte(jsonData), &ati)
	if err != nil {
		return ati, err
	}
	return ati, nil
}
