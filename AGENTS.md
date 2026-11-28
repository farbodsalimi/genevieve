# AGENTS.md

Rules and reference for working in this repo. The "Gotchas" section covers what trips an agent up; the rest is architecture depth.

## Quick commands

```
go build ./...              # verify compilation
go test ./...               # run all tests
go test ./pkg/graph -race   # graph engine — race detector mandatory
go test -run TestFoo        # single test
go mod tidy                 # clean deps
go fmt ./...                # formatter (no other formatting config)
go vet ./...                # static analysis
go mod verify               # verify module integrity
```

### Running examples

```
go run ./examples/multi-models/   # multi-provider comparison
go run ./examples/agent/          # agent with tools
go run ./examples/graph-simple/   # minimal graph, no API key needed
go run ./examples/graph/          # intake → parallel research → review graph
```

## Gotchas an agent would miss

### JSON encoding: always `encoding/json/v2`

Always import `encoding/json/v2`, never `encoding/json`. This applies to all new and modified code, tests, and examples.

### Graph reducers must copy-slice before appending

A reducer is handed state that parallel nodes may still be reading. **Copy the backing array**:

```go
next := s
next.Steps = slices.Clone(s.Steps)
next.Steps = append(next.Steps, u.Step)
return next, nil
```

See `examples/graph/workflow.go` and `pkg/graph/chat` for working patterns, and `README.md "Writing a reducer"` for the zero-value guard convention.

### Adding an LLM provider requires structured tool round-tripping

1. Implement both `Generate` and `Stream` from the `llm.LLM` interface
2. Translate structured messages, tool definitions/calls, usage, and thinking effort; preserve assistant tool-call IDs and correlate tool-result messages; pass each tool's JSON Schema
3. Emit text, tool-call, and final usage events from `Stream`
4. Follow existing provider patterns for configuration and error handling — compare against `pkg/providers/openai/`, `anthropic/`, and `google/`

### Adding an agent tool

1. Implement the `agent.AgentTool` interface
2. Provide a description, object JSON Schema, terminal flag, and JSON execution contract
3. See `examples/agent/tools/` for reference implementations

### Testing

- Unit tests in `pkg/agent`, `pkg/graph/nodes`, `pkg/graph/chat`, `pkg/graph` use table-driven tests with hand-written mocks (`mockTool`, `mockLLM`, `mockNode`).
- Graph tests **must** run with `-race` — parallel node execution over shared state is exactly the bug class it catches.
- Provider integration is tested manually via `examples/`. No provider unit tests exist — they need real API keys.

### Env / config

Examples read API keys from `.env` or OS env. Run `graph-simple` to verify the code works without any keys.

## Architecture

Dependency direction is strict and intentional:

```
providers → llm ← agent ← graph/nodes
                              ↖ graph/chat (State/Update/Reducer for conversation graphs)
graph — depends on nothing internal (domain-free engine)
```

`graph/nodes` imports `llm`, `agent`, and `graph`; `graph/chat` imports `llm` and `graph` only.

### LLM core (`pkg/llm/`)

- `LLM` interface (`model.go`) — structured `Generate` and event-based `Stream`, including tool calls, usage, and thinking effort
- `Router` (`router.go`) — thread-safe registry mapping provider names to `LLM` implementations
- Public API (`gen.go`) — `Genevieve.Ask()` (single provider) and `AskAll()` (parallel fan-out)
- Error types (`errors.go`): `ProviderNotFoundError`, `ProviderRegistrationError`, `InvalidRoleError`

### Providers (`pkg/providers/`)

Concrete `llm.LLM` implementations for OpenAI, Anthropic, Google. Providers return native structured tool calls; the agent executes them and feeds correlated results back.

### Agent system (`pkg/agent/`)

- Bounded multi-turn tool loop with per-tool JSON Schema, terminal tools, usage accounting, token budgets, and budget wrap-up
- Runs until completion, a terminal tool, or a bound is reached
- Typed streaming events and tool/event middleware provide SSE and PII wrapping seams
- Provider-neutral thinking effort is mapped by each provider adapter
- Error types (`errors.go`): `ToolNotFoundError`, `ToolRegistrationError`

