package tools

import (
	"context"
	"encoding/json"

	"github.com/farbodsalimi/genevieve/pkg/agent"
)

var _ agent.AgentTool = Echo{}

type Echo struct{}

func NewEcho() *Echo {
	return &Echo{}
}

func (e Echo) Name() string {
	return "echo"
}

func (e Echo) Description() string { return "Echo text back to the user." }
func (e Echo) Schema() json.RawMessage {
	return json.RawMessage(
		`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`,
	)
}
func (e Echo) Terminal() bool { return false }
func (e Echo) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"text": "Echo: " + args.Text})
}
