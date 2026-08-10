package chat

import (
	"testing"

	"github.com/farbodsalimi/genevieve/pkg/llm"
)

func TestReducer_AppendsCopyOnWrite(t *testing.T) {
	reducer := Reducer()
	base := State{Messages: []llm.Message{{Role: llm.RoleUser, Content: "one"}}}
	next, err := reducer.Reduce(
		base,
		Update{Message: llm.Message{Role: llm.RoleAssistant, Content: "two"}},
	)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if len(next.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(next.Messages))
	}
	// base must be untouched (copy-on-write).
	if len(base.Messages) != 1 {
		t.Fatalf("base mutated: %d messages", len(base.Messages))
	}
}

func TestClone_IndependentBackingArray(t *testing.T) {
	base := State{Messages: []llm.Message{{Role: llm.RoleUser, Content: "one"}}}
	clone := base.Clone()
	clone.Messages[0].Content = "mutated"
	if base.Messages[0].Content != "one" {
		t.Fatalf("clone shares backing array: base now %q", base.Messages[0].Content)
	}
}
