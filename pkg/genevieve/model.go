package genevieve

import "encoding/json"

type LLM interface {
	Name() string
	Complete(prompt string) (string, error)
	Chat(messages []Message) (string, error)
	ChooseTool(question string, toolNames []string) (ToolExecutionInput, error)
}

type Message struct {
	Role    RoleType
	Content string
}

type RoleType string

const (
	RoleUser      RoleType = "user"
	RoleAssistant RoleType = "assistant"
	RoleSystem    RoleType = "system"
)

func (r RoleType) IsValid() bool {
	switch r {
	case RoleUser, RoleAssistant, RoleSystem:
		return true
	default:
		return false
	}
}

type ToolExecutionInput struct {
	ToolName  string `json:"tool"`
	ToolInput string `json:"input"`
}

func JSONToToolExecutionInput(jsonData string) (ToolExecutionInput, error) {
	var tei ToolExecutionInput
	err := json.Unmarshal([]byte(jsonData), &tei)
	if err != nil {
		return tei, err
	}
	return tei, nil
}
