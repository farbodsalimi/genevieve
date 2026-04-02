package openai

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

var defaultModel = openai.ChatModelGPT4o

type Client struct {
	client  *openai.Client
	options genevieve.LLMOptions
}

func NewClient(ctx context.Context, apiKey string, opts ...genevieve.LLMOption) (*Client, error) {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	c := &Client{client: &client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = defaultModel
	}
	return c, nil
}

func (c Client) Name() string {
	return "openai"
}

func (c Client) Chat(ctx context.Context, messages []genevieve.Message) (string, error) {
	var messageParamUnion []openai.ChatCompletionMessageParamUnion

	for _, msg := range messages {
		switch msg.Role {
		case genevieve.RoleUser:
			messageParamUnion = append(messageParamUnion, openai.UserMessage(msg.Content))
		case genevieve.RoleSystem:
			messageParamUnion = append(messageParamUnion, openai.SystemMessage(msg.Content))
		case genevieve.RoleAssistant:
			messageParamUnion = append(messageParamUnion, openai.AssistantMessage(msg.Content))
		default:
			return "", fmt.Errorf("openai chat: %w", genevieve.NewInvalidRoleError(msg.Role))
		}
	}

	chatCompletion, err := c.client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Messages: messageParamUnion,
			Model:    c.options.Model,
		},
	)
	if err != nil {
		return "", fmt.Errorf("openai chat: %w", err)
	}

	if len(chatCompletion.Choices) == 0 {
		return "", fmt.Errorf("openai chat: empty choices in response")
	}

	return chatCompletion.Choices[0].Message.Content, nil
}

func (c Client) Complete(ctx context.Context, prompt string) (string, error) {
	return c.Chat(ctx, []genevieve.Message{{Role: genevieve.RoleUser, Content: prompt}})
}
