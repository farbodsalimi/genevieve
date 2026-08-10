package graph

// This file provides composable reducer building blocks. A hand-written Reducer
// is a pile of field-by-field merge logic that every caller re-derives, and the
// common "skip zero values" convention it usually encodes is both unstated and
// wrong for bool and numeric fields, where the zero value is meaningful data.
//
// Merge composes explicit per-field Fields instead. Each Field declares one
// state field, where its value comes from in the update, and how the two
// combine, so the merge policy is visible at the call site rather than implied
// by a chain of `if u.X != ""` checks.

// Field is a single field-level merge rule: given the update, mutate the
// addressed field of the (already copied) state. Callers construct these with
// Set, SetIf, Append, AppendIf, Add, Or, and Apply rather than implementing the
// type directly.
//
// Merge hands each Field a pointer into a copy of the current state, never the
// caller's state, so a Field may write freely.
type Field[T any, U any] func(dst *T, update U)

// Merge builds a Reducer from field rules. It copies the incoming state once,
// applies every field in order, and returns the copy — the input state is never
// mutated, so a node reading the previous state concurrently is unaffected.
//
// The copy is shallow. For a slice or map field, use Append (which copies the
// backing array) or a custom Apply that copies before writing; assigning a
// slice from the update directly would share its backing array with the state
// that produced it.
func Merge[T any, U any](fields ...Field[T, U]) Reducer[T, U] {
	return ReducerFunc[T, U](func(s T, u U) (T, error) {
		out := s
		for _, f := range fields {
			f(&out, u)
		}
		return out, nil
	})
}

// Set unconditionally overwrites a field with a value derived from the update.
// Use it when every visit to the producing node is meant to replace the field,
// including with a zero value.
//
//	graph.Set(func(s *State) *string { return &s.Draft },
//	          func(u Update) string { return u.Draft })
func Set[T any, U any, V any](field func(*T) *V, value func(U) V) Field[T, U] {
	return func(dst *T, u U) { *field(dst) = value(u) }
}

// SetIf overwrites a field only when when reports true for the update. This is
// the explicit form of the usual "skip the zero value" convention: a node that
// did not produce this field leaves it alone.
//
//	graph.SetIf(func(s *State) *string { return &s.Draft },
//	            func(u Update) string { return u.Draft },
//	            graph.NonZero[string])
func SetIf[T any, U any, V any](field func(*T) *V, value func(U) V, when func(V) bool) Field[T, U] {
	return func(dst *T, u U) {
		v := value(u)
		if when(v) {
			*field(dst) = v
		}
	}
}

// Append copies a slice field and appends one element derived from the update.
// The copy is what makes concurrent fan-in safe: two nodes appending in the
// same super-step each reduce against a state whose backing array they do not
// share with anyone else.
func Append[T any, U any, V any](field func(*T) *[]V, value func(U) V) Field[T, U] {
	return func(dst *T, u U) {
		cur := field(dst)
		next := make([]V, len(*cur), len(*cur)+1)
		copy(next, *cur)
		*cur = append(next, value(u))
	}
}

// AppendIf appends only when when reports true for the derived element, so a
// node that produced no value for this field does not append an empty one.
func AppendIf[T any, U any, V any](field func(*T) *[]V, value func(U) V, when func(V) bool) Field[T, U] {
	return func(dst *T, u U) {
		v := value(u)
		if !when(v) {
			return
		}
		cur := field(dst)
		next := make([]V, len(*cur), len(*cur)+1)
		copy(next, *cur)
		*cur = append(next, v)
	}
}

// Concat copies a slice field and appends every element of a slice derived from
// the update. Use it for nodes that emit a batch rather than a single element.
func Concat[T any, U any, V any](field func(*T) *[]V, values func(U) []V) Field[T, U] {
	return func(dst *T, u U) {
		vs := values(u)
		if len(vs) == 0 {
			return
		}
		cur := field(dst)
		next := make([]V, len(*cur), len(*cur)+len(vs))
		copy(next, *cur)
		*cur = append(next, vs...)
	}
}

// Number is the set of types Add can accumulate.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Add accumulates a numeric field by the delta derived from the update. This is
// the counter case that a zero-value-skipping reducer cannot express: a delta of
// 0 and "no delta" are the same instruction, so nothing is lost by always
// adding.
//
//	graph.Add(func(s *State) *int { return &s.Revisions },
//	          func(u Update) int { if u.Revised { return 1 }; return 0 })
func Add[T any, U any, V Number](field func(*T) *V, delta func(U) V) Field[T, U] {
	return func(dst *T, u U) { *field(dst) += delta(u) }
}

// Or latches a boolean field: once true it stays true, so a flag set by one
// branch survives a later branch that did not set it.
func Or[T any, U any](field func(*T) *bool, value func(U) bool) Field[T, U] {
	return func(dst *T, u U) { *field(dst) = *field(dst) || value(u) }
}

// Apply is the escape hatch for a merge no other combinator covers — merging a
// map, or deriving one field from several update fields at once. The function
// receives a pointer into the state copy Merge already made, so it may mutate
// freely, but it must copy any slice or map it takes from the update before
// storing it.
func Apply[T any, U any](fn func(*T, U)) Field[T, U] {
	return Field[T, U](fn)
}

// NonZero reports whether v differs from its type's zero value. It is the
// predicate for SetIf and AppendIf that reproduces the usual "a node that
// didn't touch this field left it empty" convention — now stated at the call
// site instead of assumed.
func NonZero[V comparable](v V) bool {
	var zero V
	return v != zero
}
