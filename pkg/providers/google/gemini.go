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

// Chat implements genevieve.LLM.
func (c Client) Chat(messages []genevieve.Message) (string, error) {
	content := []*genai.Content{}
	for _, msg := range messages {
		content = append(
			content,
			&genai.Content{
				Role:  msg.Role,
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

// Complete implements genevieve.LLM.
func (c Client) Complete(prompt string) (string, error) {
	result, err := c.client.Models.GenerateContent(
		c.ctx,
		model,
		[]*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: prompt}}}},
		nil,
	)
	return result.Text(), err
}

// Name implements genevieve.LLM.
func (c Client) Name() string {
	return "gemini"
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
