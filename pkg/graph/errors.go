package graph

import (
	"fmt"
	"strings"
)

// CompileError is a generic compilation failure. Specific constructors below
// wrap the common cases with structured fields.
type CompileError struct {
	Reason string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("graph compile: %s", e.Reason)
}

func NewCompileError(reason string) *CompileError {
	return &CompileError{Reason: reason}
}

// NodeNotFoundError is returned when an ID references a node that was never added.
type NodeNotFoundError struct {
	NodeID NodeID
}

func (e *NodeNotFoundError) Error() string {
	return fmt.Sprintf("graph compile: node %q not found", e.NodeID)
}

func NewNodeNotFoundError(id NodeID) *NodeNotFoundError {
	return &NodeNotFoundError{NodeID: id}
}

// DuplicateNodeError is returned when the same node ID is added twice.
type DuplicateNodeError struct {
	NodeID NodeID
}

func (e *DuplicateNodeError) Error() string {
	return fmt.Sprintf("graph compile: node %q already added", e.NodeID)
}

func NewDuplicateNodeError(id NodeID) *DuplicateNodeError {
	return &DuplicateNodeError{NodeID: id}
}

// DanglingEdgeError is returned when a static edge points at an unregistered node.
type DanglingEdgeError struct {
	From NodeID
	To   NodeID
}

func (e *DanglingEdgeError) Error() string {
	return fmt.Sprintf("graph compile: edge %q -> %q targets an unregistered node", e.From, e.To)
}

func NewDanglingEdgeError(from, to NodeID) *DanglingEdgeError {
	return &DanglingEdgeError{From: from, To: to}
}

// UnreachableNodeError names nodes that cannot be reached from the entry point.
type UnreachableNodeError struct {
	NodeIDs []NodeID
}

func (e *UnreachableNodeError) Error() string {
	ids := make([]string, len(e.NodeIDs))
	for i, id := range e.NodeIDs {
		ids[i] = string(id)
	}
	return fmt.Sprintf("graph compile: unreachable nodes: %s", strings.Join(ids, ", "))
}

func NewUnreachableNodeError(ids []NodeID) *UnreachableNodeError {
	return &UnreachableNodeError{NodeIDs: ids}
}

// DeadEndError is returned when a node has no outgoing edge, no router, and is
// not marked terminal.
type DeadEndError struct {
	NodeID NodeID
}

func (e *DeadEndError) Error() string {
	return fmt.Sprintf(
		"graph compile: node %q is a dead end (no edges, no router, not terminal)",
		e.NodeID,
	)
}

func NewDeadEndError(id NodeID) *DeadEndError {
	return &DeadEndError{NodeID: id}
}

// EdgeRouterConflictError is returned when a node has both static edges and a router.
type EdgeRouterConflictError struct {
	NodeID NodeID
}

func (e *EdgeRouterConflictError) Error() string {
	return fmt.Sprintf("graph compile: node %q has both static edges and a router", e.NodeID)
}

func NewEdgeRouterConflictError(id NodeID) *EdgeRouterConflictError {
	return &EdgeRouterConflictError{NodeID: id}
}

// NoEntryPointError is returned when no entry point was set.
type NoEntryPointError struct{}

func (e *NoEntryPointError) Error() string {
	return "graph compile: no entry point set"
}

func NewNoEntryPointError() *NoEntryPointError {
	return &NoEntryPointError{}
}

// NoReducerError is returned when no reducer was supplied.
type NoReducerError struct{}

func (e *NoReducerError) Error() string {
	return "graph compile: no reducer set"
}

func NewNoReducerError() *NoReducerError {
	return &NoReducerError{}
}

// RecursionLimitError is returned when the run exceeds the configured step budget.
// It replaces cycle rejection: cycles are legal, runaway recursion is not.
type RecursionLimitError struct {
	Limit    int
	Step     int
	Frontier []NodeID
}

func (e *RecursionLimitError) Error() string {
	return fmt.Sprintf(
		"graph run: recursion limit %d exceeded at step %d, frontier %v",
		e.Limit,
		e.Step,
		e.Frontier,
	)
}

func NewRecursionLimitError(limit, step int, frontier []NodeID) *RecursionLimitError {
	return &RecursionLimitError{Limit: limit, Step: step, Frontier: frontier}
}

// ReducerError wraps a failure returned by a reducer.
type ReducerError struct {
	NodeID NodeID
	Err    error
}

func (e *ReducerError) Error() string {
	return fmt.Sprintf("graph run: reducer for node %q failed: %v", e.NodeID, e.Err)
}

func (e *ReducerError) Unwrap() error { return e.Err }

func NewReducerError(id NodeID, err error) *ReducerError {
	return &ReducerError{NodeID: id, Err: err}
}

// NodeExecutionError wraps a failure returned by a node's Execute. Unwrap lets
// callers errors.As down to the underlying provider error.
type NodeExecutionError struct {
	NodeID NodeID
	Step   int
	Err    error
}

func (e *NodeExecutionError) Error() string {
	return fmt.Sprintf("graph run: node %q failed at step %d: %v", e.NodeID, e.Step, e.Err)
}

func (e *NodeExecutionError) Unwrap() error { return e.Err }

func NewNodeExecutionError(id NodeID, step int, err error) *NodeExecutionError {
	return &NodeExecutionError{NodeID: id, Step: step, Err: err}
}

// NodeErrorHandlerError reports that a configured recovery handler could not
// convert a node failure into an update. NodeErr preserves the original node
// failure and HandlerErr is returned by Unwrap.
type NodeErrorHandlerError struct {
	NodeID     NodeID
	Step       int
	NodeErr    error
	HandlerErr error
}

func (e *NodeErrorHandlerError) Error() string {
	return fmt.Sprintf(
		"graph run: error handler for node %q failed at step %d: %v (original node error: %v)",
		e.NodeID,
		e.Step,
		e.HandlerErr,
		e.NodeErr,
	)
}

func (e *NodeErrorHandlerError) Unwrap() error { return e.HandlerErr }

func NewNodeErrorHandlerError(
	id NodeID,
	step int,
	nodeErr, handlerErr error,
) *NodeErrorHandlerError {
	return &NodeErrorHandlerError{NodeID: id, Step: step, NodeErr: nodeErr, HandlerErr: handlerErr}
}

// RouterExecutionError is returned when a Router or FanRouter returns an error.
// It halts the run rather than leaving an undecidable frontier.
type RouterExecutionError struct {
	NodeID NodeID
	Step   int
	Err    error
}

func (e *RouterExecutionError) Error() string {
	return fmt.Sprintf(
		"graph run: router for node %q failed at step %d: %v",
		e.NodeID,
		e.Step,
		e.Err,
	)
}

func (e *RouterExecutionError) Unwrap() error { return e.Err }

func NewRouterExecutionError(id NodeID, step int, err error) *RouterExecutionError {
	return &RouterExecutionError{NodeID: id, Step: step, Err: err}
}

// NodePanicError is returned when caller code panicked and was recovered.
// Deliberately not a NodeExecutionError: a returned error is an anticipated
// condition, a panic is a bug. Callers should alert on one and retry the other.
// No Unwrap: the recovered value is an any, not necessarily an error.
type NodePanicError struct {
	NodeID NodeID
	Step   int
	Value  any
	Stack  []byte
}

func (e *NodePanicError) Error() string {
	return fmt.Sprintf("graph run: node %q panicked at step %d: %v", e.NodeID, e.Step, e.Value)
}

func NewNodePanicError(id NodeID, step int, value any, stack []byte) *NodePanicError {
	return &NodePanicError{NodeID: id, Step: step, Value: value, Stack: stack}
}
