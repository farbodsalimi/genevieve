package graph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// ---- shared test state & helpers ----

// testState accumulates a log of node visits plus a counter used by loop tests.
type testState struct {
	Log   []string
	Count int
}

// Clone deep-copies the slice so parallel nodes never share the backing array.
func (s testState) Clone() testState {
	log := make([]string, len(s.Log))
	copy(log, s.Log)
	return testState{Log: log, Count: s.Count}
}

// testUpdate is a partial update: an entry to append and/or a counter delta.
type testUpdate struct {
	Entry string
	Delta int
}

func appendReducer() Reducer[testState, testUpdate] {
	return ReducerFunc[testState, testUpdate](func(s testState, u testUpdate) (testState, error) {
		log := make([]string, len(s.Log), len(s.Log)+1)
		copy(log, s.Log)
		if u.Entry != "" {
			log = append(log, u.Entry)
		}
		return testState{Log: log, Count: s.Count + u.Delta}, nil
	})
}

// appendNode returns a node that records its own name.
func appendNode(name string) Node[testState, testUpdate] {
	return NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
		return testUpdate{Entry: name, Delta: 1}, nil
	})
}

// ---- Compile validation ----

func TestCompile_MissingEntryPoint(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetTerminal("a")
	_, err := b.Compile()
	var target *NoEntryPointError
	if !errors.As(err, &target) {
		t.Fatalf("expected NoEntryPointError, got %v", err)
	}
}

func TestCompile_EntryPointUnregistered(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetTerminal("a").
		SetEntryPoint("missing")
	_, err := b.Compile()
	var target *NodeNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected NodeNotFoundError, got %v", err)
	}
}

func TestCompile_DuplicateNode(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a")
	_, err := b.Compile()
	var target *DuplicateNodeError
	if !errors.As(err, &target) {
		t.Fatalf("expected DuplicateNodeError, got %v", err)
	}
}

func TestCompile_DanglingEdge(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddEdge("a", "ghost").
		SetEntryPoint("a")
	_, err := b.Compile()
	var target *DanglingEdgeError
	if !errors.As(err, &target) {
		t.Fatalf("expected DanglingEdgeError, got %v", err)
	}
}

func TestCompile_EmptyNodeIDRejected(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("", appendNode("x")).
		SetEntryPoint("a")
	_, err := b.Compile()
	if err == nil {
		t.Fatal("expected error for empty node ID")
	}
}

func TestCompile_UnreachableNode(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		SetTerminal("a", "b").
		SetEntryPoint("a")
	_, err := b.Compile()
	var target *UnreachableNodeError
	if !errors.As(err, &target) {
		t.Fatalf("expected UnreachableNodeError, got %v", err)
	}
}

func TestCompile_DeadEndNode(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a")
	_, err := b.Compile()
	var target *DeadEndError
	if !errors.As(err, &target) {
		t.Fatalf("expected DeadEndError, got %v", err)
	}
}

func TestCompile_TerminalNotDeadEnd(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a")
	if _, err := b.Compile(); err != nil {
		t.Fatalf("terminal node should not be a dead end: %v", err)
	}
}

func TestCompile_TerminalUnregistered(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddEdge("a", "a").
		SetEntryPoint("a").
		SetTerminal("ghost")
	_, err := b.Compile()
	var target *NodeNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected NodeNotFoundError, got %v", err)
	}
}

func TestCompile_EdgeRouterConflict(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddEdge("a", "b").
		AddConditionalEdge("a", func(ctx context.Context, s testState) (NodeID, error) { return "b", nil }).
		SetEntryPoint("a").
		SetTerminal("b")
	_, err := b.Compile()
	var target *EdgeRouterConflictError
	if !errors.As(err, &target) {
		t.Fatalf("expected EdgeRouterConflictError, got %v", err)
	}
}

func TestCompile_MissingReducer(t *testing.T) {
	b := NewBuilder[testState, testUpdate](nil).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a")
	_, err := b.Compile()
	var target *NoReducerError
	if !errors.As(err, &target) {
		t.Fatalf("expected NoReducerError, got %v", err)
	}
}

