# Genevieve

Genevieve is a modular, extensible Go library for building agentic AI systems with a provider-agnostic interface to large language models (LLMs). It simplifies the creation of autonomous AI agents that can reason, plan, and act while seamlessly integrating with providers like OpenAI, Anthropic (Claude), and Google Gemini.

With Genevieve, developers can:

- Define and compose agent behaviors in Go
- Swap or combine LLM backends without changing core logic
- Orchestrate multi-step workflows as explicit graphs, with bounded loops and parallel fan-out

## Packages

| Package           | Purpose                                                                         |
| ----------------- | ------------------------------------------------------------------------------- |
| `pkg/llm`         | The `LLM` interface, provider router, and the `Genevieve` API (`Ask`, `AskAll`) |
| `pkg/providers`   | Concrete providers: OpenAI, Anthropic, Google                                   |
| `pkg/agent`       | Bounded multi-turn agents with tools, budgets, streaming, and middleware        |
| `pkg/graph`       | A generic, domain-free orchestration engine — knows nothing about LLMs          |
| `pkg/graph/nodes` | Adapters wiring the LLM packages into the graph engine                          |
| `pkg/graph/chat`  | Batteries-included conversation state (`State`, `Update`, `Reducer`)            |

## Examples

### Provider-agnostic Interface

```go
ctx := context.Background()

openaiClient, _ := openai.NewClient(ctx, "xxx")
anthropicClient, _ := anthropic.NewClient(ctx, "xxx")
geminiClient, _ := google.NewClient(ctx, "xxx")

router := llm.NewRouter()
router.Register(openaiClient)
router.Register(anthropicClient)
router.Register(geminiClient)

gen := llm.NewGenevieve(router)
request := llm.GenerateRequest{
	Messages: []llm.Message{{Role: llm.RoleUser, Content: "When did human life first appear on Earth?"}},
}
results := gen.AskAll(ctx, request)
```

### AI Agents

```go
ctx := context.Background()

openaiClient, _ := openai.NewClient(ctx, "sk-xxx", llm.WithModel("gpt-4o"))

router := llm.NewRouter()
router.Register(openaiClient)

myAgent := agent.NewAgent(router)

// Option 1: Register with error handling
if err := myAgent.RegisterTool(tools.NewCalculator()); err != nil {
	log.Fatal(err)
}

// Option 2: Register silently (ignores invalid tools)
myAgent.TryRegisterTool(tools.NewCalculator())

answer, _ := myAgent.Run(ctx, agent.RunRequest{
	Provider: openaiClient.Name(),
	Messages: []llm.Message{{Role: llm.RoleUser, Content: "What is 4 + 5?"}},
	MaxTurns: 6,
	ThinkingEffort: llm.ThinkingMedium,
	Budget: agent.Budget{MaxTotalTokens: 8_000, WrapUpReserve: 500},
	Stream: true,
	Callbacks: agent.Callbacks{
		OnText: func(delta string) error { /* write an SSE text event */; return nil },
		OnToolCall: func(call llm.ToolCall) error { /* write an SSE tool event */; return nil },
	},
})
fmt.Println(answer.Output, answer.Usage.Total())
```

Every tool supplies its description and JSON Schema to the model. Tools marked
terminal can end a run with structured JSON. `UseTool` wraps execution for PII,
authorization, logging, or retries; `UseEvents` wraps streamed model output.

### Graphs

An agent that loops until it decides to stop is hard to reason about and hard to
bound. `pkg/graph` models a workflow as an explicit state machine instead: the
graph owns the topology, and each node only interprets the task at its own
position. There are four moving parts.

**1. State and Update.** State is what the graph carries. A node never writes to
it — it returns an `Update`, a partial change describing only the fields it owns.

```go
type State struct {
	Input string
	Steps []string
}

type Update struct {
	Step string
}
```

**2. Nodes.** A node takes state by value and returns an update. Because it can't
touch shared state, nodes in the same step can run in parallel safely.

```go
upper := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
	return Update{Step: strings.ToUpper(s.Input)}, nil
})

exclaim := graph.NodeFunc[State, Update](func(ctx context.Context, s State) (Update, error) {
	return Update{Step: s.Steps[len(s.Steps)-1] + "!"}, nil
})
```

