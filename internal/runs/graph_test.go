package runs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/farbodsalimi/genevieve/pkg/graph"
)

// --- a tiny JSON-round-trippable test graph ---

type gstate struct {
	Log []string `json:"log"`
}

type gupdate struct {
	Entry string
}

func gReducer() graph.Reducer[gstate, gupdate] {
	return graph.ReducerFunc[gstate, gupdate](func(s gstate, u gupdate) (gstate, error) {
		log := make([]string, len(s.Log), len(s.Log)+1)
		copy(log, s.Log)
		if u.Entry != "" {
			log = append(log, u.Entry)
		}
		return gstate{Log: log}, nil
	})
}

func gNode(name string) graph.Node[gstate, gupdate] {
	return graph.NodeFunc[gstate, gupdate](func(_ context.Context, _ gstate) (gupdate, error) {
		return gupdate{Entry: name}, nil
	})
}

// buildLinear compiles a -> b -> c with the store checkpointer at interval 1.
func buildLinear(cp graph.Checkpointer[gstate]) (*graph.Runner[gstate, gupdate], error) {
	return graph.NewBuilder(gReducer()).
		AddNode("a", gNode("a")).
		AddNode("b", gNode("b")).
		AddNode("c", gNode("c")).
		AddEdge("a", "b").
		AddEdge("b", "c").
		SetEntryPoint("a").
		SetTerminal("c").
		Compile(graph.WithCheckpointer(cp), graph.WithCheckpointInterval(1))
}

func gInitial(b []byte) (gstate, error) {
	var s gstate
	if len(b) == 0 {
		return s, nil
	}
	return s, json.Unmarshal(b, &s)
}

// waitStatus polls until the run reaches want or the deadline passes.
func waitStatus(t *testing.T, m *Manager, id string, want storage.Status, d time.Duration) storage.RunRecord {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		rec, _, err := m.Get(context.Background(), id)
		if err == nil && rec.Status == want {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec, _, _ := m.Get(context.Background(), id)
	t.Fatalf("run %s did not reach %q in %s (last: %+v)", id, want, d, rec)
	return storage.RunRecord{}
}

func TestRegisterGraph_StartSucceeds(t *testing.T) {
	ctx := context.Background()
	m := NewManager(storage.NewMemoryStore())
	if err := RegisterGraph(m, "demo", buildLinear, gInitial); err != nil {
		t.Fatalf("register: %v", err)
	}

	id, err := m.Start(ctx, "demo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	rec := waitStatus(t, m, id, storage.StatusSucceeded, 2*time.Second)
	if rec.Step != 3 {
		t.Errorf("final step = %d, want 3", rec.Step)
	}

	_, snap, _ := m.Get(ctx, id)
	var st gstate
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(st.Log) != 3 || st.Log[0] != "a" || st.Log[2] != "c" {
		t.Errorf("final state log = %v, want [a b c]", st.Log)
	}
	hist, _ := m.History(ctx, id)
	if len(hist) != 3 {
		t.Errorf("history = %d snapshots, want 3", len(hist))
	}
}

func TestGraphCheckpointer_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	cp := graphCheckpointer[gstate]{store: store}

	in := graph.Checkpoint[gstate]{
		ThreadID: "r1",
		Frontier: []graph.NodeID{"b", "c"},
		Step:     2,
		State:    gstate{Log: []string{"a"}},
	}
	// Save needs a run record present to update; create one.
	_ = store.PutRun(ctx, storage.RunRecord{ID: "r1", Kind: storage.KindGraph, Name: "demo", Status: storage.StatusRunning})
	if err := cp.Save(ctx, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := cp.LoadLatest(ctx, "r1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Step != 2 || got.ThreadID != "r1" {
		t.Errorf("checkpoint meta mismatch: %+v", got)
	}
	if len(got.Frontier) != 2 || got.Frontier[0] != "b" || got.Frontier[1] != "c" {
		t.Errorf("frontier = %v, want [b c]", got.Frontier)
	}
	if len(got.State.Log) != 1 || got.State.Log[0] != "a" {
		t.Errorf("state = %v, want log [a]", got.State.Log)
	}
}
