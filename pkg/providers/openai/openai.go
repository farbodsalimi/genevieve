package openai

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

type Client struct {
	ctx    context.Context
	client *openai.Client
}

// Chat implements genevieve.LLM.
func (c Client) Chat(messages []genevieve.Message) (string, error) {
	panic("unimplemented")
}

// Complete implements genevieve.LLM.
func (c Client) Complete(prompt string) (string, error) {
	chatCompletion, err := c.client.Chat.Completions.New(
		c.ctx,
		openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage(prompt),
			},
			Model: openai.ChatModelGPT4o,
		},
	)
	if err != nil {
		panic(err.Error())
	}

	return chatCompletion.Choices[0].Message.Content, err
}

// Name implements genevieve.LLM.
func (c Client) Name() string {
	return "openai"
}

func NewClient(ctx context.Context, apiKey string) *Client {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Client{ctx: ctx, client: &client}
}
