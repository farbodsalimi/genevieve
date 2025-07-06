package openai

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

var defaultModel = openai.ChatModelGPT4o

type Client struct {
	ctx     context.Context
	client  *openai.Client
	options genevieve.LLMOptions
}

func NewClient(ctx context.Context, apiKey string, opts ...genevieve.LLMOption) *Client {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	c := &Client{ctx: ctx, client: &client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = defaultModel
	}
	return c
}

func (c Client) Name() string {
	return "openai"
}

func (c Client) Chat(messages []genevieve.Message) (string, error) {
	var messageParamUnion []openai.ChatCompletionMessageParamUnion

	for _, msg := range messages {
		switch msg.Role {
		case genevieve.RoleUser:
			messageParamUnion = append(messageParamUnion, openai.UserMessage(msg.Content))
		case genevieve.RoleSystem:
			messageParamUnion = append(messageParamUnion, openai.SystemMessage(msg.Content))
		case genevieve.RoleAssistant:
			messageParamUnion = append(messageParamUnion, openai.AssistantMessage(msg.Content))
		}
	}

	chatCompletion, err := c.client.Chat.Completions.New(
		c.ctx,
		openai.ChatCompletionNewParams{
			Messages: messageParamUnion,
			Model:    c.options.Model,
		},
	)
	if err != nil {
		return "", err
	}

	return chatCompletion.Choices[0].Message.Content, nil
}

func (c Client) Complete(prompt string) (string, error) {
	return c.Chat([]genevieve.Message{{Role: genevieve.RoleUser, Content: prompt}})
}

func (c Client) ChooseTool(
	question string,
	toolNames []string,
) (genevieve.AgentToolInput, error) {
	jsonData, err := c.Chat([]genevieve.Message{
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
