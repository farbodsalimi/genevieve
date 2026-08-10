package graph

import (
	"context"
	"fmt"
	"sync"
)

// Checkpoint is a snapshot of run state after a super-step.
type Checkpoint[T any] struct {
	ThreadID string
	NodeID   NodeID
	Step     int
	State    T
}

// Checkpointer persists run state. The in-memory implementation ships here;
// Redis/Postgres are the caller's problem.
type Checkpointer[T any] interface {
	Save(ctx context.Context, cp Checkpoint[T]) error
	LoadLatest(ctx context.Context, threadID string) (Checkpoint[T], error)
	History(ctx context.Context, threadID string) ([]Checkpoint[T], error)
}

// CheckpointNotFoundError is returned when a thread has no saved checkpoints.
type CheckpointNotFoundError struct {
	ThreadID string
}

func (e *CheckpointNotFoundError) Error() string {
	return fmt.Sprintf("checkpoint: no checkpoint for thread %q", e.ThreadID)
}

func NewCheckpointNotFoundError(threadID string) *CheckpointNotFoundError {
	return &CheckpointNotFoundError{ThreadID: threadID}
}

// MemoryCheckpointer retains at most maxHistory checkpoints per thread,
// discarding oldest first. Zero means unbounded. It follows the RWMutex registry
// pattern from genevieve's router.
type MemoryCheckpointer[T any] struct {
	mu         sync.RWMutex
	maxHistory int
	threads    map[string][]Checkpoint[T]
}

// NewMemoryCheckpointer returns an in-memory Checkpointer bounded to maxHistory
// entries per thread (0 = unbounded).
func NewMemoryCheckpointer[T any](maxHistory int) *MemoryCheckpointer[T] {
	return &MemoryCheckpointer[T]{
		maxHistory: maxHistory,
		threads:    make(map[string][]Checkpoint[T]),
	}
}

func (m *MemoryCheckpointer[T]) Save(ctx context.Context, cp Checkpoint[T]) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	hist := append(m.threads[cp.ThreadID], cp)
	if m.maxHistory > 0 && len(hist) > m.maxHistory {
		hist = hist[len(hist)-m.maxHistory:]
	}
	m.threads[cp.ThreadID] = hist
	return nil
}

func (m *MemoryCheckpointer[T]) LoadLatest(ctx context.Context, threadID string) (Checkpoint[T], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.threads[threadID]
	if len(hist) == 0 {
		return Checkpoint[T]{}, NewCheckpointNotFoundError(threadID)
	}
	return hist[len(hist)-1], nil
}

func (m *MemoryCheckpointer[T]) History(ctx context.Context, threadID string) ([]Checkpoint[T], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hist := m.threads[threadID]
	out := make([]Checkpoint[T], len(hist))
	copy(out, hist)
	return out, nil
}
