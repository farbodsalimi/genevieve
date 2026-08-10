package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/farbodsalimi/genevieve/pkg/llm"
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
	mu     sync.RWMutex
	router *llm.Router
	tools  map[string]AgentTool
}

func NewAgent(router *llm.Router) *Agent {
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

	a.mu.Lock()
	defer a.mu.Unlock()

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

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.tools[name]; exists {
		return
	}

	a.tools[name] = tool
}

func (a *Agent) Handle(ctx context.Context, provider string, prompt string) (string, error) {
	client, ok := a.router.Get(provider)
	if !ok {
		return "", llm.NewProviderNotFoundError(provider)
	}

	a.mu.RLock()
	toolNames := make([]string, 0, len(a.tools))
	for name := range a.tools {
		toolNames = append(toolNames, name)
	}
	a.mu.RUnlock()

	toolInput, err := a.chooseTool(ctx, client, prompt, toolNames)
	if err != nil {
		return "", fmt.Errorf("agent handle: %w", err)
	}

	a.mu.RLock()
	tool, ok := a.tools[toolInput.ToolName]
	a.mu.RUnlock()
	if !ok {
		return "", NewToolNotFoundError(toolInput.ToolName)
	}

	return tool.Execute(ctx, toolInput)
}

func (a *Agent) chooseTool(
	ctx context.Context,
	client llm.LLM,
	question string,
	toolNames []string,
) (AgentToolInput, error) {
	jsonData, err := client.Chat(ctx, []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: AgentSystemPrompt(),
		},
		{
			Role:    llm.RoleUser,
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
