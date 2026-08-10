package google

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

var _ llm.LLM = Client{}

const defaultModel = "gemini-2.0-flash"

type Client struct {
	client  *genai.Client
	options llm.LLMOptions
}

func NewClient(ctx context.Context, apiKey string, opts ...llm.LLMOption) (*Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini client init: %w", err)
	}

	c := &Client{client: client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = defaultModel
	}
	return c, nil
}

func (c Client) Name() string {
	return "gemini"
}

func (c Client) Chat(ctx context.Context, messages []llm.Message) (string, error) {
	var content []*genai.Content
	for _, msg := range messages {
		var role string
		switch msg.Role {
		case llm.RoleUser, llm.RoleSystem:
			role = "user"
		case llm.RoleAssistant:
			role = "model"
		default:
			return "", fmt.Errorf("gemini chat: %w", llm.NewInvalidRoleError(msg.Role))
		}
		content = append(
			content,
			&genai.Content{
				Role:  role,
				Parts: []*genai.Part{{Text: msg.Content}},
			},
		)
	}
	result, err := c.client.Models.GenerateContent(
		ctx,
		c.options.Model,
		content,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("gemini chat: %w", err)
	}

	return result.Text(), nil
}

func (c Client) Complete(ctx context.Context, prompt string) (string, error) {
	return c.Chat(ctx, []llm.Message{{Role: llm.RoleUser, Content: prompt}})
}
