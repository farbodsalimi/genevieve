package graph

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// MapOption configures a MapNode.
type MapOption func(*mapConfig)

type mapConfig struct {
	parallel int
}

// WithMapParallel bounds how many payloads a MapNode processes concurrently.
// Default unbounded. This limit is independent of the parent Runner's
// MaxParallel and must never draw from it: a MapNode runs as a frontier node, so
// it already holds one slot of the parent limit while awaiting its own
// sub-tasks. Drawing from the same limit would deadlock a node against a resource
// it is itself holding.
func WithMapParallel(n int) MapOption {
	return func(c *mapConfig) { c.parallel = n }
}

// MapNode runs inner once per payload, concurrently, then folds the N updates
// into a single U via combine. It is one node in the frontier, not N — the
// payload concept is confined here rather than added as a third type parameter
// on every Node signature.
//
// This is the map-reduce / dynamic fan-out construct: use it when a node must
// spawn N concurrent instances of the same work, one per payload, which a
// FanRouter (which returns a deduplicated set of distinct node IDs) cannot express.
func MapNode[T any, U any, P any](
	payloads func(T) []P,
	inner func(ctx context.Context, state T, payload P) (U, error),
	combine func([]U) (U, error),
	opts ...MapOption,
) Node[T, U] {
	cfg := mapConfig{parallel: 0} // 0 = unbounded
	for _, opt := range opts {
		opt(&cfg)
	}
	return NodeFunc[T, U](func(ctx context.Context, state T) (U, error) {
		var zero U
		ps := payloads(state)
		if len(ps) == 0 {
			return combine(nil)
		}

		updates := make([]U, len(ps))
		eg, gctx := errgroup.WithContext(ctx)
		if cfg.parallel > 0 {
			eg.SetLimit(cfg.parallel)
		}
		for i, p := range ps {
			eg.Go(func() error {
				u, err := inner(gctx, state, p)
				if err != nil {
					return err
				}
				updates[i] = u
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return zero, err
		}
		return combine(updates)
	})
}
