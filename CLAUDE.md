# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building and Testing

- `go build ./...` - Build all packages
- `go test ./...` - Run all tests
- `go test ./pkg/graph -race` - Test the graph engine (race detector mandatory)
- `go test -run TestSpecificFunction` - Run specific test
- `go mod tidy` - Clean up dependencies

### Code Quality

- `go fmt ./...` - Format all Go code
- `go vet ./...` - Run static analysis
- `go mod verify` - Verify module integrity

### Running Examples

- `go run ./examples/multi-models/` - Run multi-provider example
- `go run ./examples/agent/` - Run agent example
- `go run ./examples/graph-simple/` - Run the minimal graph example (no API key needed)
- `go run ./examples/graph/` - Run graph orchestration example (draft→critique→publish loop)

## Architecture Overview

### Core Components

The code is split into focused packages by concern. Dependencies flow one way:
`providers` → `llm`; `agent` → `llm`; `graph/nodes` → `llm` + `agent` + `graph`.
`graph` depends on nothing internal, so it stays a domain-free engine.

**LLM Core** (`pkg/llm/`):

- `LLM` interface (`model.go`) — the contract every provider implements; two methods, `Complete()` and `Chat()`, both context-aware
- `Router` (`router.go`) — thread-safe registry mapping provider names to `LLM` implementations
- Main API (`gen.go`) — `Genevieve.Ask()` (single provider) and `AskAll()` (parallel fan-out)
- Provider/role error types (`errors.go`): `ProviderNotFoundError`, `ProviderRegistrationError`, `InvalidRoleError`

**Providers** (`pkg/providers/`):

- Concrete `llm.LLM` implementations for OpenAI, Anthropic, Google

**Agent System** (`pkg/agent/`):

- Autonomous agents that use tools (`agent.go`); prompts (`prompts.go`) and tool-selection schema (`schema.go`)
- Single tool execution per request (no chaining; multi-step orchestration lives in `pkg/graph`)
- Tool selection via LLM reasoning in the unexported `Agent.chooseTool` (builds on `llm.LLM.Chat`)
- Tool error types (`errors.go`): `ToolNotFoundError`, `ToolRegistrationError`

**Graph Engine** (`pkg/graph/`):

- Generic, domain-free orchestration engine — knows nothing about LLMs
- `Graph[T, U]` parameterized over caller state `T` and partial update `U`; nodes return deltas merged by a `Reducer`
- Reducer combinators (`reduce.go`) — `Merge` composes per-field rules (`Set`, `SetIf`, `Append`, `AppendIf`, `Concat`, `Add`, `Or`, `Apply`, predicate `NonZero`) so callers stop hand-writing zero-value-checking merge logic; `Merge` copies state once and `Append`/`Concat` copy slice backing arrays, keeping fan-in safe
- Two-phase: `Builder.Compile()` runs static analysis (dangling edges, unreachable nodes, dead ends) and returns an immutable `Runner` safe for concurrent reuse
- Super-step execution: each step runs the active frontier in parallel via `errgroup`, then applies reducers in deterministic node-ID order
- Supports sequential edges, parallel fan-out/fan-in, conditional/fan routing, bounded loops (recursion limit, not cycle rejection), `Stream`, checkpointing, `MapNode` map-reduce, and panic containment
- Fail-fast on the first node/router error; caller panics are recovered as `NodePanicError`

**Graph Bindings** (`pkg/graph/nodes/`):

- Adapters wiring the LLM packages into the graph engine: `LLMNode`, `ToolNode`, `AgentNode`
- Every export is a `graph.Node` constructor — state types live in `pkg/graph/chat`, not here
- Imports `llm`, `agent`, and `graph` — never the reverse — so `pkg/graph` stays domain-free with no import cycle

**Chat State** (`pkg/graph/chat/`):

- `State`/`Update`/`Reducer` batteries-included conversation default, for graphs whose state is just `[]llm.Message`
- State and reducer types, not nodes; a caller with richer state ignores this package and writes their own pair
- Imports `llm` and `graph` only — no dependency on `pkg/agent` or `pkg/graph/nodes`

### Key Architectural Patterns

**Provider Abstraction**: All LLM providers implement the same interface, allowing seamless swapping without code changes.

**Router Pattern**: Central registry pattern enables dynamic provider management and multi-provider operations.

**Agent-Tool Architecture**: Agents use LLMs to choose appropriate tools based on user queries, with JSON-based tool selection.

## Project Structure

```
pkg/
├── llm/                # LLM core: interface, router, main API, provider errors
│   ├── model.go        # LLM interface & types (Message, RoleType, options)
│   ├── router.go       # Provider registry
│   ├── gen.go          # Main API (Genevieve.Ask / AskAll)
│   └── errors.go       # Provider & role error types
├── agent/              # Agent system (depends on llm)
│   ├── agent.go        # Agent, AgentTool, tool selection
│   ├── prompts.go      # Agent prompts
│   ├── schema.go       # Tool selection schema
│   └── errors.go       # Tool error types
├── graph/              # Generic orchestration engine (no LLM knowledge)
│   ├── graph.go        # Node, NodeFunc, Router, Reducer, Builder
│   ├── reduce.go       # Reducer combinators (Merge, Set, Append, Add, ...)
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

examples/                # Usage examples
├── multi-models/       # Provider comparison
├── agent/              # Agent with tools
├── graph-simple/       # Smallest graph: two sequential nodes, no LLM, no API key
└── graph/              # Graph orchestration (draft→critique→publish loop)
    ├── main.go         # Topology only
    ├── workflow.go     # State, Update, reducer
    ├── prompts.go      # Prompt construction
    └── config.go       # Provider/router setup
```

## Development Notes

### Adding New LLM Providers

1. Implement the `llm.LLM` interface in `pkg/providers/yourprovider/`
2. Ensure both methods work: `Complete()` and `Chat()`
3. `Chat()` must return well-formed content — `agent.Agent.chooseTool` parses its JSON into `agent.AgentToolInput`
4. Follow existing provider patterns for configuration and error handling

### Adding New Agent Tools

1. Implement the `agent.AgentTool` interface
2. Provide meaningful `Name()` and handle JSON input in `Execute()`
3. Look at `examples/agent/tools/` for reference implementations

### Current Limitations

- Single tool execution per agent request (no chaining) — addressed by the `pkg/graph` orchestration engine for multi-step workflows
- Plain string responses (no metadata)
- No structured logging or observability (graph observability hooks planned as `Middleware[T, U]`)
- No retry logic or rate limiting (graph `WithMaxParallel` bounds per-run concurrency)

### Testing Strategy

- `pkg/agent`, `pkg/graph/nodes`, `pkg/graph/chat`, and `pkg/graph` have table-driven unit tests with hand-written mocks (`mockTool`, `mockLLM`, `mockNode`)
- Run the graph suite under the race detector: `go test ./pkg/graph -race` (parallel node execution over shared state is exactly the bug class it catches)
- Provider code is still tested manually via the `examples/` directory (needs real API keys)

### Dependencies

Core LLM SDKs:

- `github.com/openai/openai-go`
- `github.com/anthropics/anthropic-sdk-go`
- `google.golang.org/genai`

Utilities:

- `github.com/joho/godotenv` - Environment configuration
- `github.com/kelseyhightower/envconfig` - Config parsing
- `github.com/sirupsen/logrus` - Logging (minimal usage currently)
- `golang.org/x/sync` - `errgroup` for graph super-step concurrency
