# Durable run store + run-management APIs for graphs and agents

## Context

An earlier iteration built an in-memory SSE *visualization* dashboard. That was the wrong
target. What's actually needed: **persist the state of running graphs (and agents) to an
external store (Redis now, DynamoDB later) and expose HTTP APIs to start, list, inspect,
resume, and cancel those runs.**

The engine already has the right seam. `pkg/graph/checkpoint.go` defines
`Checkpointer[T]` (`Save`/`LoadLatest`/`History`) and ships only `MemoryCheckpointer`; its
own comment says *"Redis/Postgres are the caller's problem."* `Runner.RunWithThread` and
`Runner.Resume` already drive checkpoint save/restore keyed by a thread ID.

But three gaps block "APIs for running graphs/agents":

1. **No run status** — `Checkpoint[T]` holds only `{ThreadID, Frontier, Step, State}`; nothing records running/succeeded/failed/canceled.
2. **No enumeration** — `Checkpointer[T]` can't list threads, so "show running graphs" is impossible today.
3. **Agents have no identity or persistence** — `agent.Agent.Run` is single-shot and stateless. It does expose `Callbacks{OnText,OnToolCall,OnUsage}` and a rich `Result{Messages,Usage,Turns,StopReason}` we can snapshot.

The design closes these with a byte-oriented `Store`, a non-generic `Manager` lifecycle
layer, and a chi REST API — reusing the existing generic `Checkpointer[T]` seam via a thin
JSON adapter so the store stays type-agnostic.

## Decisions

- Backing store: **pluggable `Store` interface + Redis first** (go-redis). DynamoDB later behind the same interface. `MemoryStore` stays as the no-dependency default/test backend.
- **Location: the interface and all plugins live under `internal/storage`.** The lifecycle and HTTP layers live alongside as `internal/runs` and `internal/api` — this is application wiring, not public library API. The only public seam touched is the already-public `graph.Checkpointer[T]`.
- Scope: **graphs + agents** in pass one.
- **Run manager with status** (`RunRecord` + lifecycle), not just raw persistence.
- APIs: **start, list, get, history, resume, cancel**, plus **graph topology inspect**.
- **No streaming.** The dashboard **polls the REST APIs** on a short interval while a run is
  active — no SSE, no event bus. Node-level liveness is derived from the persisted
  `Frontier`, not an event stream (see below).
- **The dashboard is refactored into a thin API client.** It no longer owns its own
  in-process `Stream`/`Hub`; it renders runs from the store/Manager over the HTTP APIs. The
  earlier `pkg/graph/topology.go` accessor is **kept** and reused by the topology endpoint.
  The old `internal/server` package is absorbed into `internal/api` (the embedded page moves
  there). The old live `Hub`/`Instrument`/`Event` machinery is **dropped entirely**.

## Architecture

Key insight — **type-erase at the store boundary with JSON.** The `Store` deals only in
`json.RawMessage` state blobs + non-generic metadata, so one store and one HTTP surface
serve every graph type and agents uniformly. Generics stay confined to a per-graph adapter.

```
Dashboard (API client, served by internal/api) — polls on a timer while a run is active
    │  POST /runs, GET /runs, GET /runs/{id}, GET /runs/{id}/history, GET /graphs/{name}/topology
    v
HTTP (chi)  ── internal/api ──>  Manager (internal/runs, non-generic)
                                   │  drives runs in goroutines; tracks status + cancel funcs
                                   ├─ Runnable (graph)  -> graphCheckpointer[T] -> Store
                                   └─ Runnable (agent)  -> agent.Callbacks       -> Store
                                              Store (internal/storage): Memory | Redis
```

One update channel: the **durable `Store`.** Super-step snapshots carry `State`, `Step`, and
`Frontier`; `RunRecord` carries status. The dashboard polls `GET /runs` and
`GET /runs/{id}`, highlighting the **active frontier nodes** from the latest snapshot's
`Frontier` against the topology — live topology lighting with no event stream. (With
`WithCheckpointInterval(1)` every super-step is persisted, so polling sees each step.)

### `internal/storage/` (package `storage`)

The Store interface and its plugins. Non-generic, JSON/bytes. Follows the `llm.Router`
RWMutex registry pattern (also used by `MemoryCheckpointer`).

- `storage.go`: types + interface

  ```go
  type Status string // "running","succeeded","failed","canceled"
  type RunRecord struct {
      ID, Kind, Name string        // Kind: "graph"|"agent"
      Status Status
      Step   int
      Frontier []string            // graph only
      Error  string
      StartedAt, UpdatedAt time.Time
  }
  type Snapshot struct {
      RunID string; Step int
      Frontier []string            // graph only; needed to rebuild graph.Checkpoint on resume
      State json.RawMessage        // serialized T (graph) or agent conversation snapshot
      At time.Time
  }
  type Filter struct { Kind string; Status Status } // optional list filters
  type Store interface {
      PutRun(ctx, RunRecord) error
      GetRun(ctx, id string) (RunRecord, error)
      ListRuns(ctx, Filter) ([]RunRecord, error)
      AppendSnapshot(ctx, Snapshot) error
      LatestSnapshot(ctx, runID string) (Snapshot, error)
      History(ctx, runID string) ([]Snapshot, error)
  }
  ```

