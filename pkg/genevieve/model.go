package genevieve

import "context"

// TODO: Consider returning structured Result type with metadata instead of plain strings
type LLM interface {
	Name() string
	Complete(ctx context.Context, prompt string) (string, error)
	Chat(ctx context.Context, messages []Message) (string, error)
	ChooseTool(ctx context.Context, question string, toolNames []string) (AgentToolInput, error)
}

type LLMOptions struct {
	APIKey string
	Model  string
}

type LLMOption func(*LLMOptions)

func WithModel(model string) func(*LLMOptions) {
	return func(s *LLMOptions) {
		s.Model = model
	}
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
