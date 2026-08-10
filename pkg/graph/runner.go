package graph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"runtime/debug"
	"sort"

	"golang.org/x/sync/errgroup"
)

// Runner is an immutable compiled topology, safe for concurrent reuse across
// requests.
type Runner[T any, U any] struct {
	nodes    map[NodeID]Node[T, U]
	edges    map[NodeID][]NodeID
	conds    map[NodeID]Router[T]
	fans     map[NodeID]FanRouter[T]
	terminal map[NodeID]bool
	entry    NodeID
	reducer  Reducer[T, U]

	recursionLimit     int
	maxParallel        int
	checkpointInterval int
	cloner             func(T) T
	checkpointer       Checkpointer[T]
	acyclic            bool
}

// IsAcyclic reports whether the static-edge subgraph contains no cycles. Useful
// for callers who want to assert a workflow can't loop.
func (r *Runner[T, U]) IsAcyclic() bool { return r.acyclic }

// Run executes the graph from initial state until the frontier empties or an
// error occurs. A fresh thread ID is generated for checkpointing.
func (r *Runner[T, U]) Run(ctx context.Context, initial T) (T, error) {
	return r.RunWithThread(ctx, newThreadID(), initial)
}

// RunWithThread is Run with a caller-supplied thread ID, so checkpoints for this
// run can be located and resumed later.
func (r *Runner[T, U]) RunWithThread(ctx context.Context, threadID string, initial T) (T, error) {
	return r.runLoop(ctx, threadID, initial, []NodeID{r.entry}, 0)
}

// runLoop drives super-steps from a starting state, frontier, and step counter.
// Run starts at the entry with step 0; Resume starts past a checkpoint.
func (r *Runner[T, U]) runLoop(ctx context.Context, threadID string, initial T, frontier []NodeID, startStep int) (T, error) {
	state := initial
	step := startStep

	for len(frontier) > 0 {
		step++
		if step > r.recursionLimit {
			return state, NewRecursionLimitError(r.recursionLimit, step, frontier)
		}

		newState, lastID, err := r.runSuperStep(ctx, state, frontier, step)
		if err != nil {
			return state, err
		}
		state = newState

		if r.checkpointer != nil && step%r.checkpointInterval == 0 {
			if err := r.checkpointer.Save(ctx, Checkpoint[T]{
				ThreadID: threadID,
				NodeID:   lastID,
				Step:     step,
				State:    state,
			}); err != nil {
				return state, err
			}
		}

		next, err := r.nextFrontier(ctx, state, frontier, step)
		if err != nil {
			return state, err
		}
		frontier = next
	}
	return state, nil
}

// runSuperStep runs the whole frontier in parallel, then applies reducers
// sequentially in deterministic (node-ID-sorted) order. Returns the new state
// and the ID of the last-reduced node (for checkpoint metadata).
func (r *Runner[T, U]) runSuperStep(ctx context.Context, state T, frontier []NodeID, step int) (T, NodeID, error) {
	type slot struct {
		id     NodeID
		update U
	}
	results := make([]slot, len(frontier))

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(r.maxParallel)

	for i, id := range frontier {
		node := r.nodes[id]
		eg.Go(func() (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					err = NewNodePanicError(id, step, rec, debug.Stack())
				}
			}()
			// snapshot inside the guard so a panicking cloner is contained too.
			snapshot := r.snapshot(state)
			u, execErr := node.Execute(gctx, snapshot)
			if execErr != nil {
				return NewNodeExecutionError(id, step, execErr)
			}
			results[i] = slot{id: id, update: u}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return state, "", err
	}

	// Sort by node ID so fan-in is deterministic and testable.
	sort.Slice(results, func(a, b int) bool { return results[a].id < results[b].id })

	var lastID NodeID
	for _, res := range results {
		merged, err := r.reduceGuarded(state, res.update)
		if err != nil {
			return state, "", NewReducerError(res.id, err)
		}
		state = merged
		lastID = res.id
	}
	return state, lastID, nil
}

// reduceGuarded runs the reducer with panic containment on the runner's own
// goroutine.
func (r *Runner[T, U]) reduceGuarded(state T, update U) (out T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = NewNodePanicError("", 0, rec, debug.Stack())
		}
	}()
	return r.reducer.Reduce(state, update)
}

