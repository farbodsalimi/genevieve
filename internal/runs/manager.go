// Package runs is the lifecycle layer over internal/storage: it starts, tracks,
// resumes, and cancels runs (graph executions and agent sessions), recording
// their status so the API can list and monitor "running" work. It depends on
// internal/storage, pkg/graph, and pkg/agent — a higher layer than all three,
// with no import cycle (the same shape as pkg/graph/nodes).
package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/farbodsalimi/genevieve/internal/storage"
)

// ErrNotResumable is returned when a resume is requested for a run whose kind
// cannot resume (agents).
var ErrNotResumable = errors.New("runs: this run kind cannot be resumed")

// ErrAlreadyRunning is returned when resuming or starting a run that is already
// executing in this process.
var ErrAlreadyRunning = errors.New("runs: run is already running")

// Runnable is a registered unit of work the Manager can drive by run ID. Start
// runs from an initial input; Resume continues from the store's latest state.
type Runnable interface {
	Start(ctx context.Context, runID string, initial json.RawMessage) error
	Resume(ctx context.Context, runID string) error
}

type registered struct {
	kind     string
	runnable Runnable
	topology json.RawMessage // graphs only; nil for agents
}

// Manager owns the registry of runnables and the set of in-flight cancel funcs.
// It persists run status transitions to the store; progress snapshots are
// written by each runnable (graphs via a store-backed checkpointer, agents via
// callbacks).
type Manager struct {
	store     storage.Store
	mu        sync.Mutex
	runnables map[string]registered         // keyed by name
	cancels   map[string]context.CancelFunc // keyed by runID
}

// NewManager returns a Manager backed by store.
func NewManager(store storage.Store) *Manager {
	return &Manager{
		store:     store,
		runnables: make(map[string]registered),
		cancels:   make(map[string]context.CancelFunc),
	}
}

func (m *Manager) register(name, kind string, r Runnable, topo json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.runnables[name]; exists {
		return fmt.Errorf("runs: %q already registered", name)
	}
	m.runnables[name] = registered{kind: kind, runnable: r, topology: topo}
	return nil
}

// Start creates a run of the named runnable from initial input and launches it
// in the background. It returns the new run ID immediately.
func (m *Manager) Start(ctx context.Context, name string, initial json.RawMessage) (string, error) {
	m.mu.Lock()
	reg, ok := m.runnables[name]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("runs: no runnable named %q", name)
	}

	runID := newID()
	now := time.Now().UTC()
	rec := storage.RunRecord{
		ID: runID, Kind: reg.kind, Name: name,
		Status: storage.StatusRunning, StartedAt: now, UpdatedAt: now,
	}
	if err := m.store.PutRun(ctx, rec); err != nil {
		return "", err
	}
	m.launch(runID, func(c context.Context) error {
		return reg.runnable.Start(c, runID, initial)
	})
	return runID, nil
}

// Resume re-launches a run from its latest stored checkpoint. Agent runs are not
// resumable and return ErrNotResumable.
func (m *Manager) Resume(ctx context.Context, runID string) error {
	rec, err := m.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	reg, ok := m.runnables[rec.Name]
	_, running := m.cancels[runID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("runs: no runnable named %q", rec.Name)
	}
	if reg.kind == storage.KindAgent {
		return ErrNotResumable
	}
	if running {
		return ErrAlreadyRunning
	}

	rec.Status = storage.StatusRunning
	rec.Error = ""
	rec.UpdatedAt = time.Now().UTC()
	if err := m.store.PutRun(ctx, rec); err != nil {
		return err
	}
	m.launch(runID, func(c context.Context) error {
		return reg.runnable.Resume(c, runID)
	})
	return nil
}

// Cancel stops an in-flight run via context cancellation. It errors if the run
// is not currently executing in this process.
func (m *Manager) Cancel(_ context.Context, runID string) error {
	m.mu.Lock()
	cancel, ok := m.cancels[runID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("runs: %q is not running", runID)
	}
	cancel()
	return nil
}

// Get returns a run's record plus its latest snapshot (zero snapshot if none yet).
func (m *Manager) Get(ctx context.Context, runID string) (storage.RunRecord, storage.Snapshot, error) {
	rec, err := m.store.GetRun(ctx, runID)
	if err != nil {
		return storage.RunRecord{}, storage.Snapshot{}, err
	}
	snap, err := m.store.LatestSnapshot(ctx, runID)
	if err != nil {
		var nf *storage.NotFoundError
		if errors.As(err, &nf) {
			return rec, storage.Snapshot{}, nil
		}
		return rec, storage.Snapshot{}, err
	}
	return rec, snap, nil
}

// List returns run records matching the filter.
func (m *Manager) List(ctx context.Context, f storage.Filter) ([]storage.RunRecord, error) {
	return m.store.ListRuns(ctx, f)
}

// History returns all persisted snapshots for a run.
func (m *Manager) History(ctx context.Context, runID string) ([]storage.Snapshot, error) {
	return m.store.History(ctx, runID)
}

// Graphs returns the names of registered graph runnables.
func (m *Manager) Graphs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for name, reg := range m.runnables {
		if reg.kind == storage.KindGraph {
			out = append(out, name)
		}
	}
	return out
}

// Topology returns the cached topology JSON for a registered graph.
func (m *Manager) Topology(name string) (json.RawMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.runnables[name]
	if !ok || reg.topology == nil {
		return nil, false
	}
	return reg.topology, true
}

// launch runs fn in a goroutine under a cancelable context tracked by runID, then
// records the terminal status.
func (m *Manager) launch(runID string, fn func(context.Context) error) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[runID] = cancel
	m.mu.Unlock()

	go func() {
		err := fn(ctx)
		m.mu.Lock()
		delete(m.cancels, runID)
		m.mu.Unlock()
		m.finish(runID, ctx, err)
		cancel()
	}()
}

// finish writes the terminal status for a run. Cancellation wins over a reported
// error, since a canceled context surfaces as an error from the runnable.
func (m *Manager) finish(runID string, ctx context.Context, runErr error) {
	bg := context.Background()
	rec, err := m.store.GetRun(bg, runID)
	if err != nil {
		return
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		rec.Status = storage.StatusCanceled
	case runErr != nil:
		rec.Status = storage.StatusFailed
		rec.Error = runErr.Error()
	default:
		rec.Status = storage.StatusSucceeded
	}
	rec.UpdatedAt = time.Now().UTC()
	_ = m.store.PutRun(bg, rec)
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
