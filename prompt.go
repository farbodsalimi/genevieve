package genevieve

import (
	"encoding/json"
	"fmt"
)

// PromptGenerator defines the interface for prompt and JSON generation.
type PromptGenerator interface {
	GetPrompt() string
	GenerateJSON(v interface{}) ([]byte, error)
}

// SimplePromptGenerator is a basic implementation of PromptGenerator.
type SimplePromptGenerator struct {
	PromptTemplate string
}

// GetPrompt returns the prompt string.
func (s *SimplePromptGenerator) GetPrompt() string {
	return s.PromptTemplate
}

// GenerateJSON generates a JSON representation of the given data.
func (s *SimplePromptGenerator) GenerateJSON(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to generate json: %w", err)
	}
	return data, nil
}

// NewSimplePromptGenerator creates a new SimplePromptGenerator.
func NewSimplePromptGenerator(template string) *SimplePromptGenerator {
	return &SimplePromptGenerator{PromptTemplate: template}
}
