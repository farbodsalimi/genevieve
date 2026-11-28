package graph

import "slices"

// Topology is a static, domain-free snapshot of a compiled graph's structure,
// exported for tooling such as visualizers and dashboards. It describes what the
// compiler validated: the entry node, every registered node, the static edges,
// which nodes resolve their successors at runtime via a conditional or fan
// router, and which nodes are terminal.
//
// Router targets are deliberately absent: a Router/FanRouter is an opaque
// closure whose next nodes are only known at run time. A node listed in Routed
// has no entry in Edges; a tool should render its outgoing edges from observed
// execution instead.
type Topology struct {
	Entry    NodeID
	Nodes    []NodeID
	Edges    map[NodeID][]NodeID
	Routed   []NodeID
	Terminal []NodeID
}

// Topology returns a copy of the runner's structure. The result shares nothing
// with the runner, so callers may retain or mutate it freely. All slices are
// sorted by node ID for deterministic output.
func (r *Runner[T, U]) Topology() Topology {
	t := Topology{
		Entry:    r.entry,
		Nodes:    make([]NodeID, 0, len(r.nodes)),
		Edges:    make(map[NodeID][]NodeID, len(r.edges)),
		Routed:   make([]NodeID, 0, len(r.conds)+len(r.fans)),
		Terminal: make([]NodeID, 0, len(r.terminal)),
	}
	for id := range r.nodes {
		t.Nodes = append(t.Nodes, id)
	}
	for from, tos := range r.edges {
		cp := slices.Clone(tos)
		slices.Sort(cp)
		t.Edges[from] = cp
	}
	for id := range r.conds {
		t.Routed = append(t.Routed, id)
	}
	for id := range r.fans {
		t.Routed = append(t.Routed, id)
	}
	for id := range r.terminal {
		t.Terminal = append(t.Terminal, id)
	}
	slices.Sort(t.Nodes)
	slices.Sort(t.Routed)
	slices.Sort(t.Terminal)
	return t
}
