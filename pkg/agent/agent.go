package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

type AgentTool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Terminal() bool
	Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}

type ToolExecutor func(context.Context, llm.ToolCall) (json.RawMessage, error)
type ToolMiddleware func(ToolExecutor) ToolExecutor
type EventMiddleware func(llm.EventHandler) llm.EventHandler

type Budget struct {
	MaxInputTokens  int
	MaxOutputTokens int
	MaxTotalTokens  int
	WrapUpReserve   int
}

type RunRequest struct {
	Provider       string
	Messages       []llm.Message
	MaxTurns       int
	MaxTokens      int
	ThinkingEffort llm.ThinkingEffort
	Budget         Budget
	Stream         bool
	OnEvent        llm.EventHandler
	WrapUpPrompt   string
}

type StopReason string

const (
	StopCompleted    StopReason = "completed"
	StopTerminalTool StopReason = "terminal_tool"
	StopTurnLimit    StopReason = "turn_limit"
	StopTokenBudget  StopReason = "token_budget"
)

type Result struct {
	Output         string
	TerminalTool   string
	TerminalOutput json.RawMessage
	Messages       []llm.Message
	Usage          llm.Usage
	Turns          int
	StopReason     StopReason
}

type Agent struct {
	mu              sync.RWMutex
	router          *llm.Router
	tools           map[string]AgentTool
	toolMiddleware  []ToolMiddleware
	eventMiddleware []EventMiddleware
}

func NewAgent(router *llm.Router) *Agent {
	return &Agent{router: router, tools: make(map[string]AgentTool)}
}

func (a *Agent) RegisterTool(tool AgentTool) error {
	if tool == nil {
		return NewNilToolError()
	}
	name := tool.Name()
	if name == "" {
		return NewEmptyToolNameError()
	}
	if !json.Valid(tool.Schema()) {
		return NewToolRegistrationError(name, "schema is not valid JSON")
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil || schema["type"] != "object" {
		return NewToolRegistrationError(name, "schema must describe a JSON object")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.tools[name]; exists {
		return NewDuplicateToolError(name)
	}
	a.tools[name] = tool
	return nil
}

func (a *Agent) TryRegisterTool(tool AgentTool) {
	_ = a.RegisterTool(tool)
}

func (a *Agent) UseTool(middleware ...ToolMiddleware) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolMiddleware = append(a.toolMiddleware, middleware...)
}

func (a *Agent) UseEvents(middleware ...EventMiddleware) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.eventMiddleware = append(a.eventMiddleware, middleware...)
}

