package storage

import (
	"context"
	"slices"
	"sync"
)

var _ Store = &MemoryStore{}

// MemoryStore is an in-process Store, the no-dependency default. It follows the
// RWMutex registry pattern used by pkg/llm.Router and graph.MemoryCheckpointer.
type MemoryStore struct {
	mu    sync.RWMutex
	runs  map[string]RunRecord
	snaps map[string][]Snapshot
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:  make(map[string]RunRecord),
		snaps: make(map[string][]Snapshot),
	}
}

func (m *MemoryStore) PutRun(_ context.Context, r RunRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = r
	return nil
}

func (m *MemoryStore) GetRun(_ context.Context, id string) (RunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[id]
	if !ok {
		return RunRecord{}, NewRunNotFoundError(id)
	}
	return r, nil
}

func (m *MemoryStore) ListRuns(_ context.Context, f Filter) ([]RunRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RunRecord, 0, len(m.runs))
	for _, r := range m.runs {
		if f.matches(r) {
			out = append(out, r)
		}
	}
	// Newest first; ID as a stable tiebreaker for deterministic output.
	slices.SortFunc(out, func(a, b RunRecord) int {
		if !a.StartedAt.Equal(b.StartedAt) {
			return b.StartedAt.Compare(a.StartedAt)
		}
		return slices.Compare([]string{a.ID}, []string{b.ID})
	})
	return out, nil
}

func (m *MemoryStore) AppendSnapshot(_ context.Context, s Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snaps[s.RunID] = append(m.snaps[s.RunID], s)
	return nil
}

func (m *MemoryStore) LatestSnapshot(_ context.Context, runID string) (Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.snaps[runID]
	if len(hist) == 0 {
		return Snapshot{}, NewSnapshotNotFoundError(runID)
	}
	return hist[len(hist)-1], nil
}

func (m *MemoryStore) History(_ context.Context, runID string) ([]Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.snaps[runID]
	out := make([]Snapshot, len(hist))
	copy(out, hist)
	return out, nil
}
