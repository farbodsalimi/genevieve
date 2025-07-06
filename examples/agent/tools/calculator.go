package tools

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
)

var _ genevieve.AgentTool = Calculator{}

type Calculator struct{}

func NewCalculator() *Calculator {
	return &Calculator{}
}

func (c Calculator) Name() string {
	return "calculator"
}

func (c Calculator) Execute(input genevieve.ToolExecutionInput) (string, error) {
	parts := strings.Split(input.ToolInput, "+")
	if len(parts) != 2 {
		return "", errors.New("only support 'a + b'")
	}
	a, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return fmt.Sprintf("%d", a+b), nil
}
