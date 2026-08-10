package graph

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMapNode_FoldsPayloads(t *testing.T) {
	// payloads = [1,2,3,4]; inner doubles each; combine sums -> 20.
	node := MapNode[testState, testUpdate, int](
		func(s testState) []int { return []int{1, 2, 3, 4} },
		func(ctx context.Context, s testState, p int) (testUpdate, error) {
			return testUpdate{Delta: p * 2}, nil
		},
		func(us []testUpdate) (testUpdate, error) {
			total := 0
			for _, u := range us {
				total += u.Delta
			}
			return testUpdate{Delta: total}, nil
		},
	)
	runner, err := NewBuilder(appendReducer()).
		AddNode("map", node).
		SetEntryPoint("map").
		SetTerminal("map").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final.Count != 20 {
		t.Fatalf("expected 20, got %d", final.Count)
	}
}

func TestMapNode_EmptyPayloads(t *testing.T) {
	node := MapNode[testState, testUpdate, int](
		func(s testState) []int { return nil },
		func(ctx context.Context, s testState, p int) (testUpdate, error) {
			return testUpdate{Delta: p}, nil
		},
		func(us []testUpdate) (testUpdate, error) { return testUpdate{Delta: len(us)}, nil },
	)
	runner, err := NewBuilder(appendReducer()).
		AddNode("map", node).SetEntryPoint("map").SetTerminal("map").Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	final, err := runner.Run(context.Background(), testState{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if final.Count != 0 {
		t.Fatalf("expected 0, got %d", final.Count)
	}
}

func TestMapNode_InnerError(t *testing.T) {
	boom := errors.New("inner boom")
	node := MapNode[testState, testUpdate, int](
		func(s testState) []int { return []int{1, 2, 3} },
		func(ctx context.Context, s testState, p int) (testUpdate, error) {
			if p == 2 {
				return testUpdate{}, boom
			}
			return testUpdate{Delta: p}, nil
		},
		func(us []testUpdate) (testUpdate, error) { return testUpdate{}, nil },
	)
	runner, err := NewBuilder(appendReducer()).
		AddNode("map", node).SetEntryPoint("map").SetTerminal("map").Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = runner.Run(context.Background(), testState{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected inner boom, got %v", err)
	}
}

// TestMapNode_NestedLimitNoDeadlock is the §1 regression: a frontier of
// MaxParallel MapNodes, each fanning out internally, must complete rather than
// hang. A MapNode's internal limit is independent of the parent runner's
// semaphore; if it drew from the same limit, each node would wait on a slot it
// is itself holding. The timeout turns a regression into a test failure instead
// of a wedged CI.
func TestMapNode_NestedLimitNoDeadlock(t *testing.T) {
	const maxParallel = 2

	makeMap := func() Node[testState, testUpdate] {
		return MapNode[testState, testUpdate, int](
			func(s testState) []int { return []int{1, 2, 3, 4} },
			func(ctx context.Context, s testState, p int) (testUpdate, error) {
				return testUpdate{Delta: p}, nil
			},
			func(us []testUpdate) (testUpdate, error) {
				total := 0
				for _, u := range us {
					total += u.Delta
				}
				return testUpdate{Delta: total}, nil
			},
			WithMapParallel(maxParallel), // same width as the parent limit
		)
	}

	// Frontier of maxParallel MapNodes.
	b := NewBuilder(appendReducer()).AddNode("root", appendNode("root"))
	var targets []NodeID
	for i := range maxParallel {
		id := NodeID(string(rune('a' + i)))
		targets = append(targets, id)
		b.AddNode(id, makeMap())
	}
	b.AddFanEdge("root", func(ctx context.Context, s testState) ([]NodeID, error) { return targets, nil }).
		SetEntryPoint("root").
		SetTerminal(targets...)

	runner, err := b.Compile(WithMaxParallel(maxParallel))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, testState{})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("nested MapNode fan-out deadlocked")
	}
}
