package agent

import (
	"fmt"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// ToolNotFoundError is returned when an agent tries to execute a tool that is not registered
type ToolNotFoundError struct {
	ToolName string
}

func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool %q not found", e.ToolName)
}

// NewToolNotFoundError creates a new ToolNotFoundError
func NewToolNotFoundError(name string) *ToolNotFoundError {
	return &ToolNotFoundError{ToolName: name}
}

// ToolRegistrationError is returned when tool registration fails
type ToolRegistrationError struct {
	ToolName string
	Reason   string
}

func (e *ToolRegistrationError) Error() string {
	if e.ToolName != "" {
		return fmt.Sprintf("failed to register tool %q: %s", e.ToolName, e.Reason)
	}
	return fmt.Sprintf("failed to register tool: %s", e.Reason)
}

// NewToolRegistrationError creates a new ToolRegistrationError
func NewToolRegistrationError(name, reason string) *ToolRegistrationError {
	return &ToolRegistrationError{ToolName: name, Reason: reason}
}

// Common tool registration error constructors
func NewNilToolError() *ToolRegistrationError {
	return &ToolRegistrationError{Reason: "cannot register nil tool"}
}

func NewEmptyToolNameError() *ToolRegistrationError {
	return &ToolRegistrationError{Reason: "tool name cannot be empty"}
}

func NewDuplicateToolError(name string) *ToolRegistrationError {
	return &ToolRegistrationError{ToolName: name, Reason: "tool is already registered"}
}

type TurnLimitError struct{ Limit int }

func (e *TurnLimitError) Error() string           { return fmt.Sprintf("agent turn limit %d reached", e.Limit) }
func NewTurnLimitError(limit int) *TurnLimitError { return &TurnLimitError{Limit: limit} }

type BudgetExceededError struct{ Usage llm.Usage }

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("agent token budget exceeded after %d tokens", e.Usage.Total())
}
func NewBudgetExceededError(usage llm.Usage) *BudgetExceededError {
	return &BudgetExceededError{Usage: usage}
}
