package tools

import (
	"context"

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

func (e Echo) Execute(ctx context.Context, input agent.AgentToolInput) (string, error) {
	return "Echo: " + input.ToolInput, nil
}
