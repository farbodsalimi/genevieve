package anthropic

import (
	"context"
	"fmt"

	anthropic_sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

var defaultModel = anthropic_sdk.ModelClaudeSonnet4_20250514

type Client struct {
	client  *anthropic_sdk.Client
	options genevieve.LLMOptions
}

func (c Client) Name() string {
	return "claude"
}

func NewClient(apiKey string, opts ...genevieve.LLMOption) *Client {
	client := anthropic_sdk.NewClient(option.WithAPIKey(apiKey))
	c := &Client{client: &client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = string(defaultModel)
	}
	return c
}

func (c Client) Chat(ctx context.Context, messages []genevieve.Message) (string, error) {
	var messageParam []anthropic_sdk.MessageParam
	var systemBlocks []anthropic_sdk.TextBlockParam

	for _, msg := range messages {
		switch msg.Role {
		case genevieve.RoleUser:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewUserMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		case genevieve.RoleSystem:
			systemBlocks = append(systemBlocks, anthropic_sdk.TextBlockParam{
				Text: msg.Content,
			})
		case genevieve.RoleAssistant:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewAssistantMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		}
	}

	params := anthropic_sdk.MessageNewParams{
		MaxTokens: 1024,
		Messages:  messageParam,
		Model:     anthropic_sdk.Model(c.options.Model),
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	message, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic chat: %w", err)
	}

	if len(message.Content) == 0 {
		return "", fmt.Errorf("anthropic chat: empty response content")
	}

	return message.Content[0].Text, nil
}

func (c Client) Complete(ctx context.Context, prompt string) (string, error) {
	return c.Chat(ctx, []genevieve.Message{{Role: genevieve.RoleUser, Content: prompt}})
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
		return genevieve.AgentToolInput{}, fmt.Errorf("anthropic choose tool: %w", err)
	}

	resp, err := genevieve.JSONToToolExecutionInput(jsonData)
	if err != nil {
		return genevieve.AgentToolInput{}, fmt.Errorf("anthropic choose tool: parse response: %w", err)
	}

	return resp, nil
}