func TestCompile_RunnerUnaffectedByLaterMutation(t *testing.T) {
	b := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a")
	runner, err := b.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Mutate the builder after compile.
	b.AddNode("b", appendNode("b"))
	b.AddEdge("a", "b")

	if len(runner.nodes) != 1 {
		t.Fatalf("runner topology changed after builder mutation: %d nodes", len(runner.nodes))
	}
}

// ---- Run behavior ----

func TestRun_SequentialOrder(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddNode("c", appendNode("c")).
		AddEdge("a", "b").
		AddEdge("b", "c").
		SetEntryPoint("a").
		SetTerminal("c").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !slices.Equal(final.Log, want) {
		t.Fatalf("got %v want %v", final.Log, want)
	}
}

func TestRun_ParallelFanOutConcurrency(t *testing.T) {
	var concurrent, maxConcurrent int32
	var mu sync.Mutex
	start := make(chan struct{})

	slowNode := func(name string) Node[testState, testUpdate] {
		return NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			cur := atomic.AddInt32(&concurrent, 1)
			mu.Lock()
			if cur > maxConcurrent {
				maxConcurrent = cur
			}
			mu.Unlock()
			<-start // block until both are in-flight
			atomic.AddInt32(&concurrent, -1)
			return testUpdate{Entry: name}, nil
		})
	}

	runner, err := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root")).
		AddNode("x", slowNode("x")).
		AddNode("y", slowNode("y")).
		AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) {
			return []NodeID{"x", "y"}, nil
		}).
		SetEntryPoint("root").
		SetTerminal("x", "y").
		Compile(WithMaxParallel(2))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	go func() {
		// Wait until both fan-out nodes are concurrent, then release.
		for atomic.LoadInt32(&concurrent) < 2 {
		}
		close(start)
	}()

	if _, err := runner.Run(context.Background(), testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if maxConcurrent < 2 {
		t.Fatalf("expected 2 concurrent nodes, saw max %d", maxConcurrent)
	}
}

func TestRun_ConditionalBranch(t *testing.T) {
	build := func(goLeft bool) (*Runner[testState, testUpdate], error) {
		return NewBuilder(appendReducer()).
			AddNode("a", appendNode("a")).
			AddNode("left", appendNode("left")).
			AddNode("right", appendNode("right")).
			AddConditionalEdge("a", func(ctx context.Context, s testState) (NodeID, error) {
				if goLeft {
					return "left", nil
				}
				return "right", nil
			}).
			SetEntryPoint("a").
			SetTerminal("left", "right").
			Compile()
	}

	for _, goLeft := range []bool{true, false} {
		runner, err := build(goLeft)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		final, err := runner.Run(context.Background(), testState{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		want := "right"
		if goLeft {
			want = "left"
		}
		if !slices.Contains(final.Log, want) {
			t.Fatalf("goLeft=%v: expected %q in log %v", goLeft, want, final.Log)
		}
	}
}

func TestRun_RouterZeroHaltsBranch(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddConditionalEdge("a", func(ctx context.Context, s testState) (NodeID, error) {
			return "", nil // halt
		}).
		SetEntryPoint("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(final.Log, []string{"a"}) {
		t.Fatalf("got %v", final.Log)
	}
}

func TestRun_FanRouterNilAndEmptyHalt(t *testing.T) {
	for _, ret := range [][]NodeID{nil, {}} {
		runner, err := NewBuilder(appendReducer()).
			AddNode("a", appendNode("a")).
			AddFanEdge("a", func(ctx context.Context, s testState) ([]NodeID, error) {
				return ret, nil
			}).
			SetEntryPoint("a").
			Compile()
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if _, err := runner.Run(context.Background(), testState{}); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
}

func TestRun_TerminalViaStaticEdge(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddEdge("a", "b").
		SetEntryPoint("a").
		SetTerminal("b").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(final.Log, []string{"a", "b"}) {
		t.Fatalf("got %v", final.Log)
	}
}

func TestRun_MultiTargetBranch(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root")).
		AddNode("x", appendNode("x")).
		AddNode("y", appendNode("y")).
		AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) {
			return []NodeID{"x", "y"}, nil
		}).
		SetEntryPoint("root").
		SetTerminal("x", "y").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Deterministic sort by node ID: root, then x, y.
	want := []string{"root", "x", "y"}
	if !slices.Equal(final.Log, want) {
		t.Fatalf("got %v want %v", final.Log, want)
	}
}

func TestRun_LoopWithTermination(t *testing.T) {
	// A node cycles back to itself until Count reaches 3.
	runner, err := NewBuilder(appendReducer()).
		AddNode("loop", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			return testUpdate{Entry: "loop", Delta: 1}, nil
		})).
		AddNode("done", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			return testUpdate{Entry: "done"}, nil
		})).
		AddConditionalEdge("loop", func(ctx context.Context, s testState) (NodeID, error) {
			if s.Count >= 3 {
				return "done", nil
			}
			return "loop", nil
		}).
		SetEntryPoint("loop").
		SetTerminal("done").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final.Count != 3 {
		t.Fatalf("expected count 3, got %d", final.Count)
	}
}

