package graph

import (
	"context"
	"slices"
	"testing"
)

// TestResume_ParallelFrontierReplaysAllBranches is a regression test for a bug
// where resuming after a super-step whose frontier had more than one node lost
// every branch except the last-reduced one: the checkpoint persisted a single
// NodeID, and Resume rebuilt the next frontier from that node alone.
//
//	s --> a --> c(terminal)
//	s --> b --> d(terminal)
//
// Super-step 2 runs {a,b} in parallel. A resume from that checkpoint must replay
// both a->c and b->d; before the fix, a->c was dropped and "c" never ran.
func TestResume_ParallelFrontierReplaysAllBranches(t *testing.T) {
	build := func(cp Checkpointer[testState]) *Runner[testState, testUpdate] {
		r, err := NewBuilder(appendReducer()).
			AddNode("s", appendNode("s")).
			AddNode("a", appendNode("a")).
			AddNode("b", appendNode("b")).
			AddNode("c", appendNode("c")).
			AddNode("d", appendNode("d")).
			AddEdge("s", "a").
			AddEdge("s", "b").
			AddEdge("a", "c").
			AddEdge("b", "d").
			SetEntryPoint("s").
			SetTerminal("c", "d").
			Compile(WithCheckpointer(cp))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		return r
	}

	// Full uninterrupted run: the ground truth.
	wantFinal, err := build(NewMemoryCheckpointer[testState](0)).
		Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	wantSorted := slices.Clone(wantFinal.Log)
	slices.Sort(wantSorted)

	// Record every checkpoint, then isolate the one taken after super-step 2 (the
	// parallel {a,b} step) and resume from a checkpointer holding only that one.
	cpAll := NewMemoryCheckpointer[testState](0)
	if _, err := build(cpAll).RunWithThread(context.Background(), "t1", testState{}); err != nil {
		t.Fatalf("recording run: %v", err)
	}
	hist, _ := cpAll.History(context.Background(), "t1")
	var step2 Checkpoint[testState]
	for _, c := range hist {
		if c.Step == 2 {
			step2 = c
		}
	}
	if len(step2.Frontier) != 2 {
		t.Fatalf("step-2 checkpoint should carry a 2-node frontier, got %v", step2.Frontier)
	}

	cpResume := NewMemoryCheckpointer[testState](0)
	if err := cpResume.Save(context.Background(), step2); err != nil {
		t.Fatalf("save: %v", err)
	}
	resumed, err := build(cpResume).Resume(context.Background(), "t1")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	gotSorted := slices.Clone(resumed.Log)
	slices.Sort(gotSorted)
	if !slices.Equal(gotSorted, wantSorted) {
		t.Fatalf("resume lost nodes: got %v, want %v", gotSorted, wantSorted)
	}
}
