package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type evidenceLookupTool struct{}

func (evidenceLookupTool) Name() string { return "evidence_lookup" }
func (evidenceLookupTool) Description() string {
	return "Search the example's small evidence corpus for facts relevant to a research angle."
}
func (evidenceLookupTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {"query": {"type": "string", "description": "The focused evidence query"}},
  "required": ["query"],
  "additionalProperties": false
}`)
}
func (evidenceLookupTool) Terminal() bool { return false }
func (evidenceLookupTool) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	return json.Marshal(map[string]any{
		"query": args.Query,
		"evidence": []map[string]string{
			{"id": "E1", "text": "Bounded workflows cap turns and make maximum execution cost estimable."},
			{"id": "E2", "text": "Parallel independent reviews reduce latency but consume provider quota concurrently."},
			{"id": "E3", "text": "Typed state and deterministic reducers make intermediate decisions inspectable and testable."},
			{"id": "E4", "text": "Graceful partial failure preserves successful reports but requires explicit disclosure of missing evidence."},
		},
	})
}

type reportObservationTool struct{}

func (reportObservationTool) Name() string { return "report_observation" }
func (reportObservationTool) Description() string {
	return "Submit the final structured research report and end the synthesis run."
}
func (reportObservationTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {"type": "string"},
    "summary": {"type": "string"},
    "findings": {"type": "array", "items": {"type": "string"}},
    "risks": {"type": "array", "items": {"type": "string"}},
    "limitations": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["title", "summary", "findings", "risks", "limitations"],
  "additionalProperties": false
}`)
}
func (reportObservationTool) Terminal() bool { return true }
func (reportObservationTool) Execute(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	report, err := decodeObservation(input)
	if err != nil {
		return nil, err
	}
	if report.Title == "" || report.Summary == "" {
		return nil, fmt.Errorf("title and summary are required")
	}
	return json.Marshal(report)
}