func TestRun_LoopWithoutTermination(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("loop", appendNode("loop")).
		AddConditionalEdge("loop", func(ctx context.Context, s testState) (NodeID, error) {
			return "loop", nil // never terminates
		}).
		SetEntryPoint("loop").
		Compile(WithRecursionLimit(5))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var target *RecursionLimitError
	if !errors.As(err, &target) {
		t.Fatalf("expected RecursionLimitError, got %v", err)
	}
	if target.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", target.Limit)
	}
	if !slices.Equal(target.Frontier, []NodeID{"loop"}) {
		t.Fatalf("expected frontier [loop], got %v", target.Frontier)
	}
}

func TestRun_FailFast(t *testing.T) {
	boom := errors.New("boom")
	peerStarted := make(chan struct{})
	peerCancelled := make(chan struct{})

	runner, err := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root")).
		AddNode("bad", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			return testUpdate{}, boom
		})).
		AddNode("peer", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			close(peerStarted)
			<-ctx.Done() // should be cancelled by fail-fast
			close(peerCancelled)
			return testUpdate{}, ctx.Err()
		})).
		AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) {
			return []NodeID{"bad", "peer"}, nil
		}).
		SetEntryPoint("root").
		SetTerminal("bad", "peer").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var target *NodeExecutionError
	if !errors.As(err, &target) {
		t.Fatalf("expected NodeExecutionError, got %v", err)
	}
	if target.NodeID != "bad" {
		t.Fatalf("expected node 'bad', got %q", target.NodeID)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	<-peerStarted
	<-peerCancelled // confirms the peer was cancelled
}

