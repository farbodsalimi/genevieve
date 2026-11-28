package runs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/farbodsalimi/genevieve/pkg/agent"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

// RegisterAgent wires an agent under name as a run kind. reqFromJSON decodes a
// start request body into an agent.RunRequest. Agent runs persist incremental
// progress via streaming callbacks and a final snapshot of the Result; they are
// not checkpoint-resumable (Resume returns ErrNotResumable).
func RegisterAgent(
	m *Manager,
	name string,
	a *agent.Agent,
	reqFromJSON func([]byte) (agent.RunRequest, error),
) error {
	r := &agentRunnable{store: m.store, agent: a, reqFromJSON: reqFromJSON}
	return m.register(name, storage.KindAgent, r, nil)
}

type agentRunnable struct {
	store       storage.Store
	agent       *agent.Agent
	reqFromJSON func([]byte) (agent.RunRequest, error)
}

func (ar *agentRunnable) Start(ctx context.Context, runID string, initial json.RawMessage) error {
	req, err := ar.reqFromJSON(initial)
	if err != nil {
		return err
	}

	// Persist incremental progress: snapshot accumulating text and each tool call
	// as the agent streams, preserving any caller-supplied callbacks.
	step := 0
	var text strings.Builder
	base := req.Callbacks
	req.Stream = true
	req.Callbacks = agent.Callbacks{
		OnText: func(s string) error {
			text.WriteString(s)
			step++
			ar.snapshot(ctx, runID, step, map[string]string{"type": "text", "text": text.String()})
			if base.OnText != nil {
				return base.OnText(s)
			}
			return nil
		},
		OnToolCall: func(tc llm.ToolCall) error {
			step++
			ar.snapshot(ctx, runID, step, map[string]string{"type": "tool_call", "name": tc.Name})
			if base.OnToolCall != nil {
				return base.OnToolCall(tc)
			}
			return nil
		},
		OnUsage: base.OnUsage,
	}

	res, err := ar.agent.Run(ctx, req)
	if err != nil {
		return err
	}
	step++
	ar.snapshot(ctx, runID, step, res)
	return nil
}

// Resume is unsupported: an agent turn cannot continue from a mid-run snapshot.
func (ar *agentRunnable) Resume(context.Context, string) error {
	return ErrNotResumable
}

func (ar *agentRunnable) snapshot(ctx context.Context, runID string, step int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	_ = ar.store.AppendSnapshot(ctx, storage.Snapshot{RunID: runID, Step: step, State: b, At: now})
	if rec, err := ar.store.GetRun(ctx, runID); err == nil {
		rec.Step = step
		rec.UpdatedAt = now
		_ = ar.store.PutRun(ctx, rec)
	}
}