func (a *Agent) Run(ctx context.Context, req RunRequest) (Result, error) {
	client, ok := a.router.Get(req.Provider)
	if !ok {
		return Result{}, llm.NewProviderNotFoundError(req.Provider)
	}
	if req.MaxTurns < 1 {
		req.MaxTurns = 8
	}

	a.mu.RLock()
	tools := make(map[string]AgentTool, len(a.tools))
	definitions := make([]llm.ToolDefinition, 0, len(a.tools))
	for name, tool := range a.tools {
		tools[name] = tool
		definitions = append(
			definitions,
			llm.ToolDefinition{
				Name:        name,
				Description: tool.Description(),
				InputSchema: slices.Clone(tool.Schema()),
			},
		)
	}
	slices.SortFunc(definitions, func(x, y llm.ToolDefinition) int {
		if x.Name < y.Name {
			return -1
		}
		if x.Name > y.Name {
			return 1
		}
		return 0
	})
	toolMiddleware := slices.Clone(a.toolMiddleware)
	eventMiddleware := slices.Clone(a.eventMiddleware)
	a.mu.RUnlock()

	handler := req.OnEvent
	if handler == nil {
		handler = func(llm.Event) error { return nil }
	}
	for i := len(eventMiddleware) - 1; i >= 0; i-- {
		handler = eventMiddleware[i](handler)
	}

	messages := slices.Clone(req.Messages)
	result := Result{Messages: messages}
	for turn := 1; turn <= req.MaxTurns; turn++ {
		if budgetReached(result.Usage, req.Budget, true) {
			return a.wrapUp(ctx, client, req, handler, result)
		}
		generation := llm.GenerateRequest{
			Messages:       messages,
			Tools:          definitions,
			ThinkingEffort: req.ThinkingEffort,
			MaxTokens:      req.MaxTokens,
		}
		response, err := generate(ctx, client, generation, req.Stream, handler)
		if err != nil {
			return result, err
		}
		result.Turns = turn
		result.Usage = result.Usage.Add(response.Usage)
		messages = append(
			messages,
			llm.Message{
				Role:      llm.RoleAssistant,
				Content:   response.Text,
				ToolCalls: slices.Clone(response.ToolCalls),
			},
		)
		result.Messages = messages

		if len(response.ToolCalls) == 0 {
			result.Output = response.Text
			result.StopReason = StopCompleted
			return result, nil
		}
		if budgetReached(result.Usage, req.Budget, true) {
			return a.wrapUp(ctx, client, req, handler, result)
		}

		for _, call := range response.ToolCalls {
			tool, exists := tools[call.Name]
			if !exists {
				return result, NewToolNotFoundError(call.Name)
			}
			executor := ToolExecutor(
				func(ctx context.Context, call llm.ToolCall) (json.RawMessage, error) {
					return tool.Execute(ctx, call.Input)
				},
			)
			for i := len(toolMiddleware) - 1; i >= 0; i-- {
				executor = toolMiddleware[i](executor)
			}
			output, execErr := executor(ctx, call)
			if tool.Terminal() {
				if execErr != nil {
					return result, fmt.Errorf("terminal tool %q: %w", call.Name, execErr)
				}
				result.TerminalTool = call.Name
				result.TerminalOutput = slices.Clone(output)
				result.Output = string(output)
				result.StopReason = StopTerminalTool
				return result, nil
			}
			content := string(output)
			if execErr != nil {
				content = execErr.Error()
			}
			messages = append(
				messages,
				llm.Message{
					Role:       llm.RoleTool,
					Content:    content,
					ToolCallID: call.ID,
					ToolName:   call.Name,
					IsError:    execErr != nil,
				},
			)
		}
		result.Messages = messages
	}
	result.StopReason = StopTurnLimit
	return result, NewTurnLimitError(req.MaxTurns)
}

func generate(
	ctx context.Context,
	client llm.LLM,
	req llm.GenerateRequest,
	stream bool,
	handler llm.EventHandler,
) (llm.GenerateResponse, error) {
	if stream {
		return client.Stream(ctx, req, handler)
	}
	return client.Generate(ctx, req)
}

func budgetReached(usage llm.Usage, budget Budget, reserve bool) bool {
	if budget.MaxInputTokens > 0 && usage.InputTokens >= budget.MaxInputTokens {
		return true
	}
	if budget.MaxOutputTokens > 0 && usage.OutputTokens >= budget.MaxOutputTokens {
		return true
	}
	limit := budget.MaxTotalTokens
	if reserve {
		limit -= budget.WrapUpReserve
	}
	return budget.MaxTotalTokens > 0 && usage.Total() >= limit
}

func (a *Agent) wrapUp(
	ctx context.Context,
	client llm.LLM,
	req RunRequest,
	handler llm.EventHandler,
	result Result,
) (Result, error) {
	if req.Budget.WrapUpReserve <= 0 || budgetReached(result.Usage, req.Budget, false) {
		result.StopReason = StopTokenBudget
		return result, NewBudgetExceededError(result.Usage)
	}
	prompt := req.WrapUpPrompt
	if prompt == "" {
		prompt = "The execution budget is exhausted. Do not call tools. Briefly summarize the useful progress and clearly state what remains incomplete."
	}
	messages := slices.Clone(result.Messages)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: prompt})
	response, err := generate(
		ctx,
		client,
		llm.GenerateRequest{
			Messages:       messages,
			ThinkingEffort: req.ThinkingEffort,
			MaxTokens:      req.Budget.WrapUpReserve,
		},
		req.Stream,
		handler,
	)
	if err != nil {
		return result, err
	}
	result.Usage = result.Usage.Add(response.Usage)
	result.Output = response.Text
	result.Messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: response.Text})
	result.StopReason = StopTokenBudget
	return result, nil
}
