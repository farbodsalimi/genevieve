# Genevieve

Genevieve is a modular, extensible Go library for building agentic AI systems with a provider-agnostic interface to large language models (LLMs). It simplifies the creation of autonomous AI agents that can reason, plan, and act while seamlessly integrating with providers like OpenAI, Anthropic (Claude), and Google Gemini.

With Genevieve, developers can:

- Define and compose agent behaviors in Go
- Swap or combine LLM backends without changing core logic
- Orchestrate multi-step workflows as explicit graphs, with bounded loops and parallel fan-out

## Packages

| Package | Purpose |
| --- | --- |
| `pkg/llm` | The `LLM` interface, provider router, and the `Genevieve` API (`Ask`, `AskAll`) |
| `pkg/providers` | Concrete providers: OpenAI, Anthropic, Google |
| `pkg/agent` | Agents that select and run a tool per request |
| `pkg/graph` | A generic, domain-free orchestration engine — knows nothing about LLMs |
| `pkg/graph/nodes` | Adapters wiring the LLM packages into the graph engine |
| `pkg/graph/chat` | Batteries-included conversation state (`State`, `Update`, `Reducer`) |

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
results := gen.AskAll(ctx, "When did human life first appear on Earth?")
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

answer, _ := myAgent.Handle(ctx, openaiClient.Name(), "What is 4 + 5?")
```

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

**3. A reducer.** It merges updates into state. Rather than hand-writing merge
logic, declare one rule per field — `Append` copies the slice's backing array,
which is what keeps a parallel fan-in safe.

```go
reducer := graph.Merge(
	graph.Append(
		func(s *State) *[]string { return &s.Steps },
		func(u Update) string { return u.Step },
	),
)
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

#### Reducer combinators

`graph.Merge` composes per-field rules, so the merge policy is visible at the
call site instead of hidden in a chain of zero-value checks:

| Combinator | Merge policy |
| --- | --- |
| `Set` | Overwrite always, including with a zero value |
| `SetIf` | Overwrite when a predicate passes (`NonZero` is the usual one) |
| `Append` / `AppendIf` | Copy-on-write append of one element |
| `Concat` | Copy-on-write append of a batch |
| `Add` | Accumulate a number — the counter case a zero-value check can't express |
| `Or` | Latch a bool true once any branch sets it |
| `Apply` | Escape hatch: a raw `func(*State, Update)` for anything else |

#### Loops, branching, and LLM nodes

Beyond a straight line, a graph supports conditional routing, parallel
fan-out/fan-in, and bounded loops — cycles are legal, runaway recursion is not:

```go
// After a critique, either loop back to draft or move on to publish.
route := func(ctx context.Context, s State) (graph.NodeID, error) {
	if approved(s.lastCritique()) || s.Revisions >= maxRevisions {
		return "publish", nil
	}
	return "draft", nil
}

runner, err := graph.NewBuilder(reducer()).
	AddNode("draft", draft).
	AddNode("critique", critique).
	AddNode("publish", publish).
	AddEdge("draft", "critique").
	AddConditionalEdge("critique", route).
	SetEntryPoint("draft").
	SetTerminal("publish").
	Compile(graph.WithRecursionLimit(2*maxRevisions + 2))
```

`pkg/graph/nodes` supplies the nodes that call a model, so an LLM step is
just a node like any other:

```go
draft := nodes.LLMNode(
	router, provider,
	draftPrompt,                                     // func(State) string
	func(resp string) Update { return Update{Draft: resp} },
)
```

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
