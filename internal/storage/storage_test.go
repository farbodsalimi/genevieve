package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newStores returns one factory per Store implementation so the same suite runs
// against MemoryStore and a miniredis-backed RedisStore.
func newStores(t *testing.T) map[string]Store {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return map[string]Store{
		"memory": NewMemoryStore(),
		"redis":  NewRedisStore(rdb),
	}
}

func run(id, kind, name string, status Status, started time.Time) RunRecord {
	return RunRecord{ID: id, Kind: kind, Name: name, Status: status, StartedAt: started, UpdatedAt: started}
}

func TestStore_RunCRUD(t *testing.T) {
	ctx := context.Background()
	for name, s := range newStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.GetRun(ctx, "missing"); !isNotFound(err) {
				t.Fatalf("GetRun(missing) = %v, want NotFoundError", err)
			}
			r := run("r1", KindGraph, "demo", StatusRunning, time.Unix(100, 0))
			if err := s.PutRun(ctx, r); err != nil {
				t.Fatalf("PutRun: %v", err)
			}
			got, err := s.GetRun(ctx, "r1")
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if got.Name != "demo" || got.Status != StatusRunning {
				t.Fatalf("round trip mismatch: %+v", got)
			}
		})
	}
}

func TestStore_ListAndFilter(t *testing.T) {
	ctx := context.Background()
	for name, s := range newStores(t) {
		t.Run(name, func(t *testing.T) {
			_ = s.PutRun(ctx, run("g1", KindGraph, "demo", StatusRunning, time.Unix(100, 0)))
			_ = s.PutRun(ctx, run("g2", KindGraph, "demo", StatusSucceeded, time.Unix(300, 0)))
			_ = s.PutRun(ctx, run("a1", KindAgent, "bot", StatusRunning, time.Unix(200, 0)))

			all, _ := s.ListRuns(ctx, Filter{})
			if len(all) != 3 {
				t.Fatalf("ListRuns all = %d, want 3", len(all))
			}
			// Newest first: g2(300), a1(200), g1(100).
			if all[0].ID != "g2" || all[1].ID != "a1" || all[2].ID != "g1" {
				t.Fatalf("order = %s,%s,%s", all[0].ID, all[1].ID, all[2].ID)
			}

			graphs, _ := s.ListRuns(ctx, Filter{Kind: KindGraph})
			if len(graphs) != 2 {
				t.Fatalf("kind=graph = %d, want 2", len(graphs))
			}
			running, _ := s.ListRuns(ctx, Filter{Status: StatusRunning})
			if len(running) != 2 {
				t.Fatalf("status=running = %d, want 2", len(running))
			}
			runningGraphs, _ := s.ListRuns(ctx, Filter{Kind: KindGraph, Status: StatusRunning})
			if len(runningGraphs) != 1 || runningGraphs[0].ID != "g1" {
				t.Fatalf("running graphs = %+v, want [g1]", runningGraphs)
			}
		})
	}
}

func TestStore_StatusReindex(t *testing.T) {
	ctx := context.Background()
	for name, s := range newStores(t) {
		t.Run(name, func(t *testing.T) {
			_ = s.PutRun(ctx, run("r1", KindGraph, "demo", StatusRunning, time.Unix(100, 0)))
			// Transition running -> succeeded.
			_ = s.PutRun(ctx, run("r1", KindGraph, "demo", StatusSucceeded, time.Unix(100, 0)))

			if running, _ := s.ListRuns(ctx, Filter{Status: StatusRunning}); len(running) != 0 {
				t.Fatalf("still listed as running: %+v", running)
			}
			done, _ := s.ListRuns(ctx, Filter{Status: StatusSucceeded})
			if len(done) != 1 || done[0].ID != "r1" {
				t.Fatalf("not reindexed to succeeded: %+v", done)
			}
		})
	}
}

func TestStore_Snapshots(t *testing.T) {
	ctx := context.Background()
	for name, s := range newStores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.LatestSnapshot(ctx, "r1"); !isNotFound(err) {
				t.Fatalf("LatestSnapshot(empty) = %v, want NotFoundError", err)
			}
			for step := 1; step <= 3; step++ {
				st, _ := json.Marshal(map[string]int{"step": step})
				snap := Snapshot{RunID: "r1", Step: step, Frontier: []string{"n"}, State: st, At: time.Unix(int64(step), 0)}
				if err := s.AppendSnapshot(ctx, snap); err != nil {
					t.Fatalf("AppendSnapshot: %v", err)
				}
			}
			latest, err := s.LatestSnapshot(ctx, "r1")
			if err != nil {
				t.Fatalf("LatestSnapshot: %v", err)
			}
			if latest.Step != 3 {
				t.Fatalf("latest step = %d, want 3", latest.Step)
			}
			hist, _ := s.History(ctx, "r1")
			if len(hist) != 3 || hist[0].Step != 1 || hist[2].Step != 3 {
				t.Fatalf("history = %+v, want steps 1..3 in order", hist)
			}
		})
	}
}

func isNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}
