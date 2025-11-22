package genevieve

import "fmt"

// ProviderNotFoundError is returned when a requested provider is not registered in the router
type ProviderNotFoundError struct {
	ProviderName string
}

func (e *ProviderNotFoundError) Error() string {
	return fmt.Sprintf("provider %q not found", e.ProviderName)
}

// NewProviderNotFoundError creates a new ProviderNotFoundError
func NewProviderNotFoundError(name string) *ProviderNotFoundError {
	return &ProviderNotFoundError{ProviderName: name}
}

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