// nextFrontier computes the frontier for the following super-step from the new
// state: terminal nodes contribute nothing; a node with static edges follows
// them; a node with a router invokes it. Duplicates are removed.
func (r *Runner[T, U]) nextFrontier(ctx context.Context, state T, frontier []NodeID, step int) ([]NodeID, error) {
	seen := make(map[NodeID]bool)
	var next []NodeID
	add := func(id NodeID) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		next = append(next, id)
	}

	for _, id := range frontier {
		if r.terminal[id] {
			continue
		}
		if tos, ok := r.edges[id]; ok {
			for _, to := range tos {
				add(to)
			}
			continue
		}
		if router, ok := r.conds[id]; ok {
			target, err := r.routeGuarded(ctx, router, state)
			if err != nil {
				return nil, NewRouterExecutionError(id, step, err)
			}
			add(target)
			continue
		}
		if fan, ok := r.fans[id]; ok {
			targets, err := r.fanGuarded(ctx, fan, state)
			if err != nil {
				return nil, NewRouterExecutionError(id, step, err)
			}
			for _, t := range targets {
				add(t)
			}
			continue
		}
		// No edges, no router, not terminal: compile rejected this, so it is
		// unreachable here. Treat as branch end defensively.
	}
	return next, nil
}

func (r *Runner[T, U]) routeGuarded(ctx context.Context, router Router[T], state T) (id NodeID, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = NewNodePanicError("", 0, rec, debug.Stack())
		}
	}()
	return router(ctx, state)
}

func (r *Runner[T, U]) fanGuarded(ctx context.Context, fan FanRouter[T], state T) (ids []NodeID, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = NewNodePanicError("", 0, rec, debug.Stack())
		}
	}()
	return fan(ctx, state)
}

// snapshot returns a copy of state for a node to read. Precedence: explicit
// WithStateCloner, then a detected Clone() T method, then pass-through.
func (r *Runner[T, U]) snapshot(state T) T {
	if r.cloner != nil {
		return r.cloner(state)
	}
	if c, ok := any(state).(interface{ Clone() T }); ok {
		return c.Clone()
	}
	return state
}

// Stream runs the same loop in a goroutine, emitting a state snapshot on the
// state channel after every super-step (independent of checkpointInterval). Both
// channels are closed on completion or error. The caller must drain the channels
// or cancel the context to avoid a goroutine leak.
func (r *Runner[T, U]) Stream(ctx context.Context, initial T) (<-chan T, <-chan error) {
	states := make(chan T)
	errs := make(chan error, 1)

	go func() {
		defer close(states)
		defer close(errs)
		defer func() {
			if rec := recover(); rec != nil {
				errs <- NewNodePanicError("", 0, rec, debug.Stack())
			}
		}()

		threadID := newThreadID()
		state := initial
		frontier := []NodeID{r.entry}
		step := 0

		for len(frontier) > 0 {
			step++
			if step > r.recursionLimit {
				errs <- NewRecursionLimitError(r.recursionLimit, step, frontier)
				return
			}

			newState, lastID, err := r.runSuperStep(ctx, state, frontier, step)
			if err != nil {
				errs <- err
				return
			}
			state = newState

			if r.checkpointer != nil && step%r.checkpointInterval == 0 {
				if err := r.checkpointer.Save(ctx, Checkpoint[T]{
					ThreadID: threadID, NodeID: lastID, Step: step, State: state,
				}); err != nil {
					errs <- err
					return
				}
			}

			select {
			case states <- state:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}

			next, err := r.nextFrontier(ctx, state, frontier, step)
			if err != nil {
				errs <- err
				return
			}
			frontier = next
		}
	}()

	return states, errs
}

// Resume loads the latest checkpoint for threadID and continues the run from
// there, reusing the same thread ID for further checkpoints.
func (r *Runner[T, U]) Resume(ctx context.Context, threadID string) (T, error) {
	var zero T
	if r.checkpointer == nil {
		return zero, NewCompileError("Resume requires a checkpointer")
	}
	cp, err := r.checkpointer.LoadLatest(ctx, threadID)
	if err != nil {
		return zero, err
	}
	// The checkpoint captures state after node cp.NodeID's super-step. Continue
	// from the frontier that node leads to.
	frontier, err := r.nextFrontier(ctx, cp.State, []NodeID{cp.NodeID}, cp.Step)
	if err != nil {
		return zero, err
	}
	if len(frontier) == 0 {
		return cp.State, nil
	}
	return r.runLoop(ctx, threadID, cp.State, frontier, cp.Step)
}

func newThreadID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
