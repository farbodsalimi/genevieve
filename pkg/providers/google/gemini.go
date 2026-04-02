package google

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

const defaultModel = "gemini-2.0-flash"

type Client struct {
	client  *genai.Client
	options genevieve.LLMOptions
}

func NewClient(ctx context.Context, apiKey string, opts ...genevieve.LLMOption) (*Client, error) {
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

func (c Client) Chat(ctx context.Context, messages []genevieve.Message) (string, error) {
	content := []*genai.Content{}
	for _, msg := range messages {
		content = append(
			content,
			&genai.Content{
				Role:  string(msg.Role),
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
	result, err := c.Chat(ctx, []genevieve.Message{{Role: genevieve.RoleUser, Content: prompt}})
	return result, err
}

func (c Client) ChooseTool(
	ctx context.Context,
	question string,
	toolNames []string,
) (genevieve.AgentToolInput, error) {
	jsonData, err := c.Chat(ctx, []genevieve.Message{
		{
			Role:    genevieve.RoleSystem,
			Content: genevieve.AgentSystemPrompt(),
		},
		{
			Role:    genevieve.RoleUser,
			Content: genevieve.AgentChooseToolPrompt(toolNames, question),
		},
	})
	if err != nil {
		return genevieve.AgentToolInput{}, fmt.Errorf("gemini choose tool: %w", err)
	}

	resp, err := genevieve.JSONToToolExecutionInput(jsonData)
	if err != nil {
		return genevieve.AgentToolInput{}, fmt.Errorf("gemini choose tool: parse response: %w", err)
	}

	return resp, nil
}
