# Genevieve

Genevieve is a modular, extensible Go library for building agentic AI systems with a provider-agnostic interface to large language models (LLMs). It simplifies the creation of autonomous AI agents that can reason, plan, and act while seamlessly integrating with providers like OpenAI, Anthropic (Claude), and Google Gemini.

With Genevieve, developers can:

- Define and compose agent behaviors in Go
- Swap or combine LLM backends without changing core logic
- Manage multi-step reasoning and tool use [Coming Soon]

## Examples

### Provider-agnostic Interface

```go
ctx := context.Background()

openaiClient := openai.NewClient("xxx")
anthropicClient := anthropic.NewClient("xxx")
geminiClient := google.NewClient(ctx, "xxx")

router := genevieve.NewRouter()
router.Register(openaiClient)
router.Register(anthropicClient)
router.Register(geminiClient)

gen := genevieve.NewGenevieve(router)
results := gen.AskAll(ctx, "When did human life first appear on Earth?")
```

### AI Agents

```go
ctx := context.Background()

openaiClient := openai.NewClient("sk-xxx", genevieve.WithModel("gpt-4o"))

router := genevieve.NewRouter()
router.Register(openaiClient)

myAgent := genevieve.NewAgent(router)

// Option 1: Register with error handling
if err := myAgent.RegisterTool(tools.NewCalculator()); err != nil {
	log.Fatal(err)
}

// Option 2: Register silently (ignores invalid tools)
myAgent.TryRegisterTool(tools.NewCalculator())

answer, _ := myAgent.Handle(ctx, openaiClient.Name(), "What is 4 + 5?")
```

