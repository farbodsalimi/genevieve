package genevieve

import (
	"encoding/json"
	"fmt"
)

type AgentTool interface {
	Name() string
	Execute(input AgentToolInput) (string, error)
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

func (a *Agent) RegisterTool(tool AgentTool) {
	a.tools[tool.Name()] = tool
}

func (a *Agent) Handle(provider string, prompt string) (string, error) {
	llm, ok := a.router.Get(provider)
	if !ok {
		return "", fmt.Errorf("provider %s not found", provider)
	}

	toolNames := make([]string, 0, len(a.tools))
	for name := range a.tools {
		toolNames = append(toolNames, name)
	}

	toolInput, err := llm.ChooseTool(prompt, toolNames)
	if err != nil {
		return "", err
	}

	tool, ok := a.tools[toolInput.ToolName]
	if !ok {
		return "", fmt.Errorf("Unknown tool: %s", toolInput.ToolName)
	}

	return tool.Execute(toolInput)
}

func JSONToToolExecutionInput(jsonData string) (AgentToolInput, error) {
	var ati AgentToolInput
	err := json.Unmarshal([]byte(jsonData), &ati)
	if err != nil {
		return ati, err
	}
	return ati, nil
}
