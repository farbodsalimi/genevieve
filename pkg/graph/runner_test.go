package graph

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- Fan-in determinism ----

func TestFanInDeterminism(t *testing.T) {
	// Two parallel nodes produce distinct entries; sorted reduce must yield the
	// same merged state every run.
	build := func() (*Runner[testState, testUpdate], error) {
		return NewBuilder(appendReducer()).
			AddNode("root", appendNode("root")).
			AddNode("x", appendNode("x")).
			AddNode("y", appendNode("y")).
			AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) {
				return []NodeID{"x", "y"}, nil
			}).
			SetEntryPoint("root").
			SetTerminal("x", "y").
			Compile()
	}
	runner, err := build()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := []string{"root", "x", "y"}
	for i := range 100 {
		final, err := runner.Run(context.Background(), testState{})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if strings.Join(final.Log, ",") != strings.Join(want, ",") {
			t.Fatalf("run %d: got %v want %v", i, final.Log, want)
		}
	}
}

// ---- Panic containment ----

func TestPanic_NodeYieldsNodePanicError(t *testing.T) {
	peerCancelled := make(chan struct{})
	runner, err := NewBuilder(appendReducer()).
		AddNode("root", appendNode("root")).
		AddNode("boom", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			panic("kaboom")
		})).
		AddNode("peer", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			<-ctx.Done()
			close(peerCancelled)
			return testUpdate{}, ctx.Err()
		})).
		AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) {
			return []NodeID{"boom", "peer"}, nil
		}).
		SetEntryPoint("root").
		SetTerminal("boom", "peer").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected NodePanicError, got %v", err)
	}
	if pe.NodeID != "boom" {
		t.Fatalf("expected node 'boom', got %q", pe.NodeID)
	}
	if pe.Step != 2 {
		t.Fatalf("expected step 2, got %d", pe.Step)
	}
	<-peerCancelled // peer was cancelled just like a returned error
}

func TestPanic_StackNamesOffendingNode(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("boom", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			panic("kaboom")
		})).
		SetEntryPoint("boom").
		SetTerminal("boom").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected NodePanicError, got %v", err)
	}
	// The stack must include the closure frame, captured before unwind.
	if !bytes.Contains(pe.Stack, []byte("graph.TestPanic_StackNamesOffendingNode")) {
		t.Fatalf("stack does not name the panicking frame:\n%s", pe.Stack)
	}
}

func TestPanic_CloneContained(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile(WithStateCloner(func(s testState) testState {
			panic("clone boom")
		}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected NodePanicError, got %v", err)
	}
}

func TestPanic_ReducerContained(t *testing.T) {
	runner, err := NewBuilder[testState, testUpdate](
		ReducerFunc[testState, testUpdate](func(s testState, u testUpdate) (testState, error) {
			panic("reduce boom")
		})).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected NodePanicError from reducer, got %v", err)
	}
}

func TestPanic_RouterContained(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddConditionalEdge("a", func(ctx context.Context, s testState) (NodeID, error) {
			panic("router boom")
		}).
		SetEntryPoint("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	if !errors.As(err, &pe) {
		t.Fatalf("expected NodePanicError from router, got %v", err)
	}
}

func TestPanic_DistinguishableFromExecutionError(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("boom", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			panic("kaboom")
		})).
		SetEntryPoint("boom").
		SetTerminal("boom").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	var pe *NodePanicError
	var ne *NodeExecutionError
	if !errors.As(err, &pe) {
		t.Fatal("expected NodePanicError")
	}
	if errors.As(err, &ne) {
		t.Fatal("panic must not also satisfy NodeExecutionError")
	}
}

// ---- State cloning ----

// cloneCountState counts how many times Clone is invoked.
type cloneCountState struct {
	counter *int32
	log     []string
}

func (s cloneCountState) Clone() cloneCountState {
	atomic.AddInt32(s.counter, 1)
	log := make([]string, len(s.log))
	copy(log, s.log)
	return cloneCountState{counter: s.counter, log: log}
}

func TestClone_DetectedAndInvokedPerDispatch(t *testing.T) {
	var count int32
	reducer := ReducerFunc[cloneCountState, string](func(s cloneCountState, u string) (cloneCountState, error) {
		return cloneCountState{counter: s.counter, log: append(slicesClone(s.log), u)}, nil
	})
	runner, err := NewBuilder[cloneCountState, string](reducer).
		AddNode("a", NodeFunc[cloneCountState, string](func(ctx context.Context, s cloneCountState) (string, error) {
			return "a", nil
		})).
		AddNode("b", NodeFunc[cloneCountState, string](func(ctx context.Context, s cloneCountState) (string, error) {
			return "b", nil
		})).
		AddEdge("a", "b").
		SetEntryPoint("a").
		SetTerminal("b").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := runner.Run(context.Background(), cloneCountState{counter: &count}); err != nil {
		t.Fatalf("run: %v", err)
	}
	// One dispatch per node = 2 clones.
	if count != 2 {
		t.Fatalf("expected 2 clones, got %d", count)
	}
}

