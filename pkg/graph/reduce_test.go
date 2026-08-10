package graph

import (
	"context"
	"slices"
	"testing"
)

// ---- shared fixtures ----

// docState is a small workflow state exercising every field shape the
// combinators cover: scalar, slice, counter, and flag.
type docState struct {
	Draft     string
	Critiques []string
	Tags      []string
	Revisions int
	Done      bool
}

// docUpdate is the partial update produced by a node.
type docUpdate struct {
	Draft    string
	Critique string
	Tags     []string
	Revised  bool
	Done     bool
}

func draftField() Field[docState, docUpdate] {
	return SetIf(
		func(s *docState) *string { return &s.Draft },
		func(u docUpdate) string { return u.Draft },
		NonZero[string],
	)
}

func critiqueField() Field[docState, docUpdate] {
	return AppendIf(
		func(s *docState) *[]string { return &s.Critiques },
		func(u docUpdate) string { return u.Critique },
		NonZero[string],
	)
}

func revisionsField() Field[docState, docUpdate] {
	return Add(
		func(s *docState) *int { return &s.Revisions },
		func(u docUpdate) int {
			if u.Revised {
				return 1
			}
			return 0
		},
	)
}

// ---- Set / SetIf ----

func TestSet_OverwritesIncludingZero(t *testing.T) {
	r := Merge(Set(
		func(s *docState) *string { return &s.Draft },
		func(u docUpdate) string { return u.Draft },
	))

	got, err := r.Reduce(docState{Draft: "old"}, docUpdate{Draft: ""})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Draft != "" {
		t.Errorf("Draft = %q, want %q (Set must not skip zero values)", got.Draft, "")
	}
}

func TestSetIf_SkipsZeroKeepsNonZero(t *testing.T) {
	r := Merge(draftField())

	tests := []struct {
		name   string
		state  docState
		update docUpdate
		want   string
	}{
		{"zero update leaves field", docState{Draft: "old"}, docUpdate{Draft: ""}, "old"},
		{"non-zero update overwrites", docState{Draft: "old"}, docUpdate{Draft: "new"}, "new"},
		{"writes into empty state", docState{}, docUpdate{Draft: "new"}, "new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Reduce(tt.state, tt.update)
			if err != nil {
				t.Fatalf("Reduce: %v", err)
			}
			if got.Draft != tt.want {
				t.Errorf("Draft = %q, want %q", got.Draft, tt.want)
			}
		})
	}
}

// ---- Append / AppendIf / Concat ----

func TestAppendIf_AppendsAndSkips(t *testing.T) {
	r := Merge(critiqueField())

	got, err := r.Reduce(docState{Critiques: []string{"a"}}, docUpdate{Critique: "b"})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(got.Critiques, want) {
		t.Errorf("Critiques = %v, want %v", got.Critiques, want)
	}

	got, err = r.Reduce(docState{Critiques: []string{"a"}}, docUpdate{Critique: ""})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if want := []string{"a"}; !slices.Equal(got.Critiques, want) {
		t.Errorf("Critiques = %v, want %v (empty value must not append)", got.Critiques, want)
	}
}

// TestAppend_DoesNotMutateInputSlice is the reason Append copies: the previous
// state is still readable by a concurrent node, so the reducer must not write
// through a shared backing array.
func TestAppend_DoesNotMutateInputSlice(t *testing.T) {
	r := Merge(Append(
		func(s *docState) *[]string { return &s.Critiques },
		func(u docUpdate) string { return u.Critique },
	))

	// Spare capacity is what makes an in-place append silently corrupt the
	// caller's slice; without it append would allocate and hide the bug.
	orig := make([]string, 1, 4)
	orig[0] = "a"
	before := docState{Critiques: orig}

	if _, err := r.Reduce(before, docUpdate{Critique: "b"}); err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if want := []string{"a"}; !slices.Equal(before.Critiques, want) {
		t.Errorf("input Critiques = %v, want %v (reducer mutated its input)", before.Critiques, want)
	}
	if n := len(orig); n != 1 {
		t.Errorf("len(orig) = %d, want 1", n)
	}
}

