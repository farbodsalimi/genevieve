package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/redis/go-redis/v9"
)

var _ Store = &RedisStore{}

// RedisStore persists runs in Redis. Key layout:
//
//	run:{id}              STRING  JSON RunRecord
//	runs                 SET     all run IDs
//	runs:kind:{kind}     SET     run IDs by kind
//	runs:status:{status} SET     run IDs by status (kept current as status changes)
//	run:{id}:snapshots   LIST    JSON Snapshots, appended in order
type RedisStore struct {
	rdb *redis.Client
}

// NewRedisStore wraps an existing go-redis client.
func NewRedisStore(rdb *redis.Client) *RedisStore { return &RedisStore{rdb: rdb} }

func runKey(id string) string    { return "run:" + id }
func snapsKey(id string) string  { return "run:" + id + ":snapshots" }
func statusKey(s Status) string  { return "runs:status:" + string(s) }
func kindKey(kind string) string { return "runs:kind:" + kind }

const allRunsKey = "runs"

func (s *RedisStore) PutRun(ctx context.Context, r RunRecord) error {
	// Keep the status index current: if the run already exists under a different
	// status, drop it from the old status set.
	if prev, err := s.GetRun(ctx, r.ID); err == nil && prev.Status != r.Status {
		if err := s.rdb.SRem(ctx, statusKey(prev.Status), r.ID).Err(); err != nil {
			return err
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, runKey(r.ID), b, 0)
	pipe.SAdd(ctx, allRunsKey, r.ID)
	pipe.SAdd(ctx, kindKey(r.Kind), r.ID)
	pipe.SAdd(ctx, statusKey(r.Status), r.ID)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) GetRun(ctx context.Context, id string) (RunRecord, error) {
	b, err := s.rdb.Get(ctx, runKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return RunRecord{}, NewRunNotFoundError(id)
	}
	if err != nil {
		return RunRecord{}, err
	}
	var r RunRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return RunRecord{}, fmt.Errorf("storage: decode run %q: %w", id, err)
	}
	return r, nil
}

func (s *RedisStore) ListRuns(ctx context.Context, f Filter) ([]RunRecord, error) {
	// Choose the narrowest index set to enumerate, then intersect in-process.
	var sets []string
	if f.Status != "" {
		sets = append(sets, statusKey(f.Status))
	}
	if f.Kind != "" {
		sets = append(sets, kindKey(f.Kind))
	}
	var ids []string
	var err error
	switch len(sets) {
	case 0:
		ids, err = s.rdb.SMembers(ctx, allRunsKey).Result()
	case 1:
		ids, err = s.rdb.SMembers(ctx, sets[0]).Result()
	default:
		ids, err = s.rdb.SInter(ctx, sets...).Result()
	}
	if err != nil {
		return nil, err
	}

	out := make([]RunRecord, 0, len(ids))
	for _, id := range ids {
		r, err := s.GetRun(ctx, id)
		if err != nil {
			var nf *NotFoundError
			if errors.As(err, &nf) {
				continue // index drift; skip
			}
			return nil, err
		}
		if f.matches(r) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b RunRecord) int {
		if !a.StartedAt.Equal(b.StartedAt) {
			return b.StartedAt.Compare(a.StartedAt)
		}
		return slices.Compare([]string{a.ID}, []string{b.ID})
	})
	return out, nil
}

func (s *RedisStore) AppendSnapshot(ctx context.Context, snap Snapshot) error {
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.rdb.RPush(ctx, snapsKey(snap.RunID), b).Err()
}

func (s *RedisStore) LatestSnapshot(ctx context.Context, runID string) (Snapshot, error) {
	b, err := s.rdb.LIndex(ctx, snapsKey(runID), -1).Bytes()
	if errors.Is(err, redis.Nil) {
		return Snapshot{}, NewSnapshotNotFoundError(runID)
	}
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("storage: decode snapshot for %q: %w", runID, err)
	}
	return snap, nil
}

func (s *RedisStore) History(ctx context.Context, runID string) ([]Snapshot, error) {
	raws, err := s.rdb.LRange(ctx, snapsKey(runID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(raws))
	for _, raw := range raws {
		var snap Snapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			return nil, fmt.Errorf("storage: decode snapshot for %q: %w", runID, err)
		}
		out = append(out, snap)
	}
	return out, nil
}
