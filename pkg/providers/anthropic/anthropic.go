package anthropic

import (
	"context"

	anthropic_sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

type Client struct {
	ctx    context.Context
	client *anthropic_sdk.Client
}

// Chat implements genevieve.LLM.
func (c Client) Chat(messages []genevieve.Message) (string, error) {
	panic("unimplemented")
}

// Complete implements genevieve.LLM.
func (c Client) Complete(prompt string) (string, error) {
	message, err := c.client.Messages.New(
		c.ctx,
		anthropic_sdk.MessageNewParams{
			MaxTokens: 1024,
			Messages: []anthropic_sdk.MessageParam{
				anthropic_sdk.NewUserMessage(anthropic_sdk.NewTextBlock(prompt)),
			},
			Model: anthropic_sdk.ModelClaudeSonnet4_20250514,
		})
	if err != nil {
		panic(err.Error())
	}

	return message.Content[0].Text, err
}

// Name implements genevieve.LLM.
func (c Client) Name() string {
	return "claude"
}

func NewClient(ctx context.Context, apiKey string) *Client {
	client := anthropic_sdk.NewClient(option.WithAPIKey(apiKey))
	return &Client{client: &client}
}
