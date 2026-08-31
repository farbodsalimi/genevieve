package llm

import (
	"context"
	"encoding/json"
)

// LLM is the provider-neutral structured generation contract. Stream emits
// incremental events and returns the same complete response as Generate.
type LLM interface {
	Name() string
	Generate(context.Context, GenerateRequest) (GenerateResponse, error)
	Stream(context.Context, GenerateRequest, EventHandler) (GenerateResponse, error)
}

type RoleType string

const (
	RoleUser      RoleType = "user"
	RoleAssistant RoleType = "assistant"
	RoleSystem    RoleType = "system"
	RoleTool      RoleType = "tool"
)

func (r RoleType) IsValid() bool {
	switch r {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		return true
	default:
		return false
	}
}

// Message represents text, an assistant tool-call turn, or a tool result.
type Message struct {
	Role       RoleType
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
	IsError    bool
}

type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }
func (u Usage) Add(v Usage) Usage {
	return Usage{InputTokens: u.InputTokens + v.InputTokens, OutputTokens: u.OutputTokens + v.OutputTokens}
}

type ThinkingEffort string

const (
	ThinkingNone   ThinkingEffort = ""
	ThinkingLow    ThinkingEffort = "low"
	ThinkingMedium ThinkingEffort = "medium"
	ThinkingHigh   ThinkingEffort = "high"
)

type GenerateRequest struct {
	Messages       []Message
	Tools          []ToolDefinition
	ThinkingEffort ThinkingEffort
	MaxTokens      int
}

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

type GenerateResponse struct {
	Text       string
	ToolCalls  []ToolCall
	Usage      Usage
	StopReason StopReason
}

type EventType string

const (
	EventTextDelta EventType = "text_delta"
	EventToolCall  EventType = "tool_call"
	EventUsage     EventType = "usage"
)

type Event struct {
	Type     EventType
	Text     string
	ToolCall *ToolCall
	Usage    Usage
}

// EventHandler is synchronous. Returning an error stops stream consumption.
type EventHandler func(Event) error

type LLMOptions struct {
	APIKey    string
	Model     string
	MaxTokens int
}

type LLMOption func(*LLMOptions)

func WithModel(model string) LLMOption { return func(s *LLMOptions) { s.Model = model } }
func WithMaxTokens(n int) LLMOption    { return func(s *LLMOptions) { s.MaxTokens = n } }

type Result struct {
	Response GenerateResponse
	Err      error
}
