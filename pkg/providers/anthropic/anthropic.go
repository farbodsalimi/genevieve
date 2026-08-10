package anthropic

import (
	"context"
	"fmt"

	anthropic_sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

var _ llm.LLM = Client{}

var defaultModel = anthropic_sdk.ModelClaudeSonnet4_20250514

type Client struct {
	client  *anthropic_sdk.Client
	options llm.LLMOptions
}

func (c Client) Name() string {
	return "claude"
}

func NewClient(ctx context.Context, apiKey string, opts ...llm.LLMOption) (*Client, error) {
	client := anthropic_sdk.NewClient(option.WithAPIKey(apiKey))
	c := &Client{client: &client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = string(defaultModel)
	}
	if c.options.MaxTokens == 0 {
		c.options.MaxTokens = 1024
	}
	return c, nil
}

func (c Client) Chat(ctx context.Context, messages []llm.Message) (string, error) {
	var messageParam []anthropic_sdk.MessageParam
	var systemBlocks []anthropic_sdk.TextBlockParam

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewUserMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		case llm.RoleSystem:
			systemBlocks = append(systemBlocks, anthropic_sdk.TextBlockParam{
				Text: msg.Content,
			})
		case llm.RoleAssistant:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewAssistantMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		default:
			return "", fmt.Errorf("anthropic chat: %w", llm.NewInvalidRoleError(msg.Role))
		}
	}

	params := anthropic_sdk.MessageNewParams{
		MaxTokens: int64(c.options.MaxTokens),
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
	return c.Chat(ctx, []llm.Message{{Role: llm.RoleUser, Content: prompt}})
}
