package google

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

var _ llm.LLM = Client{}

const defaultModel = "gemini-2.0-flash"

type Client struct {
	client  *genai.Client
	options llm.LLMOptions
}

func NewClient(ctx context.Context, apiKey string, opts ...llm.LLMOption) (*Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, fmt.Errorf("gemini client init: %w", err)
	}
	c := &Client{client: client}
	for _, opt := range opts {
		opt(&c.options)
	}
	if c.options.Model == "" {
		c.options.Model = defaultModel
	}
	return c, nil
}

func (c Client) Name() string { return "gemini" }

func (c Client) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	contents, config, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	response, err := c.client.Models.GenerateContent(ctx, c.options.Model, contents, config)
	if err != nil {
		return llm.GenerateResponse{}, fmt.Errorf("gemini generate: %w", err)
	}
	return geminiResponse(response), nil
}

func (c Client) Stream(ctx context.Context, req llm.GenerateRequest, emit llm.EventHandler) (llm.GenerateResponse, error) {
	contents, config, err := c.params(req)
	if err != nil {
		return llm.GenerateResponse{}, err
	}
	var out llm.GenerateResponse
	for chunk, err := range c.client.Models.GenerateContentStream(ctx, c.options.Model, contents, config) {
		if err != nil {
			return out, fmt.Errorf("gemini stream: %w", err)
		}
		piece := geminiResponse(chunk)
		if piece.Text != "" {
			out.Text += piece.Text
			if err := emit(llm.Event{Type: llm.EventTextDelta, Text: piece.Text}); err != nil {
				return out, err
			}
		}
		for _, call := range piece.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, call)
			call := call
			if err := emit(llm.Event{Type: llm.EventToolCall, ToolCall: &call}); err != nil {
				return out, err
			}
		}
		if piece.Usage.Total() > 0 {
			out.Usage = piece.Usage
		}
		if piece.StopReason != "" {
			out.StopReason = piece.StopReason
		}
	}
	if err := emit(llm.Event{Type: llm.EventUsage, Usage: out.Usage}); err != nil {
		return out, err
	}
	return out, nil
}

func (c Client) params(req llm.GenerateRequest) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	var contents []*genai.Content
	config := &genai.GenerateContentConfig{}
	for _, msg := range req.Messages {
		switch msg.Role {
		case llm.RoleSystem:
			if config.SystemInstruction == nil {
				config.SystemInstruction = &genai.Content{Role: "user"}
			}
			config.SystemInstruction.Parts = append(config.SystemInstruction.Parts, &genai.Part{Text: msg.Content})
		case llm.RoleUser:
			contents = append(contents, &genai.Content{Role: "user", Parts: []*genai.Part{{Text: msg.Content}}})
		case llm.RoleAssistant:
			content := &genai.Content{Role: "model"}
			if msg.Content != "" {
				content.Parts = append(content.Parts, &genai.Part{Text: msg.Content})
			}
			for _, call := range msg.ToolCalls {
				var args map[string]any
				if err := json.Unmarshal(call.Input, &args); err != nil {
					return nil, nil, fmt.Errorf("gemini tool call %q input: %w", call.Name, err)
				}
				content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Name, Args: args}})
			}
			contents = append(contents, content)
		case llm.RoleTool:
			var value any
			if err := json.Unmarshal([]byte(msg.Content), &value); err != nil {
				value = msg.Content
			}
			key := "output"
			if msg.IsError {
				key = "error"
			}
			contents = append(contents, &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: msg.ToolCallID, Name: msg.ToolName, Response: map[string]any{key: value}}}}})
		default:
			return nil, nil, llm.NewInvalidRoleError(msg.Role)
		}
	}
	if len(req.Tools) > 0 {
		tool := &genai.Tool{}
		for _, definition := range req.Tools {
			var schema any
			if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
				return nil, nil, fmt.Errorf("gemini tool %q schema: %w", definition.Name, err)
			}
			tool.FunctionDeclarations = append(tool.FunctionDeclarations, &genai.FunctionDeclaration{Name: definition.Name, Description: definition.Description, ParametersJsonSchema: schema})
		}
		config.Tools = []*genai.Tool{tool}
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.options.MaxTokens
	}
	if maxTokens > 0 {
		config.MaxOutputTokens = int32(maxTokens)
	}
	if req.ThinkingEffort != llm.ThinkingNone {
		level := genai.ThinkingLevelMedium
		if req.ThinkingEffort == llm.ThinkingLow {
			level = genai.ThinkingLevelLow
		}
		if req.ThinkingEffort == llm.ThinkingHigh {
			level = genai.ThinkingLevelHigh
		}
		config.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: level}
	}
	return contents, config, nil
}

func geminiResponse(response *genai.GenerateContentResponse) llm.GenerateResponse {
	out := llm.GenerateResponse{StopReason: llm.StopEndTurn}
	if response == nil {
		return out
	}
	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.Thought {
				continue
			}
			out.Text += part.Text
			if part.FunctionCall != nil {
				input, _ := json.Marshal(part.FunctionCall.Args)
				out.ToolCalls = append(out.ToolCalls, llm.ToolCall{ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Input: input})
			}
		}
		if candidate.FinishReason == genai.FinishReasonMaxTokens {
			out.StopReason = llm.StopMaxTokens
		}
	}
	if len(out.ToolCalls) > 0 {
		out.StopReason = llm.StopToolUse
	}
	if response.UsageMetadata != nil {
		out.Usage = llm.Usage{InputTokens: int(response.UsageMetadata.PromptTokenCount), OutputTokens: int(response.UsageMetadata.CandidatesTokenCount + response.UsageMetadata.ThoughtsTokenCount)}
	}
	return out
}
