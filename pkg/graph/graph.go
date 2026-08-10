// Package graph is a generic, domain-free orchestration engine. A workflow is
// modeled explicitly as a state machine: the orchestrator owns the topology and
// each node is confined to interpreting the task at its assigned position. This
// limits the blast radius of a single node's failure and makes each node
// independently testable.
//
// The engine knows nothing about LLMs. The LLM bindings live in
// pkg/graph/nodes
// so this package never imports pkg/llm or pkg/agent.
package graph

import (
	"context"
	"sync"
)

// NodeID identifies a node. A distinct type, not a bare string, so that
// AddEdge(from, to) cannot silently accept transposed or arbitrary string
// variables. The zero value means "no node" and is how a Router signals
// termination.
type NodeID string

// Reducer merges a partial update U into state T, returning a new T.
// Implementations must not mutate currentState, and must be pure: fast,
// synchronous, no I/O. There is deliberately no context parameter.
type Reducer[T any, U any] interface {
	Reduce(currentState T, update U) (T, error)
}

// ReducerFunc adapts an ordinary function to Reducer.
type ReducerFunc[T any, U any] func(T, U) (T, error)

func (f ReducerFunc[T, U]) Reduce(s T, u U) (T, error) { return f(s, u) }

// Node is one discrete unit of work. It receives state by value and returns a
// partial update — it never writes to shared state directly.
type Node[T any, U any] interface {
	Execute(ctx context.Context, state T) (U, error)
}

// NodeFunc adapts an ordinary function to Node.
type NodeFunc[T any, U any] func(ctx context.Context, state T) (U, error)

func (f NodeFunc[T, U]) Execute(ctx context.Context, s T) (U, error) { return f(ctx, s) }

// Router inspects state and names the next node. Return the zero NodeID ("") to
// halt this branch. An error halts the entire run (RouterExecutionError).
type Router[T any] func(ctx context.Context, state T) (NodeID, error)

// FanRouter names a set of distinct next nodes — static-topology branching where
// several different nodes should run in the same super-step. Return nil or an
// empty slice to halt this branch. It cannot spawn N instances of one node; the
// set is deduplicated by the frontier.
type FanRouter[T any] func(ctx context.Context, state T) ([]NodeID, error)

// Builder accumulates a topology. Methods are chainable and record errors
// internally, surfaced by Compile, so callers write one fluent chain instead of
// checking six error returns.
type Builder[T any, U any] struct {
	mu       sync.RWMutex
	nodes    map[NodeID]Node[T, U]
	order    []NodeID // insertion order, for deterministic diagnostics
	edges    map[NodeID][]NodeID
	conds    map[NodeID]Router[T]
	fans     map[NodeID]FanRouter[T]
	terminal map[NodeID]bool
	entry    NodeID
	reducer  Reducer[T, U]
	errs     []error
}

// NewBuilder returns a Builder using r to merge node updates into state.
func NewBuilder[T any, U any](r Reducer[T, U]) *Builder[T, U] {
	return &Builder[T, U]{
		nodes:    make(map[NodeID]Node[T, U]),
		edges:    make(map[NodeID][]NodeID),
		conds:    make(map[NodeID]Router[T]),
		fans:     make(map[NodeID]FanRouter[T]),
		terminal: make(map[NodeID]bool),
		reducer:  r,
	}
}

// AddNode registers a node under id. An empty id or a duplicate is recorded and
// surfaced by Compile.
func (b *Builder[T, U]) AddNode(id NodeID, n Node[T, U]) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == "" {
		b.errs = append(b.errs, NewCompileError("node ID cannot be empty"))
		return b
	}
	if n == nil {
		b.errs = append(b.errs, NewCompileError("node "+string(id)+" is nil"))
		return b
	}
	if _, exists := b.nodes[id]; exists {
		b.errs = append(b.errs, NewDuplicateNodeError(id))
		return b
	}
	b.nodes[id] = n
	b.order = append(b.order, id)
	return b
}

// AddEdge adds an unconditional static edge from -> to.
func (b *Builder[T, U]) AddEdge(from, to NodeID) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.edges[from] = append(b.edges[from], to)
	return b
}

// AddConditionalEdge attaches a single-target router to a node.
func (b *Builder[T, U]) AddConditionalEdge(from NodeID, r Router[T]) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.conds[from]; exists {
		b.errs = append(
			b.errs,
			NewCompileError("node "+string(from)+" already has a conditional router"),
		)
		return b
	}
	b.conds[from] = r
	return b
}

// AddFanEdge attaches a multi-target router to a node.
func (b *Builder[T, U]) AddFanEdge(from NodeID, r FanRouter[T]) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.fans[from]; exists {
		b.errs = append(b.errs, NewCompileError("node "+string(from)+" already has a fan router"))
		return b
	}
	b.fans[from] = r
	return b
}

// SetEntryPoint names the node the run starts from.
func (b *Builder[T, U]) SetEntryPoint(id NodeID) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entry = id
	return b
}

// SetTerminal marks nodes that end a branch. A terminal node contributes nothing
// to the next frontier.
func (b *Builder[T, U]) SetTerminal(ids ...NodeID) *Builder[T, U] {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		b.terminal[id] = true
	}
	return b
}
