# AGENTS.md

Rules for working in this repo. If a line could be guessed from filenames, leave it out.

## Quick commands

```
go build ./...          # verify compilation
go test ./...           # run all tests
go test ./pkg/graph -race   # graph engine — race detector mandatory
go test -run TestFoo    # single test
go mod tidy             # clean deps
go fmt ./...            # formatter (no other formatting config)
```

## Architecture at a glance

Dependency direction is strict and intentional:

```
providers → llm ← agent ← graph/nodes
                              ↖ graph/chat (State/Update/Reducer for conversation graphs)
graph — depends on nothing internal (domain-free engine)
```

- `pkg/llm/gen.go` — public API (`Genevieve.Ask`, `AskAll`)
- `pkg/llm/router.go` — thread-safe provider registry
- `pkg/graph/runner.go` — super-step execution with parallel frontier + deterministic reducer application
- `pkg/graph/nodes/nodes.go` — only adapters (`LLMNode`, `ToolNode`, `AgentNode`)

## Gotchas an agent would miss

### Graph reducers must copy-slice before appending

A reducer is handed state that parallel nodes may still be reading. **Copy the backing array**:

```go
next := s
next.Steps = slices.Clone(s.Steps)
next.Steps = append(next.Steps, u.Step)
return next, nil
```

See `examples/graph/workflow.go` for a working pattern and `README.md "Writing a reducer"` for the zero-value guard convention.

### Adding an LLM provider requires structured tool round-tripping

Implement both `Generate` and `Stream`. Preserve assistant tool-call IDs and
correlate tool-result messages, pass each tool's JSON Schema, report usage, map
thinking effort, and emit text/tool/usage stream events. Compare against
`pkg/providers/openai/`, `anthropic/`, and `google/`.

### Testing

- Unit tests in `pkg/agent`, `pkg/graph/nodes`, `pkg/graph/chat`, `pkg/graph` use table-driven tests with hand-written mocks (`mockTool`, `mockLLM`, `mockNode`).
- Provider integration is tested manually via `examples/`. No provider unit tests exist — they need real API keys.
- Graph tests **must** run with `-race`.

### Env / config

Examples read API keys from `.env` or OS env. Run `graph-simple` to verify the code works without any keys.

## What's in CLAUDE.md but not here

CLAUDE.md (in repo root) has the full architecture reference, package listing, provider/tool-adding guides, and dependency table. Read it when you need depth — this file only covers what trips an agent up.
