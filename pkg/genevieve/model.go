package genevieve

type LLM interface {
	Name() string
	Complete(prompt string) (string, error)
	Chat(messages []Message) (string, error)
}

type Message struct {
	Role    string // "user", "assistant", etc.
	Content string
}
