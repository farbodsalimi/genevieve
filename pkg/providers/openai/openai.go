package openai

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

var model = openai.ChatModelGPT4o

type Client struct {
	ctx    context.Context
	client *openai.Client
}

func NewClient(ctx context.Context, apiKey string) *Client {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Client{ctx: ctx, client: &client}
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
			Model:    model,
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
) (genevieve.ToolExecutionInput, error) {
	jsonData, err := c.Chat([]genevieve.Message{
		{
			Role:    genevieve.RoleSystem,
			Content: "You're an agent that chooses the right tool to answer a user's question.",
		},
		{
			Role:    genevieve.RoleUser,
			Content: genevieve.AgentChooseToolPrompt(toolNames, question),
		},
	})
	if err != nil {
		return genevieve.ToolExecutionInput{}, err
	}

	fmt.Println("---------")
	fmt.Println(jsonData)
	fmt.Println("---------")

	resp, err := genevieve.JSONToToolExecutionInput(jsonData)
	if err != nil {
		return genevieve.ToolExecutionInput{}, err
	}

	fmt.Println("---------")
	fmt.Println(resp)
	fmt.Println("---------")

	return resp, nil
}