func TestRun_NodeErrorHandlerContinuesToFanIn(t *testing.T) {
	boom := errors.New("boom")
	peerFinished := make(chan struct{})

	runner, err := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root")).
		AddNode("bad", NodeFunc[testState, testUpdate](func(context.Context, testState) (testUpdate, error) {
			return testUpdate{}, boom
		})).
		AddNode("peer", NodeFunc[testState, testUpdate](func(context.Context, testState) (testUpdate, error) {
			close(peerFinished)
			return testUpdate{Entry: "peer", Delta: 1}, nil
		})).
		AddNode("synthesize", NodeFunc[testState, testUpdate](func(_ context.Context, s testState) (testUpdate, error) {
			return testUpdate{Entry: fmt.Sprintf("synthesized-%d", len(s.Log))}, nil
		})).
		AddFanEdge("root", func(context.Context, testState) ([]NodeID, error) {
			return []NodeID{"bad", "peer"}, nil
		}).
		AddEdge("bad", "synthesize").
		AddEdge("peer", "synthesize").
		SetEntryPoint("root").
		SetTerminal("synthesize").
		Compile(WithNodeErrorHandler(func(id NodeID, err error) (testUpdate, error) {
			var nodeErr *NodeExecutionError
			if id != "bad" || !errors.As(err, &nodeErr) || !errors.Is(err, boom) {
				t.Fatalf("unexpected handled error for %q: %v", id, err)
			}
			return testUpdate{Entry: "bad failed"}, nil
		}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	<-peerFinished
	if want := []string{"root", "bad failed", "peer", "synthesized-3"}; !slices.Equal(final.Log, want) {
		t.Fatalf("log = %v, want %v", final.Log, want)
	}
}

func TestRun_NodeErrorHandlerErrorStopsRun(t *testing.T) {
	nodeBoom := errors.New("node boom")
	handlerBoom := errors.New("handler boom")
	runner, err := NewBuilder(appendReducer()).
		AddNode("bad", NodeFunc[testState, testUpdate](func(context.Context, testState) (testUpdate, error) {
			return testUpdate{}, nodeBoom
		})).
		SetEntryPoint("bad").
		SetTerminal("bad").
		Compile(WithNodeErrorHandler(func(NodeID, error) (testUpdate, error) {
			return testUpdate{}, handlerBoom
		}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(context.Background(), testState{})
	var target *NodeErrorHandlerError
	if !errors.As(err, &target) {
		t.Fatalf("expected NodeErrorHandlerError, got %v", err)
	}
	if !errors.Is(err, handlerBoom) || !errors.Is(target.NodeErr, nodeBoom) {
		t.Fatalf("handler or node error was not preserved: %v", err)
	}
}

func TestRun_ReducerError(t *testing.T) {
	boom := errors.New("reduce boom")
	badReducer := ReducerFunc[testState, testUpdate](func(s testState, u testUpdate) (testState, error) {
		return s, boom
	})
	runner, err := NewBuilder[testState, testUpdate](badReducer).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var target *ReducerError
	if !errors.As(err, &target) {
		t.Fatalf("expected ReducerError, got %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom")
	}
}

func TestRun_RouterError(t *testing.T) {
	boom := errors.New("router boom")
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddConditionalEdge("a", func(ctx context.Context, s testState) (NodeID, error) {
			return "", boom
		}).
		SetEntryPoint("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var target *RouterExecutionError
	if !errors.As(err, &target) {
		t.Fatalf("expected RouterExecutionError, got %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom")
	}
}

func TestRun_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", NodeFunc[testState, testUpdate](func(c context.Context, s testState) (testUpdate, error) {
			cancel()
			<-c.Done()
			return testUpdate{}, c.Err()
		})).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(ctx, testState{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRun_MaxParallelRespected(t *testing.T) {
	var concurrent, maxConcurrent int32
	const limit = 2
	const fanWidth = 6

	b := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root"))
	var targets []NodeID
	for i := range fanWidth {
		id := NodeID(fmt.Sprintf("n%d", i))
		targets = append(targets, id)
		b.AddNode(id, NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			cur := atomic.AddInt32(&concurrent, 1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
					break
				}
			}
			// brief spin to overlap
			for range 100000 {
			}
			atomic.AddInt32(&concurrent, -1)
			return testUpdate{}, nil
		}))
	}
	b.AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) { return targets, nil }).
		SetEntryPoint("root").
		SetTerminal(targets...)

	runner, err := b.Compile(WithMaxParallel(limit))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := runner.Run(context.Background(), testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if maxConcurrent > limit {
		t.Fatalf("max concurrency %d exceeded limit %d", maxConcurrent, limit)
	}
}

func TestRun_IsAcyclic(t *testing.T) {
	acyclic, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddEdge("a", "b").
		SetEntryPoint("a").
		SetTerminal("b").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !acyclic.IsAcyclic() {
		t.Fatal("expected acyclic")
	}

	cyclic, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddEdge("a", "b").
		AddEdge("b", "a").
		SetEntryPoint("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cyclic.IsAcyclic() {
		t.Fatal("expected cyclic")
	}
}