func TestClone_StateClonerTakesPrecedence(t *testing.T) {
	var declaredCalls, hookCalls int32
	// State declares Clone (increments declaredCalls via counter), but the hook
	// must win.
	var count int32
	reducer := ReducerFunc[cloneCountState, string](func(s cloneCountState, u string) (cloneCountState, error) {
		return s, nil
	})
	runner, err := NewBuilder[cloneCountState, string](reducer).
		AddNode("a", NodeFunc[cloneCountState, string](func(ctx context.Context, s cloneCountState) (string, error) {
			return "a", nil
		})).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile(WithStateCloner(func(s cloneCountState) cloneCountState {
			atomic.AddInt32(&hookCalls, 1)
			return s
		}))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_ = declaredCalls
	if _, err := runner.Run(context.Background(), cloneCountState{counter: &count}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("expected hook called once, got %d", hookCalls)
	}
	if count != 0 {
		t.Fatalf("declared Clone should not run when hook is set, got %d", count)
	}
}

func slicesClone[E any](s []E) []E {
	out := make([]E, len(s))
	copy(out, s)
	return out
}

// ---- The guard test: shared-slice race ----

// raceState holds a shared slice by header. Without cloning, parallel appends
// race on the backing array.
type raceState struct {
	shared []int
}

func (s raceState) clone() raceState {
	out := make([]int, len(s.shared))
	copy(out, s.shared)
	return raceState{shared: out}
}

func runSharedSlice(t *testing.T, withCloner bool) {
	t.Helper()
	const fanWidth = 8
	// Node writes into its snapshot's slice at a fixed index. Without a cloner
	// every node shares the same backing array -> race under -race.
	reducer := ReducerFunc[raceState, int](func(s raceState, u int) (raceState, error) {
		return s, nil
	})
	b := NewBuilder[raceState, int](reducer).AddNode("root", NodeFunc[raceState, int](
		func(ctx context.Context, s raceState) (int, error) { return 0, nil }))
	var targets []NodeID
	for i := range fanWidth {
		idx := i
		id := NodeID(string(rune('a' + i)))
		targets = append(targets, id)
		b.AddNode(id, NodeFunc[raceState, int](func(ctx context.Context, s raceState) (int, error) {
			s.shared[idx] = idx // write into the (possibly shared) backing array
			return idx, nil
		}))
	}
	b.AddFanEdge("root", func(ctx context.Context, s raceState) ([]NodeID, error) { return targets, nil }).
		SetEntryPoint("root").
		SetTerminal(targets...)

	var opts []Option
	if withCloner {
		opts = append(opts, WithStateCloner(func(s raceState) raceState { return s.clone() }))
	}
	runner, err := b.Compile(opts...)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	init := raceState{shared: make([]int, fanWidth)}
	if _, err := runner.Run(context.Background(), init); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestSharedSliceRace exercises the cloner. Under -race the WithCloner case must
// pass. The no-cloner case is intentionally omitted from the default run because
// it is *designed* to trip the detector; it is documented in the plan's
// verification steps rather than failing CI here.
func TestSharedSliceRace(t *testing.T) {
	runSharedSlice(t, true)
}

// ---- Stream ----

func TestStream_EmitsPerSuperStepInOrder(t *testing.T) {
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
	states, errs := runner.Stream(context.Background(), testState{})
	var snapshots [][]string
	for s := range states {
		snapshots = append(snapshots, append([]string(nil), s.Log...))
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(snapshots) != 3 {
		t.Fatalf("expected 3 snapshots, got %d: %v", len(snapshots), snapshots)
	}
	if snapshots[0][len(snapshots[0])-1] != "a" ||
		snapshots[2][len(snapshots[2])-1] != "c" {
		t.Fatalf("snapshots out of order: %v", snapshots)
	}
}

func TestStream_ChannelsCloseOnError(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("boom", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			return testUpdate{}, errors.New("boom")
		})).
		SetEntryPoint("boom").
		SetTerminal("boom").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	states, errs := runner.Stream(context.Background(), testState{})
	for range states {
	}
	if err := <-errs; err == nil {
		t.Fatal("expected error from stream")
	}
	// Both channels must be closed.
	if _, ok := <-states; ok {
		t.Fatal("states channel not closed")
	}
	if _, ok := <-errs; ok {
		t.Fatal("errs channel not closed")
	}
}

func TestStream_ContextCancelTerminatesProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner, err := NewBuilder(appendReducer()).
		AddNode("loop", appendNode("loop")).
		AddConditionalEdge("loop", func(ctx context.Context, s testState) (NodeID, error) {
			return "loop", nil
		}).
		SetEntryPoint("loop").
		Compile(WithRecursionLimit(1000000))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	states, errs := runner.Stream(ctx, testState{})
	// Read one snapshot, then cancel.
	<-states
	cancel()
	// Drain; producer must terminate.
	done := make(chan struct{})
	go func() {
		for range states {
		}
		<-errs
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producer goroutine did not terminate after cancel")
	}
}

func TestStream_PanicClosesChannels(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("boom", NodeFunc[testState, testUpdate](func(ctx context.Context, s testState) (testUpdate, error) {
			panic("kaboom")
		})).
		SetEntryPoint("boom").
		SetTerminal("boom").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	states, errs := runner.Stream(context.Background(), testState{})
	for range states {
	}
	if err := <-errs; err == nil {
		t.Fatal("expected panic error from stream")
	}
	if _, ok := <-states; ok {
		t.Fatal("states channel not closed after panic")
	}
}
