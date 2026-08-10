package graph

import (
	"maps"
	"runtime"
	"slices"
)

// Option configures a Runner at compile time.
type Option func(*runnerConfig)

type runnerConfig struct {
	recursionLimit     int
	maxParallel        int
	checkpointInterval int
	cloner             any // func(T) T, type-asserted in Compile
	checkpointer       any // Checkpointer[T], type-asserted in Compile
}

// WithRecursionLimit caps the number of super-steps a run may take before
// returning RecursionLimitError. Default 25.
func WithRecursionLimit(n int) Option {
	return func(c *runnerConfig) { c.recursionLimit = n }
}

// WithMaxParallel bounds how many frontier nodes run concurrently within one
// super-step. Default runtime.GOMAXPROCS(0). This is a semaphore, not a worker
// pool; its real purpose is respecting provider rate limits, not CPU.
func WithMaxParallel(n int) Option {
	return func(c *runnerConfig) { c.maxParallel = n }
}

// WithCheckpointInterval saves state every n super-steps. Default 1.
func WithCheckpointInterval(n int) Option {
	return func(c *runnerConfig) { c.checkpointInterval = n }
}

// Compile validates the topology, then deep-copies it into an immutable Runner.
// Later mutation of the Builder cannot affect an already-compiled Runner.
func (b *Builder[T, U]) Compile(opts ...Option) (*Runner[T, U], error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.errs) > 0 {
		return nil, b.errs[0]
	}

	if b.reducer == nil {
		return nil, NewNoReducerError()
	}
	if b.entry == "" {
		return nil, NewNoEntryPointError()
	}
	if _, ok := b.nodes[b.entry]; !ok {
		return nil, NewNodeNotFoundError(b.entry)
	}

	// Terminal IDs must name registered nodes.
	for id := range b.terminal {
		if _, ok := b.nodes[id]; !ok {
			return nil, NewNodeNotFoundError(id)
		}
	}

	// Every static edge target must be registered; a node may not have both
	// static edges and a router.
	for from, tos := range b.edges {
		if _, ok := b.nodes[from]; !ok {
			return nil, NewNodeNotFoundError(from)
		}
		for _, to := range tos {
			if _, ok := b.nodes[to]; !ok {
				return nil, NewDanglingEdgeError(from, to)
			}
		}
		if _, hasCond := b.conds[from]; hasCond {
			return nil, NewEdgeRouterConflictError(from)
		}
		if _, hasFan := b.fans[from]; hasFan {
			return nil, NewEdgeRouterConflictError(from)
		}
	}
	// A node cannot carry both router kinds.
	for from := range b.conds {
		if _, ok := b.nodes[from]; !ok {
			return nil, NewNodeNotFoundError(from)
		}
		if _, hasFan := b.fans[from]; hasFan {
			return nil, NewEdgeRouterConflictError(from)
		}
	}
	for from := range b.fans {
		if _, ok := b.nodes[from]; !ok {
			return nil, NewNodeNotFoundError(from)
		}
	}

	// Dead ends: each node needs an outgoing edge, a router, or terminal mark.
	for _, id := range b.order {
		if b.terminal[id] {
			continue
		}
		if len(b.edges[id]) > 0 {
			continue
		}
		if _, ok := b.conds[id]; ok {
			continue
		}
		if _, ok := b.fans[id]; ok {
			continue
		}
		return nil, NewDeadEndError(id)
	}

	// Reachability: walk static edges from the entry. A node carrying a router
	// is treated as reaching every node, since its targets are only known at
	// runtime.
	reachable := b.reachableSet()
	var unreachable []NodeID
	for _, id := range b.order {
		if !reachable[id] {
			unreachable = append(unreachable, id)
		}
	}
	if len(unreachable) > 0 {
		return nil, NewUnreachableNodeError(unreachable)
	}

	cfg := runnerConfig{
		recursionLimit:     25,
		maxParallel:        runtime.GOMAXPROCS(0),
		checkpointInterval: 1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.recursionLimit < 1 {
		cfg.recursionLimit = 25
	}
	if cfg.maxParallel < 1 {
		cfg.maxParallel = runtime.GOMAXPROCS(0)
	}
	if cfg.checkpointInterval < 1 {
		cfg.checkpointInterval = 1
	}

	r := &Runner[T, U]{
		nodes:              make(map[NodeID]Node[T, U], len(b.nodes)),
		edges:              make(map[NodeID][]NodeID, len(b.edges)),
		conds:              make(map[NodeID]Router[T], len(b.conds)),
		fans:               make(map[NodeID]FanRouter[T], len(b.fans)),
		terminal:           make(map[NodeID]bool, len(b.terminal)),
		entry:              b.entry,
		reducer:            b.reducer,
		recursionLimit:     cfg.recursionLimit,
		maxParallel:        cfg.maxParallel,
		checkpointInterval: cfg.checkpointInterval,
		acyclic:            b.isAcyclic(),
	}
	maps.Copy(r.nodes, b.nodes)
	for from, tos := range b.edges {
		cp := make([]NodeID, len(tos))
		copy(cp, tos)
		r.edges[from] = cp
	}
	maps.Copy(r.conds, b.conds)
	maps.Copy(r.fans, b.fans)
	for id := range b.terminal {
		r.terminal[id] = true
	}

	// Type-assert the generic-carrying options against this Runner's T.
	if cfg.cloner != nil {
		if fn, ok := cfg.cloner.(func(T) T); ok {
			r.cloner = fn
		} else {
			return nil, NewCompileError("WithStateCloner type does not match graph state type")
		}
	}
	if cfg.checkpointer != nil {
		if cp, ok := cfg.checkpointer.(Checkpointer[T]); ok {
			r.checkpointer = cp
		} else {
			return nil, NewCompileError("WithCheckpointer type does not match graph state type")
		}
	}
	return r, nil
}

