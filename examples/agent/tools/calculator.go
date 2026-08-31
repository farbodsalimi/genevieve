package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/farbodsalimi/genevieve/pkg/agent"
)

var _ agent.AgentTool = Calculator{}

type Calculator struct{}

func NewCalculator() *Calculator {
	return &Calculator{}
}

func (c Calculator) Name() string {
	return "calculator"
}

func (c Calculator) Description() string { return "Add two integers." }
func (c Calculator) Schema() json.RawMessage {
	return json.RawMessage(
		`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"]}`,
	)
}
func (c Calculator) Terminal() bool { return false }
func (c Calculator) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if len(input) == 0 {
		return nil, errors.New("missing calculator input")
	}
	return json.Marshal(map[string]int{"result": args.A + args.B})
}
