package genevieve

type LLM interface {
	Name() string
	Complete(prompt string) (string, error)
	Chat(messages []Message) (string, error)
	ChooseTool(question string, toolNames []string) (AgentToolInput, error)
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
