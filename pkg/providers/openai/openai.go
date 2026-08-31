package openai

import (
	"context"
	"encoding/json"
	"fmt"

	openai_sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

var _ llm.LLM = Client{}
var defaultModel = openai_sdk.ChatModelGPT4o

type Client struct {
	client  *openai_sdk.Client
	options llm.LLMOptions
}

func NewClient(_ context.Context, apiKey string, opts ...llm.LLMOption) (*Client, error) {
	client := openai_sdk.NewClient(option.WithAPIKey(apiKey))
	c := &Client{client: &client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = defaultModel
	}
	return c, nil
}

func (c Client) Name() string { return "openai" }

func (c Client) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	completion, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return llm.GenerateResponse{}, fmt.Errorf("openai generate: %w", err)
	}
	if len(completion.Choices) == 0 {
		return llm.GenerateResponse{}, fmt.Errorf("openai generate: empty choices")
	}
	return response(completion), nil
}

func (c Client) Stream(ctx context.Context, req llm.GenerateRequest, emit llm.EventHandler) (llm.GenerateResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	params.StreamOptions.IncludeUsage = openai_sdk.Bool(true)
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	acc := openai_sdk.ChatCompletionAccumulator{}
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := emit(llm.Event{Type: llm.EventTextDelta, Text: choice.Delta.Content}); err != nil {
					return llm.GenerateResponse{}, err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return llm.GenerateResponse{}, fmt.Errorf("openai stream: %w", err)
	}
	out := response(&acc.ChatCompletion)
	for i := range out.ToolCalls {
		call := out.ToolCalls[i]
		if err := emit(llm.Event{Type: llm.EventToolCall, ToolCall: &call}); err != nil {
			return llm.GenerateResponse{}, err
		}
	}
	if err := emit(llm.Event{Type: llm.EventUsage, Usage: out.Usage}); err != nil {
		return llm.GenerateResponse{}, err
	}
	return out, nil
}

func (c Client) params(req llm.GenerateRequest) (openai_sdk.ChatCompletionNewParams, error) {
	messages := make([]openai_sdk.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if !msg.Role.IsValid() {
			return openai_sdk.ChatCompletionNewParams{}, llm.NewInvalidRoleError(msg.Role)
		}
		switch msg.Role {
		case llm.RoleSystem:
			messages = append(messages, openai_sdk.SystemMessage(msg.Content))
		case llm.RoleUser:
			messages = append(messages, openai_sdk.UserMessage(msg.Content))
		case llm.RoleTool:
			messages = append(messages, openai_sdk.ToolMessage(msg.Content, msg.ToolCallID))
		case llm.RoleAssistant:
			m := openai_sdk.AssistantMessage(msg.Content)
			for _, call := range msg.ToolCalls {
				m.OfAssistant.ToolCalls = append(m.OfAssistant.ToolCalls, openai_sdk.ChatCompletionMessageToolCallParam{ID: call.ID, Function: openai_sdk.ChatCompletionMessageToolCallFunctionParam{Name: call.Name, Arguments: string(call.Input)}})
			}
			messages = append(messages, m)
		}
	}
	tools := make([]openai_sdk.ChatCompletionToolParam, 0, len(req.Tools))
	for _, tool := range req.Tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return openai_sdk.ChatCompletionNewParams{}, fmt.Errorf("openai tool %q schema: %w", tool.Name, err)
		}
		tools = append(tools, openai_sdk.ChatCompletionToolParam{Function: shared.FunctionDefinitionParam{Name: tool.Name, Description: openai_sdk.String(tool.Description), Parameters: schema}})
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.options.MaxTokens
	}
	p := openai_sdk.ChatCompletionNewParams{Model: c.options.Model, Messages: messages, Tools: tools}
	if maxTokens > 0 {
		p.MaxCompletionTokens = openai_sdk.Int(int64(maxTokens))
	}
	switch req.ThinkingEffort {
	case llm.ThinkingLow:
		p.ReasoningEffort = openai_sdk.ReasoningEffortLow
	case llm.ThinkingMedium:
		p.ReasoningEffort = openai_sdk.ReasoningEffortMedium
	case llm.ThinkingHigh:
		p.ReasoningEffort = openai_sdk.ReasoningEffortHigh
	}
	return p, nil
}

func response(completion *openai_sdk.ChatCompletion) llm.GenerateResponse {
	choice := completion.Choices[0]
	out := llm.GenerateResponse{Text: choice.Message.Content, Usage: llm.Usage{InputTokens: int(completion.Usage.PromptTokens), OutputTokens: int(completion.Usage.CompletionTokens)}, StopReason: llm.StopEndTurn}
	for _, call := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{ID: call.ID, Name: call.Function.Name, Input: json.RawMessage(call.Function.Arguments)})
	}
	if len(out.ToolCalls) > 0 {
		out.StopReason = llm.StopToolUse
	}
	if choice.FinishReason == "length" {
		out.StopReason = llm.StopMaxTokens
	}
	return out
}
