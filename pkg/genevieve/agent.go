package genevieve

import (
	"context"
	"encoding/json"
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
		return NewNilToolError()
	}

	name := tool.Name()
	if name == "" {
		return NewEmptyToolNameError()
	}

	if _, exists := a.tools[name]; exists {
		return NewDuplicateToolError(name)
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
		return "", NewProviderNotFoundError(provider)
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
		return "", NewToolNotFoundError(toolInput.ToolName)
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
