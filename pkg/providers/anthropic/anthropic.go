package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	SDK "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

var _ llm.LLM = Client{}
var defaultModel = SDK.ModelClaudeSonnet5

type Client struct {
	client  *SDK.Client
	options llm.LLMOptions
}

func NewClient(_ context.Context, apiKey string, opts ...llm.LLMOption) (*Client, error) {
	client := SDK.NewClient(option.WithAPIKey(apiKey))
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

func (c Client) Name() string { return "claude" }

func (c Client) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	message, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return llm.GenerateResponse{}, fmt.Errorf("anthropic generate: %w", err)
	}
	return anthropicResponse(message), nil
}

func (c Client) Stream(ctx context.Context, req llm.GenerateRequest, emit llm.EventHandler) (llm.GenerateResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	stream := c.client.Messages.NewStreaming(ctx, params)
	var message SDK.Message
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return llm.GenerateResponse{}, err
		}
		if delta, ok := event.AsAny().(SDK.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsAny().(SDK.TextDelta); ok && text.Text != "" {
				if err := emit(llm.Event{Type: llm.EventTextDelta, Text: text.Text}); err != nil {
					return llm.GenerateResponse{}, err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return llm.GenerateResponse{}, fmt.Errorf("anthropic stream: %w", err)
	}
	out := anthropicResponse(&message)
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

func (c Client) params(req llm.GenerateRequest) (SDK.MessageNewParams, error) {
	var messages []SDK.MessageParam
	var system []SDK.TextBlockParam
	for _, msg := range req.Messages {
		switch msg.Role {
		case llm.RoleSystem:
			system = append(system, SDK.TextBlockParam{Text: msg.Content})
		case llm.RoleUser:
			messages = append(messages, SDK.NewUserMessage(SDK.NewTextBlock(msg.Content)))
		case llm.RoleTool:
			messages = append(messages, SDK.NewUserMessage(SDK.NewToolResultBlock(msg.ToolCallID, msg.Content, msg.IsError)))
		case llm.RoleAssistant:
			blocks := []SDK.ContentBlockParamUnion{}
			if msg.Content != "" {
				blocks = append(blocks, SDK.NewTextBlock(msg.Content))
			}
			for _, call := range msg.ToolCalls {
				var input any
				if err := json.Unmarshal(call.Input, &input); err != nil {
					return SDK.MessageNewParams{}, fmt.Errorf("anthropic tool call %q input: %w", call.Name, err)
				}
				blocks = append(blocks, SDK.NewToolUseBlock(call.ID, input, call.Name))
			}
			messages = append(messages, SDK.NewAssistantMessage(blocks...))
		default:
			return SDK.MessageNewParams{}, llm.NewInvalidRoleError(msg.Role)
		}
	}
	tools := make([]SDK.ToolUnionParam, 0, len(req.Tools))
	for _, tool := range req.Tools {
		var schema SDK.ToolInputSchemaParam
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return SDK.MessageNewParams{}, fmt.Errorf("anthropic tool %q schema: %w", tool.Name, err)
		}
		t := SDK.ToolUnionParamOfTool(schema, tool.Name)
		t.OfTool.Description = SDK.String(tool.Description)
		tools = append(tools, t)
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.options.MaxTokens
	}
	p := SDK.MessageNewParams{MaxTokens: int64(maxTokens), Messages: messages, Model: SDK.Model(c.options.Model), System: system, Tools: tools}
	if req.ThinkingEffort != llm.ThinkingNone {
		p.Thinking = SDK.ThinkingConfigParamUnion{OfAdaptive: &SDK.ThinkingConfigAdaptiveParam{}}
	}
	return p, nil
}

func anthropicResponse(message *SDK.Message) llm.GenerateResponse {
	out := llm.GenerateResponse{Usage: llm.Usage{InputTokens: int(message.Usage.InputTokens), OutputTokens: int(message.Usage.OutputTokens)}, StopReason: llm.StopReason(message.StopReason)}
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			out.Text += block.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, llm.ToolCall{ID: block.ID, Name: block.Name, Input: block.Input})
		}
	}
	return out
}