- `memory.go`: `MemoryStore` (RWMutex + maps), mirrors `MemoryCheckpointer` structure.
  DynamoDB lands later as `dynamo.go` in this same package, behind the same interface.
- `redis.go`: `RedisStore` over `github.com/redis/go-redis/v9`. Layout:
  - `run:{id}` → JSON string of `RunRecord`.
  - `runs` → SET of run IDs; `runs:status:{status}` / `runs:kind:{kind}` → SETs for `ListRuns` filtering.
  - `run:{id}:snapshots` → LIST (RPUSH). `LatestSnapshot`=`LINDEX -1`; `History`=`LRANGE 0 -1`.
- `storage_test.go`: table-driven CRUD + list-filter tests run against **both** `MemoryStore` and a `RedisStore` backed by `github.com/alicebob/miniredis/v2` (hermetic, test-only dep).

### `internal/runs/` (package `runs`)

The lifecycle layer. Depends on `internal/storage`, `pkg/graph`, `pkg/agent` (a higher
layer than all three; no import cycle, same shape as `pkg/graph/nodes`).

- `manager.go`:

  ```go
  type Runnable interface {
      Start(ctx context.Context, runID string, initial json.RawMessage) error
      Resume(ctx context.Context, runID string) error // graphs; agents return ErrNotResumable
  }
  type Manager struct { /* store, mu, runnables map[name]Runnable, cancels map[runID]context.CancelFunc */ }
  func (m *Manager) Register(name string, r Runnable)
  func (m *Manager) Start(ctx, kind, name string, initial json.RawMessage) (runID string, err error)
  func (m *Manager) Get(ctx, runID) (storage.RunRecord, storage.Snapshot, error)
  func (m *Manager) List(ctx, storage.Filter) ([]storage.RunRecord, error)
  func (m *Manager) Resume(ctx, runID) error
  func (m *Manager) Cancel(ctx, runID) error
  ```

  `Start` writes a `running` `RunRecord`, launches the runnable in a goroutine under a
  cancelable context tracked in `cancels`, and on return flips status to
  succeeded/failed/canceled + `UpdatedAt`. `Cancel` calls the tracked cancel func. No event
  bus — progress is observed by polling the store.

- `graph.go`:

  ```go
  // RegisterGraph wires a typed graph under name. build compiles the runner with the
  // supplied store-backed checkpointer (graph.WithCheckpointer) + WithCheckpointInterval(1).
  func RegisterGraph[T any, U any](
      m *Manager, name string,
      build func(cp graph.Checkpointer[T]) (*graph.Runner[T, U], error),
      initialFromJSON func([]byte) (T, error),
  ) error
  ```

  Internally a `graphCheckpointer[T]{store}` implements `graph.Checkpointer[T]`: `Save`
  JSON-marshals `cp.State` → `AppendSnapshot` (+ updates `RunRecord.Step/Frontier`);
  `LoadLatest`/`History` unmarshal back into `graph.Checkpoint[T]` (rebuilding `Frontier`
  from `[]string`). The runnable calls `runner.RunWithThread(ctx, runID, state)` /
  `runner.Resume(ctx, runID)`. Thread ID == run ID flows through `cp.ThreadID`, so one
  adapter instance serves all runs of that graph. Nodes are **not** wrapped — progress is
  captured by the checkpointer alone.

  At registration the Manager caches `runner.Topology()` (JSON) under the graph name — this
  is what `GET /graphs/{name}/topology` returns, and why `pkg/graph/topology.go` is retained.
  The dashboard overlays each polled snapshot's `Frontier` on this topology to highlight
  active nodes.

