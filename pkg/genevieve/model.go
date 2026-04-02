package genevieve

import "context"

type LLM interface {
	Name() string
	Complete(ctx context.Context, prompt string) (string, error)
	Chat(ctx context.Context, messages []Message) (string, error)
}

// Result holds the response from a single provider in a multi-provider query.
type Result struct {
	Response string
	Err      error
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