### Graph engine (`pkg/graph/`)

- Generic, domain-free orchestration engine — knows nothing about LLMs
- `Graph[T, U]` parameterized over caller state `T` and partial update `U`; nodes return deltas merged by a `Reducer`
- Reducers are hand-written per caller via `graph.ReducerFunc` — there are no combinators (see copy-slice gotcha above)
- Two-phase: `Builder.Compile()` runs static analysis (dangling edges, unreachable nodes, dead ends) and returns an immutable `Runner` safe for concurrent reuse
- Super-step execution (`runner.go`): each step runs the active frontier in parallel via `errgroup`, then applies reducers in deterministic node-ID order
- Supports sequential edges, parallel fan-out/fan-in, conditional/fan routing, bounded loops (recursion limit, not cycle rejection), `Stream`, checkpointing, `MapNode` map-reduce, and panic containment
- Fail-fast by default; `WithNodeErrorHandler` can convert node failures into updates so siblings and downstream nodes continue. Router/reducer errors remain fail-fast. Caller panics are recovered as `NodePanicError`

### Graph bindings (`pkg/graph/nodes/`)

- Only adapters wiring the LLM packages into the graph engine: `LLMNode`, `ToolNode`, `AgentNode`
- Every export is a `graph.Node` constructor — state types live in `pkg/graph/chat`, not here

### Chat state (`pkg/graph/chat/`)

- `State`/`Update`/`Reducer` batteries-included conversation default, for graphs whose state is just `[]llm.Message`
- State and reducer types, not nodes; a caller with richer state ignores this package and writes their own pair

## Project structure

```
pkg/
├── llm/                # LLM core: interface, router, main API, provider errors
│   ├── model.go        # LLM interface & types (Message, RoleType, options)
│   ├── router.go       # Provider registry
│   ├── gen.go          # Main API (Genevieve.Ask / AskAll)
│   └── errors.go       # Provider & role error types
├── agent/              # Agent system (depends on llm)
│   ├── agent.go        # Agent loop, tools, budgets, events, middleware
│   └── errors.go       # Tool error types
├── graph/              # Generic orchestration engine (no LLM knowledge)
│   ├── graph.go        # Node, NodeFunc, Router, Reducer, ReducerFunc, Builder
│   ├── compile.go      # Static analysis → Runner
│   ├── runner.go       # Super-step execution, Run + Stream + Resume
│   ├── mapnode.go      # MapNode map-reduce with independent parallelism limit
│   ├── checkpoint.go   # Checkpointer interface + MemoryCheckpointer
│   ├── errors.go       # Graph error types
│   ├── nodes/          # llm/agent ⇄ graph adapters (depends on llm, agent, graph)
│   │   └── nodes.go    # LLMNode, ToolNode, AgentNode
│   └── chat/           # Conversation state default (depends on llm, graph)
│       └── chat.go     # State, Update, Reducer
└── providers/          # llm.LLM implementations
    ├── openai/
    ├── anthropic/
    └── google/

examples/
├── multi-models/       # Provider comparison
├── agent/              # Agent with tools
├── graph-simple/       # Smallest graph: two sequential nodes, no LLM, no API key
└── graph/              # Agentic research workflow with parallel fan-out and review loop
    ├── main.go         # Topology only
    ├── workflow.go     # State, Update, reducer
    ├── prompts.go      # Prompt construction
    └── config.go       # Provider/router setup
```

## Current limitations

- No structured logging or observability (graph observability hooks planned as `Middleware[T, U]`)
- No built-in retry policy; agent tool middleware and graph node wrappers can supply one

## Dependencies

Core LLM SDKs:

- `github.com/openai/openai-go`
- `github.com/anthropics/anthropic-sdk-go`
- `google.golang.org/genai`

Utilities:

- `github.com/joho/godotenv` — environment configuration
- `github.com/kelseyhightower/envconfig` — config parsing
- `github.com/sirupsen/logrus` — logging (minimal usage currently)
- `golang.org/x/sync` — `errgroup` for graph super-step concurrency
