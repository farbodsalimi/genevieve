package anthropic

import (
	"context"

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

	for _, msg := range messages {
		switch msg.Role {
		case genevieve.RoleUser:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewUserMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		case genevieve.RoleSystem:
		case genevieve.RoleAssistant:
			messageParam = append(
				messageParam,
				anthropic_sdk.NewAssistantMessage(anthropic_sdk.NewTextBlock(msg.Content)),
			)
		}
	}

	message, err := c.client.Messages.New(
		ctx,
		anthropic_sdk.MessageNewParams{
			MaxTokens: 1024,
			Messages:  messageParam,
			Model:     anthropic_sdk.Model(c.options.Model),
		})
	if err != nil {
		panic(err.Error())
	}

	return message.Content[0].Text, err
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
		return genevieve.AgentToolInput{}, err
	}

	resp, err := genevieve.JSONToToolExecutionInput(jsonData)
	if err != nil {
		return genevieve.AgentToolInput{}, err
	}

	return resp, nil
}
