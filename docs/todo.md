# Agent Runtime TODO Audit

This report compares the requested agent-runtime features with the current
Genevieve implementation.

## Summary

The core provider-neutral agent runtime is implemented, including structured
tool calls, multi-turn execution, schemas, usage accounting, streaming,
terminal tools, budgets, and middleware. Some application-specific integration
work remains.

## Status

| Item | Status | Notes |
| --- | --- | --- |
| Anthropic multi-turn tool loop | Partial | `Agent.Run` implements the complete model → tool → result → model loop for every provider. Anthropic does not use the SDK-specific `BetaToolRunner` or `NextMessage`. |
| Per-tool JSON Schema | Complete | Every `AgentTool` supplies a schema. Registration validates it and all providers pass it to the model. Model-generated arguments are not independently validated against the schema before execution. |
| Streaming callbacks and SSE | Partial | `OnText`, `OnToolCall`, and `OnUsage` exist and providers perform real streaming. A concrete HTTP/SSE adapter is not included. |
| Token accounting | Complete | Providers populate input/output usage and `Agent.Run` accumulates it across turns and wrap-up generation. |
| Extended thinking and effort | Partial | The neutral effort API exists. Anthropic enables adaptive thinking, but does not distinguish low, medium, and high. `LLMNode` cannot currently set thinking effort for a graph synthesizer. |
| Terminal `report_observation` tool | Partial | Terminal tools support validated structured JSON output. `report_observation` currently exists only in tests; there is no reusable implementation or exported schema. |
| Budget enforcement and wrap-up | Complete | Input, output, total, turn, and wrap-up-reserve limits are supported, along with a configurable final wrap-up prompt. Enforcement occurs between provider turns. |
| Tool wrapping and PII egress | Partial | Tool and streaming-event middleware provide wrapping seams. Event middleware can protect streamed/SSE text, but final `Result.Output`, messages, terminal output, and non-streaming text are not transformed through the same seam. |

## Remaining TODOs

- Add a reusable SSE adapter or HTTP example that serializes and flushes agent
  callback events.
- Allow graph `LLMNode` callers to configure a complete `GenerateRequest`,
  including thinking effort and token limits.
- Add a concrete terminal `report_observation` tool and its JSON Schema.
- Add a unified output middleware that processes streamed deltas and final
  result fields consistently, including non-streaming generation.
- Decide whether Anthropic must specifically use `BetaToolRunner`/`NextMessage`.
  The current provider-neutral loop is functionally equivalent and keeps agent
  policy outside the provider adapter.
- Optionally validate each model-generated tool input against its registered
  JSON Schema before invoking the tool.
- Dynamically cap each generation request using the remaining token budget to
  reduce single-turn budget overshoot.

## Verification

The current implementation passes:

```text
go vet ./...
go test -race ./pkg/agent ./pkg/graph/...
go test ./...
go build ./...
```

Live provider integration remains a manual check because it requires API keys.
