package graph

import (
	"context"
	"maps"
	"slices"
	"testing"
)

func TestTopology_ReportsStructure(t *testing.T) {
	runner, err := NewBuilder(appendReducer()).
		AddNode("intake", appendNode("intake")).
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddNode("join", appendNode("join")).
		AddNode("done", appendNode("done")).
		AddEdge("intake", "a").
		AddEdge("intake", "b").
		AddEdge("a", "join").
		AddEdge("b", "join").
		AddConditionalEdge("join", func(_ context.Context, s testState) (NodeID, error) {
			return "done", nil
		}).
		SetEntryPoint("intake").
		SetTerminal("done").
		Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	top := runner.Topology()

	if top.Entry != "intake" {
		t.Errorf("entry = %q, want intake", top.Entry)
	}
	if want := []NodeID{"a", "b", "done", "intake", "join"}; !slices.Equal(top.Nodes, want) {
		t.Errorf("nodes = %v, want %v", top.Nodes, want)
	}
	if want := []NodeID{"join"}; !slices.Equal(top.Routed, want) {
		t.Errorf("routed = %v, want %v", top.Routed, want)
	}
	if want := []NodeID{"done"}; !slices.Equal(top.Terminal, want) {
		t.Errorf("terminal = %v, want %v", top.Terminal, want)
	}
	wantEdges := map[NodeID][]NodeID{
		"intake": {"a", "b"},
		"a":      {"join"},
		"b":      {"join"},
	}
	if !maps.EqualFunc(top.Edges, wantEdges, slices.Equal) {
		t.Errorf("edges = %v, want %v", top.Edges, wantEdges)
	}
	// A routed node must not also appear in Edges.
	if _, ok := top.Edges["join"]; ok {
		t.Errorf("routed node join should have no static edges")
	}
}

func TestTopology_IsACopy(t *testing.T) {
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
	top := runner.Topology()
	top.Edges["a"][0] = "mutated"
	top.Nodes[0] = "mutated"
	// A fresh snapshot must be unaffected by mutating the previous one.
	again := runner.Topology()
	if again.Edges["a"][0] != "b" {
		t.Errorf("Topology leaked its edge backing array")
	}
	if !slices.Contains(again.Nodes, NodeID("a")) {
		t.Errorf("Topology leaked its nodes slice")
	}
}
