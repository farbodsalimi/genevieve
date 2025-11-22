package tools

import (
	"context"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.AgentTool = Echo{}

type Echo struct{}

func NewEcho() *Echo {
	return &Echo{}
}

func (e Echo) Name() string {
	return "echo"
}

func (e Echo) Execute(ctx context.Context, input genevieve.AgentToolInput) (string, error) {
	return "Echo: " + input.ToolInput, nil
}
