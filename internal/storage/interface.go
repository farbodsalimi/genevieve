// Package storage persists the lifecycle and state of runs — graph executions
// and agent sessions — behind one byte-oriented interface. State is carried as
// opaque JSON so a single store and HTTP surface serve every graph type and
// agents uniformly; generics stay out of this layer entirely.
//
// Two implementations ship: MemoryStore (the no-dependency default, used in
// tests) and RedisStore. A DynamoDB implementation can be added later behind the
// same Store interface.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Status is the lifecycle state of a run.
type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Kind distinguishes what is being run.
const (
	KindGraph = "graph"
	KindAgent = "agent"
)

// RunRecord is the metadata for one run: enough to list and monitor it without
// deserializing state. Frontier is populated for graph runs (the active node
// set) and empty for agents.
type RunRecord struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // KindGraph | KindAgent
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Step      int       `json:"step"`
	Frontier  []string  `json:"frontier,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Snapshot is one persisted point in a run's history: the serialized state at a
// step. For graphs it also carries the Frontier needed to rebuild a
// graph.Checkpoint on resume.
type Snapshot struct {
	RunID    string          `json:"runId"`
	Step     int             `json:"step"`
	Frontier []string        `json:"frontier,omitempty"`
	State    json.RawMessage `json:"state"`
	At       time.Time       `json:"at"`
}

// Filter narrows ListRuns. A zero field means "no constraint on this field".
type Filter struct {
	Kind   string
	Status Status
}

// Store persists run metadata and state snapshots. Implementations must be safe
// for concurrent use.
type Store interface {
	PutRun(ctx context.Context, r RunRecord) error
	GetRun(ctx context.Context, id string) (RunRecord, error)
	ListRuns(ctx context.Context, f Filter) ([]RunRecord, error)

	AppendSnapshot(ctx context.Context, s Snapshot) error
	LatestSnapshot(ctx context.Context, runID string) (Snapshot, error)
	History(ctx context.Context, runID string) ([]Snapshot, error)
}

// NotFoundError is returned when a run or snapshot does not exist.
type NotFoundError struct {
	Kind string // "run" | "snapshot"
	ID   string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("storage: no %s for id %q", e.Kind, e.ID)
}

// NewRunNotFoundError reports a missing run.
func NewRunNotFoundError(id string) *NotFoundError {
	return &NotFoundError{Kind: "run", ID: id}
}

// NewSnapshotNotFoundError reports a run with no snapshots.
func NewSnapshotNotFoundError(runID string) *NotFoundError {
	return &NotFoundError{Kind: "snapshot", ID: runID}
}

// matches reports whether r satisfies the filter.
func (f Filter) matches(r RunRecord) bool {
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.Status != "" && r.Status != f.Status {
		return false
	}
	return true
}
