package runs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/farbodsalimi/genevieve/pkg/graph"
)

// buildSlow compiles a single blocking node that respects context cancellation,
// so a run can be canceled mid-flight.
func buildSlow(cp graph.Checkpointer[gstate]) (*graph.Runner[gstate, gupdate], error) {
	slow := graph.NodeFunc[gstate, gupdate](func(ctx context.Context, _ gstate) (gupdate, error) {
		select {
		case <-time.After(5 * time.Second):
			return gupdate{Entry: "slow"}, nil
		case <-ctx.Done():
			return gupdate{}, ctx.Err()
		}
	})
	return graph.NewBuilder(gReducer()).
		AddNode("slow", slow).
		AddNode("done", gNode("done")).
		AddEdge("slow", "done").
		SetEntryPoint("slow").
		SetTerminal("done").
		Compile(graph.WithCheckpointer(cp), graph.WithCheckpointInterval(1))
}

func TestManager_Cancel(t *testing.T) {
	ctx := context.Background()
	m := NewManager(storage.NewMemoryStore())
	if err := RegisterGraph(m, "slow", buildSlow, gInitial); err != nil {
		t.Fatalf("register: %v", err)
	}
	id, err := m.Start(ctx, "slow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Give the goroutine a moment to enter the node, then cancel.
	time.Sleep(50 * time.Millisecond)
	if err := m.Cancel(ctx, id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	rec := waitStatus(t, m, id, storage.StatusCanceled, 2*time.Second)
	if rec.Status != storage.StatusCanceled {
		t.Fatalf("status = %q, want canceled", rec.Status)
	}
}

func TestManager_StartUnknown(t *testing.T) {
	m := NewManager(storage.NewMemoryStore())
	if _, err := m.Start(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected error starting unknown runnable")
	}
}

func TestManager_GraphsAndTopology(t *testing.T) {
	m := NewManager(storage.NewMemoryStore())
	if err := RegisterGraph(m, "demo", buildLinear, gInitial); err != nil {
		t.Fatalf("register: %v", err)
	}
	if names := m.Graphs(); len(names) != 1 || names[0] != "demo" {
		t.Fatalf("Graphs() = %v, want [demo]", names)
	}
	topo, ok := m.Topology("demo")
	if !ok {
		t.Fatal("Topology(demo) missing")
	}
	var parsed graph.Topology
	if err := json.Unmarshal(topo, &parsed); err != nil {
		t.Fatalf("topology JSON: %v", err)
	}
	if parsed.Entry != "a" {
		t.Errorf("topology entry = %q, want a", parsed.Entry)
	}
	if _, ok := m.Topology("missing"); ok {
		t.Error("Topology(missing) should be absent")
	}
}

// dummyRunnable lets us exercise agent-kind resume rejection without a live agent.
type dummyRunnable struct{}

func (dummyRunnable) Start(context.Context, string, json.RawMessage) error { return nil }
func (dummyRunnable) Resume(context.Context, string) error                 { return nil }

func TestManager_ResumeAgentRejected(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	m := NewManager(store)
	if err := m.register("bot", storage.KindAgent, dummyRunnable{}, nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	_ = store.PutRun(ctx, storage.RunRecord{ID: "x", Kind: storage.KindAgent, Name: "bot", Status: storage.StatusSucceeded})

	if err := m.Resume(ctx, "x"); !errors.Is(err, ErrNotResumable) {
		t.Fatalf("Resume agent = %v, want ErrNotResumable", err)
	}
}
