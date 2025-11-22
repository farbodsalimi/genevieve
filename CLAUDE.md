# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building and Testing

- `go build ./...` - Build all packages
- `go test ./...` - Run all tests
- `go test ./pkg/genevieve` - Test core library only
- `go test -run TestSpecificFunction` - Run specific test
- `go mod tidy` - Clean up dependencies

### Code Quality

- `go fmt ./...` - Format all Go code
- `go vet ./...` - Run static analysis
- `go mod verify` - Verify module integrity

### Running Examples

- `go run ./examples/multi-models/` - Run multi-provider example
- `go run ./examples/agent/` - Run agent example

## Architecture Overview

### Core Components

**Router System** (`pkg/genevieve/router.go`):

- Central registry for LLM providers
- Maps provider names to LLM implementations
- Currently not thread-safe - see TODO comments

**Provider Interface** (`pkg/genevieve/model.go`):

- `LLM` interface defines contract for all providers
- Three main methods: `Complete()`, `Chat()`, `ChooseTool()`
- Implementations in `pkg/providers/` for OpenAI, Anthropic, Google

**Agent System** (`pkg/genevieve/agent.go`):

- Autonomous agents that can use tools
- Single tool execution per request (no chaining yet)
- Tool selection via LLM reasoning

**Main API** (`pkg/genevieve/gen.go`):

- `Ask()` - Query single provider
- `AskAll()` - Parallel queries to all registered providers

### Key Architectural Patterns

**Provider Abstraction**: All LLM providers implement the same interface, allowing seamless swapping without code changes.

**Router Pattern**: Central registry pattern enables dynamic provider management and multi-provider operations.

**Agent-Tool Architecture**: Agents use LLMs to choose appropriate tools based on user queries, with JSON-based tool selection.

## Project Structure

```
pkg/
├── genevieve/          # Core library
│   ├── model.go        # LLM interface & types
│   ├── router.go       # Provider registry
│   ├── gen.go          # Main API
│   ├── agent.go        # Agent system
│   ├── prompts.go      # Agent prompts
│   └── schema.go       # Tool selection schema
└── providers/          # LLM provider implementations
    ├── openai/
    ├── anthropic/
    └── google/

examples/                # Usage examples
├── multi-models/       # Provider comparison
└── agent/              # Agent with tools
```

## Development Notes

### Adding New LLM Providers

1. Implement the `LLM` interface in `pkg/providers/yourprovider/`
2. Ensure all three methods work: `Complete()`, `Chat()`, `ChooseTool()`
3. Handle JSON parsing for `ChooseTool()` - must return valid `AgentToolInput`
4. Follow existing provider patterns for configuration and error handling

### Adding New Agent Tools

1. Implement the `AgentTool` interface
2. Provide meaningful `Name()` and handle JSON input in `Execute()`
3. Look at `examples/agent/tools/` for reference implementations

### Current Limitations (See TODO.md)

- No context support (cancellation/timeouts)
- Not thread-safe
- Single tool execution per agent request
- Plain string responses (no metadata)
- No structured error types

### Testing Strategy

No formal test suite exists yet. Test manually using:

- Examples in `examples/` directory
- Provider-specific functionality
- Agent tool interactions

### Dependencies

Core LLM SDKs:

- `github.com/openai/openai-go`
- `github.com/anthropics/anthropic-sdk-go`
- `google.golang.org/genai`

Utilities:

- `github.com/joho/godotenv` - Environment configuration
- `github.com/kelseyhightower/envconfig` - Config parsing
- `github.com/op/go-logging` - Logging (minimal usage currently)