// WithStateCloner supplies an explicit deep-copy hook, taking precedence over a
// detected Clone() method. Use for state types the caller cannot add a method to.
func WithStateCloner[T any](f func(T) T) Option {
	return func(c *runnerConfig) { c.cloner = f }
}

// WithCheckpointer attaches a Checkpointer, saving state every
// checkpointInterval super-steps.
func WithCheckpointer[T any](c Checkpointer[T]) Option {
	return func(cfg *runnerConfig) { cfg.checkpointer = c }
}

// reachableSet walks static edges from the entry. Any node with a router is
// assumed to reach every node, since targets are runtime-decided.
func (b *Builder[T, U]) reachableSet() map[NodeID]bool {
	reachable := make(map[NodeID]bool)
	// If any node has a router, all nodes are potentially reachable.
	hasAnyRouter := len(b.conds) > 0 || len(b.fans) > 0
	if hasAnyRouter {
		// A router's targets are unknown, so treat any node on a path that can
		// reach a router (or the router node itself) as able to reach anything.
		// Simplest sound approximation: if any router exists and is reachable,
		// mark everything reachable. Walk statically first; if a reachable node
		// carries a router, open the gates.
	}
	visited := make(map[NodeID]bool)
	var stack []NodeID
	stack = append(stack, b.entry)
	openGates := false
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[id] {
			continue
		}
		visited[id] = true
		reachable[id] = true
		if _, ok := b.conds[id]; ok {
			openGates = true
		}
		if _, ok := b.fans[id]; ok {
			openGates = true
		}
		for _, to := range b.edges[id] {
			if !visited[to] {
				stack = append(stack, to)
			}
		}
	}
	if openGates {
		for id := range b.nodes {
			reachable[id] = true
		}
	}
	return reachable
}

// isAcyclic runs Kahn's algorithm over the static edges only. Cycles are not an
// error; this is diagnostic metadata exposed as Runner.IsAcyclic.
func (b *Builder[T, U]) isAcyclic() bool {
	indeg := make(map[NodeID]int, len(b.nodes))
	for id := range b.nodes {
		indeg[id] = 0
	}
	for _, tos := range b.edges {
		for _, to := range tos {
			indeg[to]++
		}
	}
	var queue []NodeID
	for id := range b.nodes {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}
	slices.Sort(queue)
	removed := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		removed++
		for _, to := range b.edges[id] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	return removed == len(b.nodes)
}