func TestConcat_AppendsBatchAndSkipsEmpty(t *testing.T) {
	r := Merge(Concat(
		func(s *docState) *[]string { return &s.Tags },
		func(u docUpdate) []string { return u.Tags },
	))

	got, err := r.Reduce(docState{Tags: []string{"x"}}, docUpdate{Tags: []string{"y", "z"}})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if want := []string{"x", "y", "z"}; !slices.Equal(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}

	got, err = r.Reduce(docState{Tags: []string{"x"}}, docUpdate{Tags: nil})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if want := []string{"x"}; !slices.Equal(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
}

// ---- Add / Or ----

func TestAdd_AccumulatesCounter(t *testing.T) {
	r := Merge(revisionsField())

	got, err := r.Reduce(docState{Revisions: 2}, docUpdate{Revised: true})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Revisions != 3 {
		t.Errorf("Revisions = %d, want 3", got.Revisions)
	}

	got, err = r.Reduce(docState{Revisions: 2}, docUpdate{Revised: false})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Revisions != 2 {
		t.Errorf("Revisions = %d, want 2", got.Revisions)
	}
}

func TestOr_LatchesTrue(t *testing.T) {
	r := Merge(Or(
		func(s *docState) *bool { return &s.Done },
		func(u docUpdate) bool { return u.Done },
	))

	got, err := r.Reduce(docState{Done: true}, docUpdate{Done: false})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if !got.Done {
		t.Error("Done = false, want true (Or must not clear a latched flag)")
	}

	got, err = r.Reduce(docState{Done: false}, docUpdate{Done: true})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if !got.Done {
		t.Error("Done = false, want true")
	}
}

// ---- Apply ----

func TestApply_RunsCustomMerge(t *testing.T) {
	r := Merge(Apply(func(s *docState, u docUpdate) {
		if u.Draft != "" {
			s.Draft = u.Draft
			s.Revisions = len(u.Draft)
		}
	}))

	got, err := r.Reduce(docState{}, docUpdate{Draft: "abcd"})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Draft != "abcd" || got.Revisions != 4 {
		t.Errorf("got %+v, want Draft=abcd Revisions=4", got)
	}
}

// ---- Merge composition ----

func TestMerge_AppliesFieldsInOrder(t *testing.T) {
	r := Merge(
		draftField(),
		critiqueField(),
		revisionsField(),
	)

	got, err := r.Reduce(
		docState{Draft: "v1", Critiques: []string{"first"}, Revisions: 1},
		docUpdate{Draft: "v2", Critique: "second", Revised: true},
	)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Draft != "v2" {
		t.Errorf("Draft = %q, want %q", got.Draft, "v2")
	}
	if want := []string{"first", "second"}; !slices.Equal(got.Critiques, want) {
		t.Errorf("Critiques = %v, want %v", got.Critiques, want)
	}
	if got.Revisions != 2 {
		t.Errorf("Revisions = %d, want 2", got.Revisions)
	}
}

func TestMerge_NoFieldsIsIdentity(t *testing.T) {
	r := Merge[docState, docUpdate]()
	in := docState{Draft: "v1", Revisions: 3}
	got, err := r.Reduce(in, docUpdate{Draft: "ignored"})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if got.Draft != "v1" || got.Revisions != 3 {
		t.Errorf("got %+v, want unchanged %+v", got, in)
	}
}

func TestMerge_DoesNotMutateInputState(t *testing.T) {
	r := Merge(draftField(), revisionsField())
	in := docState{Draft: "v1", Revisions: 1}

	if _, err := r.Reduce(in, docUpdate{Draft: "v2", Revised: true}); err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	if in.Draft != "v1" || in.Revisions != 1 {
		t.Errorf("input state = %+v, want {Draft:v1 Revisions:1}", in)
	}
}

// ---- integration with the runner ----

// TestMerge_DrivesGraphRun checks a Merge-built reducer against the real
// super-step loop, including a parallel fan-in where two nodes append in the
// same step.
func TestMerge_DrivesGraphRun(t *testing.T) {
	critic := func(text string) Node[docState, docUpdate] {
		return NodeFunc[docState, docUpdate](func(ctx context.Context, s docState) (docUpdate, error) {
			return docUpdate{Critique: text}, nil
		})
	}

	runner, err := NewBuilder(Merge(draftField(), critiqueField(), revisionsField())).
		AddNode("draft", NodeFunc[docState, docUpdate](
			func(ctx context.Context, s docState) (docUpdate, error) {
				return docUpdate{Draft: "body", Revised: true}, nil
			})).
		AddNode("a", critic("from-a")).
		AddNode("b", critic("from-b")).
		AddEdge("draft", "a").
		AddEdge("draft", "b").
		SetEntryPoint("draft").
		SetTerminal("a", "b").
		Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	final, err := runner.Run(context.Background(), docState{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if final.Draft != "body" {
		t.Errorf("Draft = %q, want %q", final.Draft, "body")
	}
	if final.Revisions != 1 {
		t.Errorf("Revisions = %d, want 1", final.Revisions)
	}
	// Reducers apply in node-ID order, so fan-in is deterministic.
	if want := []string{"from-a", "from-b"}; !slices.Equal(final.Critiques, want) {
		t.Errorf("Critiques = %v, want %v", final.Critiques, want)
	}
}
