package llm

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

// ProviderRegistrationError is returned when provider registration fails
type ProviderRegistrationError struct {
	ProviderName string
	Reason       string
}

func (e *ProviderRegistrationError) Error() string {
	if e.ProviderName != "" {
		return fmt.Sprintf("failed to register provider %q: %s", e.ProviderName, e.Reason)
	}
	return fmt.Sprintf("failed to register provider: %s", e.Reason)
}

func NewNilProviderError() *ProviderRegistrationError {
	return &ProviderRegistrationError{Reason: "cannot register nil provider"}
}

func NewEmptyProviderNameError() *ProviderRegistrationError {
	return &ProviderRegistrationError{Reason: "provider name cannot be empty"}
}

func NewDuplicateProviderError(name string) *ProviderRegistrationError {
	return &ProviderRegistrationError{ProviderName: name, Reason: "provider is already registered"}
}

// InvalidRoleError is returned when a message has an unrecognized role
type InvalidRoleError struct {
	Role string
}

func (e *InvalidRoleError) Error() string {
	return fmt.Sprintf("invalid message role: %q", e.Role)
}

func NewInvalidRoleError(role RoleType) *InvalidRoleError {
	return &InvalidRoleError{Role: string(role)}
}
