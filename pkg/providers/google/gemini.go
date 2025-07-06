package google

import (
	"context"

	"google.golang.org/genai"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.LLM = Client{}

const model = "gemini-2.0-flash"

type Client struct {
	ctx    context.Context
	client *genai.Client
}

func NewClient(ctx context.Context, apiKey string) *Client {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		panic(err)
	}

	return &Client{ctx: ctx, client: client}
}

func (c Client) Name() string {
	return "gemini"
}

func (c Client) Chat(messages []genevieve.Message) (string, error) {
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
		c.ctx,
		model,
		content,
		nil,
	)
	return result.Text(), err
}

func (c Client) Complete(prompt string) (string, error) {
	result, err := c.Chat([]genevieve.Message{{Role: genevieve.RoleUser, Content: prompt}})
	return result, err
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

	resp, err := genevieve.JSONToToolExecutionInput(jsonData)
	if err != nil {
		return genevieve.ToolExecutionInput{}, err
	}

	return resp, nil
}