**3. A reducer.** It merges updates into state, one field at a time. Copy the
state and the backing array of any slice field before writing — that copy is what
keeps a parallel fan-in safe, since the state you were handed may still be under
read by another node.

```go
reducer := graph.ReducerFunc[State, Update](func(s State, u Update) (State, error) {
	nextState := s
	nextState.Steps = slices.Clone(s.Steps)
	if u.Step != "" {
		nextState.Steps = append(nextState.Steps, u.Step)
	}
	return nextState, nil
})
```

**4. The topology.** `Compile` runs static analysis once — dangling edges,
unreachable nodes, dead ends — and returns an immutable `Runner` you can reuse
across goroutines.

```go
runner, err := graph.NewBuilder(reducer).
	AddNode("upper", upper).
	AddNode("exclaim", exclaim).
	AddEdge("upper", "exclaim").
	SetEntryPoint("upper").
	SetTerminal("exclaim").
	Compile()
if err != nil {
	log.Fatal(err)
}

final, _ := runner.Run(ctx, State{Input: "hello"})
fmt.Println(final.Steps) // [HELLO HELLO!]
```

Runnable: `go run ./examples/graph-simple/`

#### Writing a reducer

A reducer is plain Go — you write the merge policy per field. Two rules matter:

- **Copy before you write.** Copy the state, and the backing array of every slice
  field you touch. Fan-in reduces several updates against a state that parallel
  nodes may still hold.
- **Decide what "the node didn't set this" means, per field.** A zero-value guard
  (`if u.Draft != ""`) covers most string and slice fields. It can't express a
  counter, where a delta of `0` and "no delta" are indistinguishable — carry an
  explicit flag on the update and count off that instead:

```go
if u.Revised {
	out.Revisions = s.Revisions + 1
}
```

For graphs whose state is just `[]llm.Message`, `pkg/graph/chat` ships a
ready-made `State`/`Update`/`Reducer` trio, so there's nothing to write.

#### Agentic workflows

Beyond a straight line, a graph supports conditional routing, parallel
fan-out/fan-in, bounded loops, and configurable node failure handling. The
full graph example combines them into an intake-gated research pipeline:

```go
runner, err := graph.NewBuilder(reducer()).
	AddNode("intake", intake).
	AddNode("evidence", evidenceAgent).
	AddNode("risk", riskAgent).
	AddNode("skeptic", skepticAgent).
	AddNode("synthesize", synthesizer).
	AddNode("review", reviewer).
	AddFanEdge("intake", func(ctx context.Context, s State) ([]graph.NodeID, error) {
		if !s.Accepted {
			return []graph.NodeID{"reject"}, nil
		}
		return []graph.NodeID{"evidence", "risk", "skeptic"}, nil
	}).
	AddEdge("evidence", "synthesize").
	AddEdge("risk", "synthesize").
	AddEdge("skeptic", "synthesize").
	AddConditionalEdge("review", func(ctx context.Context, s State) (graph.NodeID, error) {
		if approved(s.Review) || s.Revisions >= maxRevisions {
			return "publish", nil
		}
		return "synthesize", nil
	}).
	Compile(
		graph.WithMaxParallel(3),
		graph.WithRecursionLimit(10),
		graph.WithNodeErrorHandler(specialistFailureAsUpdate),
	)
if err != nil {
	log.Fatal(err)
}
```

The specialists are bounded multi-turn agents using a JSON-schema evidence
tool. Their reports are reduced deterministically, the synthesizer streams its
response and finishes through a structured terminal tool, and a quality gate
can route the report through a bounded revision loop. Specialist errors become
explicit state updates; failures in critical nodes remain fatal.

Runnable: `go run ./examples/graph/` (needs `GENEVIEVE_OPENAIAPIKEY`)

The graph engine also supports `Stream` for per-step state snapshots,
checkpointing with resume, `MapNode` map-reduce, and panic containment — a
panicking node surfaces as a `NodePanicError` rather than taking down the run.

## Running the examples

```sh
go run ./examples/graph-simple/  # no API key needed
go run ./examples/multi-models/
go run ./examples/agent/
go run ./examples/graph/
```

All but `graph-simple` read API keys from the environment (or a `.env` file).
