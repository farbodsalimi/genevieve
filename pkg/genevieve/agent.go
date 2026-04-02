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

func (a *Agent) Handle(ctx context.Context, provider string, prompt string) (string, error) {
	llm, ok := a.router.Get(provider)
	if !ok {
		return "", NewProviderNotFoundError(provider)
	}

	toolNames := make([]string, 0, len(a.tools))
	for name := range a.tools {
		toolNames = append(toolNames, name)
	}

	toolInput, err := a.chooseTool(ctx, llm, prompt, toolNames)
	if err != nil {
		return "", fmt.Errorf("agent handle: %w", err)
	}

	tool, ok := a.tools[toolInput.ToolName]
	if !ok {
		return "", NewToolNotFoundError(toolInput.ToolName)
	}

	return tool.Execute(ctx, toolInput)
}

func (a *Agent) chooseTool(
	ctx context.Context,
	llm LLM,
	question string,
	toolNames []string,
) (AgentToolInput, error) {
	jsonData, err := llm.Chat(ctx, []Message{
		{
			Role:    RoleSystem,
			Content: AgentSystemPrompt(),
		},
		{
			Role:    RoleUser,
			Content: AgentChooseToolPrompt(toolNames, question),
		},
	})
	if err != nil {
		return AgentToolInput{}, fmt.Errorf("tool selection: %w", err)
	}

	resp, err := JSONToToolExecutionInput(jsonData)
	if err != nil {
		return AgentToolInput{}, fmt.Errorf("tool selection: parse response: %w", err)
	}

	return resp, nil
}

func JSONToToolExecutionInput(jsonData string) (AgentToolInput, error) {
	var ati AgentToolInput
	if err := json.Unmarshal([]byte(jsonData), &ati); err != nil {
		return ati, fmt.Errorf("unmarshal tool input: %w", err)
	}
	if ati.ToolName == "" {
		return ati, fmt.Errorf("tool input missing required field \"tool\"")
	}
	return ati, nil
}