- `agent.go`:

  ```go
  func RegisterAgent(m *Manager, name string, a *agent.Agent, reqFromJSON func([]byte) (agent.RunRequest, error)) error
  ```

  Runnable `Start` injects `agent.Callbacks` that append conversation snapshots to the
  store as text/tool/usage events arrive, runs `a.Run`, then snapshots the final
  `Result{Messages,Usage,Turns,StopReason}` as JSON state. `Resume` returns
  `ErrNotResumable` (agents can't resume mid-turn; documented).

- `manager_test.go`, `graph_test.go`: lifecycle tests with `MemoryStore` + a tiny real
  graph — start→succeeded, resume-from-snapshot equivalence, cancel→canceled.

### `internal/api/` — HTTP API + dashboard (chi, mounted by `cmd/server`)

REST over the Manager:
- `POST /runs` `{kind,name,initial}` → `202 {id}`
- `GET  /runs?status=&kind=` → `[]RunRecord`
- `GET  /runs/{id}` → `{record, latestState}`
- `GET  /runs/{id}/history` → `[]Snapshot`
- `POST /runs/{id}/resume` → `202` (graph only; `400` for agents)
- `POST /runs/{id}/cancel` → `202`
- `GET  /graphs` → registered graph names
- `GET  /graphs/{name}/topology` → cached `graph.Topology` JSON

Dashboard (absorbed from `internal/server`):
- `GET  /` → the embedded `index.html`, now an **API client** that **polls**: lists runs via
  `GET /runs`, starts via `POST /runs`, polls `GET /runs/{id}` (+ `/history`) on a ~750ms
  timer while a run is `running`, renders structure from `GET /graphs/{name}/topology` and
  highlights the active nodes from the latest snapshot's `Frontier`, and drives resume/cancel
  via the API. The old `Dashboard[T,U]`, in-process `Stream` loop, self-owned `Hub`, and SSE
  are gone — the page talks only plain HTTP (fetch/poll).

### `cmd/server` wiring

- `main.go`: pick store from env — `STORE=memory` (default) or `STORE=redis` + `REDIS_ADDR`;
  construct `Manager`; `RegisterGraph` the existing `demo.go` graph (add `initialFromJSON`
  and a `build` closure that compiles the runner with the injected store checkpointer +
  `WithCheckpointInterval(1)`) and `RegisterAgent` a small demo agent; mount `internal/api`
  at `/`.
- `internal/server` is **deleted**; its `index.html` moves to `internal/api` (repointed to
  poll the APIs). The old `Hub`/`Instrument`/`Event`/`noCache` are dropped, not relocated.

### Dependencies

- Add `github.com/redis/go-redis/v9`.
- Add test-only `github.com/alicebob/miniredis/v2`.
- DynamoDB (`aws-sdk-go-v2`) deferred — second `Store` impl behind the same interface.

## Files

- New: `internal/storage/{storage,memory,redis,storage_test}.go`
- New: `internal/runs/{manager,graph,agent,manager_test,graph_test}.go`
- New: `internal/api/{api,api_test}.go` + `index.html` (moved from `internal/server`, repointed to poll the APIs)
- Delete: `internal/server/` (dashboard.go, handlers.go, routes.go, middleware.go, page.go, index.html) — page absorbed into `internal/api`; `Hub`/`Instrument`/`Event`/`noCache` dropped
- Keep: `pkg/graph/topology.go` + `topology_test.go` (now backing `GET /graphs/{name}/topology`)
- Modify: `cmd/server/main.go`, `cmd/server/demo.go` (add `initialFromJSON` + checkpointer-injecting `build`), `go.mod`/`go.sum`
- Reused seams: `graph.Checkpointer[T]`/`Checkpoint[T]`/`WithCheckpointer`/`WithCheckpointInterval`/`Topology()` (`pkg/graph`), `Runner.RunWithThread`/`Resume`, `agent.Agent.Run`/`Callbacks`/`Result` (`pkg/agent`), RWMutex registry pattern from `pkg/llm/router.go`.

## Constraints / limitations (to document in code)

- **State `T` must be JSON round-trippable** (exported fields). The demo state and
  `pkg/graph/chat.State` already qualify. Noted on `RegisterGraph`.
- **Cancel is in-process** (cancel funcs live in the owning instance's memory). Multi-instance
  cancel needs a pub/sub signal — out of scope; noted.
- **No heartbeat** — a crashed process leaves a run stuck in `running`. A TTL/heartbeat is a
  follow-up; noted.
- **Agents are not checkpoint-resumable** — `resume` is rejected for `kind=agent`.

## Verification

1. `go build ./...`, `go vet ./...`.
2. Unit: `go test ./internal/storage ./internal/runs ./internal/api` — MemoryStore +
   miniredis-backed RedisStore pass the same suite; manager start/resume/cancel lifecycle;
   graph checkpoint round-trip equals an uninterrupted run (reuse the equivalence pattern
   from `pkg/graph`'s existing `TestResume_*`).
3. Manual API, memory backend (no deps): `go run ./cmd/server`, then
   `curl -XPOST :3000/runs -d '{"kind":"graph","name":"demo","initial":{...}}'`,
   `curl :3000/runs`, `GET /runs/{id}`, `/history`, `POST /resume`, `POST /cancel` — confirm
   status transitions and that `/runs/{id}` reflects advancing `Step`/`Frontier` as it runs.
4. Dashboard end-to-end: open `http://localhost:3000/`, start a run from the UI, confirm the
   topology renders from `GET /graphs/{name}/topology`, active nodes highlight from the polled
   snapshot `Frontier`, and the run shows in the list with its status — i.e. the page is
   driven entirely by the APIs (verify in devtools Network that it makes only `fetch` polls,
   no EventSource).
5. Redis backend: `docker run -p6379:6379 redis`,
   `STORE=redis REDIS_ADDR=localhost:6379 go run ./cmd/server`; repeat (3)–(4); inspect with
   `redis-cli KEYS 'run:*'` and `LRANGE run:{id}:snapshots 0 -1`.
