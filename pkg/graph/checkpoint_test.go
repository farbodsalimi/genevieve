package graph

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func linearRunner(t *testing.T, cp Checkpointer[testState], opts ...Option) *Runner[testState, testUpdate] {
	t.Helper()
	allOpts := append([]Option{WithCheckpointer(cp)}, opts...)
	runner, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddNode("c", appendNode("c")).
		AddEdge("a", "b").
		AddEdge("b", "c").
		SetEntryPoint("a").
		SetTerminal("c").
		Compile(allOpts...)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return runner
}

func TestCheckpoint_SavesOnePerSuperStep(t *testing.T) {
	cp := NewMemoryCheckpointer[testState](0)
	runner := linearRunner(t, cp)
	if _, err := runner.RunWithThread(context.Background(), "t1", testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist, _ := cp.History(context.Background(), "t1")
	if len(hist) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(hist))
	}
	for i, c := range hist {
		if c.Step != i+1 {
			t.Fatalf("checkpoint %d has step %d", i, c.Step)
		}
	}
}

func TestCheckpoint_Interval(t *testing.T) {
	cp := NewMemoryCheckpointer[testState](0)
	// 9-step linear graph, interval 3 -> saves at 3, 6, 9.
	b := NewBuilder(appendReducer())
	var prev NodeID
	for i := range 9 {
		id := NodeID(string(rune('a' + i)))
		b.AddNode(id, appendNode(string(id)))
		if prev != "" {
			b.AddEdge(prev, id)
		}
		prev = id
	}
	b.SetEntryPoint("a").SetTerminal(prev)
	runner, err := b.Compile(WithCheckpointer[testState](cp), WithCheckpointInterval(3))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := runner.RunWithThread(context.Background(), "t1", testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist, _ := cp.History(context.Background(), "t1")
	var steps []int
	for _, c := range hist {
		steps = append(steps, c.Step)
	}
	if !slices.Equal(steps, []int{3, 6, 9}) {
		t.Fatalf("expected steps [3 6 9], got %v", steps)
	}
}

func TestCheckpoint_MaxHistoryEvictsOldest(t *testing.T) {
	cp := NewMemoryCheckpointer[testState](2)
	runner := linearRunner(t, cp)
	if _, err := runner.RunWithThread(context.Background(), "t1", testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist, _ := cp.History(context.Background(), "t1")
	if len(hist) != 2 {
		t.Fatalf("expected cap of 2, got %d", len(hist))
	}
	// Oldest (step 1) evicted; steps 2 and 3 remain.
	if hist[0].Step != 2 || hist[1].Step != 3 {
		t.Fatalf("expected steps [2 3], got [%d %d]", hist[0].Step, hist[1].Step)
	}
}

func TestCheckpoint_HistoryInStepOrder(t *testing.T) {
	cp := NewMemoryCheckpointer[testState](0)
	runner := linearRunner(t, cp)
	if _, err := runner.RunWithThread(context.Background(), "t1", testState{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	hist, _ := cp.History(context.Background(), "t1")
	for i := 1; i < len(hist); i++ {
		if hist[i].Step <= hist[i-1].Step {
			t.Fatalf("history not in step order: %v", hist)
		}
	}
}

func TestCheckpoint_LoadLatestNotFound(t *testing.T) {
	cp := NewMemoryCheckpointer[testState](0)
	_, err := cp.LoadLatest(context.Background(), "missing")
	var target *CheckpointNotFoundError
	if !errors.As(err, &target) {
		t.Fatalf("expected CheckpointNotFoundError, got %v", err)
	}
}

func TestResume_ContinuesFromCheckpoint(t *testing.T) {
	// Run a full linear graph, capturing the uninterrupted final state.
	full := linearRunner(t, NewMemoryCheckpointer[testState](0))
	wantFinal, err := full.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("full run: %v", err)
	}

	// Now run a graph that stops after node "a" via a checkpointer, then resume.
	cp := NewMemoryCheckpointer[testState](0)
	// Simulate an interrupted run: manually save a checkpoint after node "a".
	afterA, _ := appendReducer().Reduce(testState{}, testUpdate{Entry: "a", Delta: 1})
	if err := cp.Save(context.Background(), Checkpoint[testState]{
		ThreadID: "t1", NodeID: "a", Step: 1, State: afterA,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	runner := linearRunner(t, cp)
	resumed, err := runner.Resume(context.Background(), "t1")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !slices.Equal(resumed.Log, wantFinal.Log) {
		t.Fatalf("resume produced %v, want %v", resumed.Log, wantFinal.Log)
	}
}

func TestResume_NoCheckpointer(t *testing.T) {
	runner := linearRunner(t, NewMemoryCheckpointer[testState](0))
	// Build a fresh runner without a checkpointer.
	plain, err := NewBuilder(appendReducer()).
		AddNode("a", appendNode("a")).
		SetEntryPoint("a").
		SetTerminal("a").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_ = runner
	if _, err := plain.Resume(context.Background(), "t1"); err == nil {
		t.Fatal("expected error resuming without checkpointer")
	}
}
