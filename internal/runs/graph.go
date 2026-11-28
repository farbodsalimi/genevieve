package runs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"

	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/farbodsalimi/genevieve/pkg/graph"
)

// graphCheckpointer adapts the storage layer to graph.Checkpointer[T]. It is the
// single bridge where generic state T meets the byte-oriented store: Save
// marshals cp.State to JSON; LoadLatest/History unmarshal it back. One instance
// serves every run of a graph because the run ID flows through cp.ThreadID.
//
// Note: T must be JSON round-trippable (exported fields) for persistence to work.
type graphCheckpointer[T any] struct {
	store storage.Store
}

func (c graphCheckpointer[T]) Save(ctx context.Context, cp graph.Checkpoint[T]) error {
	state, err := json.Marshal(cp.State)
	if err != nil {
		return err
	}
	front := nodeIDsToStrings(cp.Frontier)
	now := time.Now().UTC()
	if err := c.store.AppendSnapshot(ctx, storage.Snapshot{
		RunID: cp.ThreadID, Step: cp.Step, Frontier: front, State: state, At: now,
	}); err != nil {
		return err
	}
	// Reflect progress on the run record so List/Get show advancing step+frontier.
	if rec, err := c.store.GetRun(ctx, cp.ThreadID); err == nil {
		rec.Step = cp.Step
		rec.Frontier = front
		rec.UpdatedAt = now
		_ = c.store.PutRun(ctx, rec)
	}
	return nil
}

func (c graphCheckpointer[T]) LoadLatest(
	ctx context.Context,
	threadID string,
) (graph.Checkpoint[T], error) {
	snap, err := c.store.LatestSnapshot(ctx, threadID)
	if err != nil {
		return graph.Checkpoint[T]{}, err
	}
	return c.toCheckpoint(threadID, snap)
}

func (c graphCheckpointer[T]) History(
	ctx context.Context,
	threadID string,
) ([]graph.Checkpoint[T], error) {
	snaps, err := c.store.History(ctx, threadID)
	if err != nil {
		return nil, err
	}
	out := make([]graph.Checkpoint[T], 0, len(snaps))
	for _, snap := range snaps {
		cp, err := c.toCheckpoint(threadID, snap)
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, nil
}

func (c graphCheckpointer[T]) toCheckpoint(
	threadID string,
	snap storage.Snapshot,
) (graph.Checkpoint[T], error) {
	var state T
	if err := json.Unmarshal(snap.State, &state); err != nil {
		return graph.Checkpoint[T]{}, err
	}
	return graph.Checkpoint[T]{
		ThreadID: threadID,
		Frontier: stringsToNodeIDs(snap.Frontier),
		Step:     snap.Step,
		State:    state,
	}, nil
}

// RegisterGraph wires a typed graph under name. build compiles the runner with
// the supplied store-backed checkpointer (pass it to graph.WithCheckpointer,
// ideally with graph.WithCheckpointInterval(1) so every super-step is
// persisted). initialFromJSON decodes a start request body into state T.
//
// T must be JSON round-trippable for persistence and resume to work.
func RegisterGraph[T any, U any](
	m *Manager,
	name string,
	build func(cp graph.Checkpointer[T]) (*graph.Runner[T, U], error),
	initialFromJSON func([]byte) (T, error),
) error {
	runner, err := build(graphCheckpointer[T]{store: m.store})
	if err != nil {
		return err
	}
	topo, err := json.Marshal(runner.Topology())
	if err != nil {
		return err
	}
	r := &graphRunnable[T, U]{runner: runner, initialFromJSON: initialFromJSON}
	return m.register(name, storage.KindGraph, r, topo)
}

type graphRunnable[T any, U any] struct {
	runner          *graph.Runner[T, U]
	initialFromJSON func([]byte) (T, error)
}

func (g *graphRunnable[T, U]) Start(
	ctx context.Context,
	runID string,
	initial jsontext.Value,
) error {
	state, err := g.initialFromJSON(initial)
	if err != nil {
		return err
	}
	_, err = g.runner.RunWithThread(ctx, runID, state)
	return err
}

func (g *graphRunnable[T, U]) Resume(ctx context.Context, runID string) error {
	_, err := g.runner.Resume(ctx, runID)
	return err
}

func nodeIDsToStrings(ids []graph.NodeID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func stringsToNodeIDs(ss []string) []graph.NodeID {
	out := make([]graph.NodeID, len(ss))
	for i, s := range ss {
		out[i] = graph.NodeID(s)
	}
	return out
}
