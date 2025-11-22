package main

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/op/go-logging"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
	"github.com/farbodsalimi/genevieve/pkg/providers/openai"

	"github.com/farbodsalimi/genevieve/examples/agent/tools"
)

var log = logging.MustGetLogger("example")

const (
	app     = "genevieve"
	version = "v1.0.0"
)

type Config struct {
	Debug           bool   `required:"false" default:"false"`
	GeminiAPIKey    string `required:"true"`
	OpenAIAPIKey    string `required:"true"`
	AnthropicAPIKey string `required:"true"`
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic occurred:", r)
			log.Error("Stack trace:\n", string(debug.Stack()))
		}
	}()

	godotenv.Load()

	var config Config
	err := envconfig.Process(app, &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a new OpenAI client
	openaiClient := openai.NewClient(config.OpenAIAPIKey, genevieve.WithModel("gpt-4o"))

	// Register OpenAI as a router
	router := genevieve.NewRouter()
	router.Register(openaiClient)

	// Create a new agent with two tools
	myAgent := genevieve.NewAgent(router)
	myAgent.RegisterTool(tools.NewCalculator())
	myAgent.RegisterTool(tools.NewEcho())

	questions := []string{
		"What is 4 + 5?",            // this question will trigger the calculator tool
		"Hello agent, how are you?", // this question will trigger the echo tool
	}

	for _, q := range questions {
		a, err := myAgent.Handle(ctx, openaiClient.Name(), q)
		if err != nil {
			log.Errorf("Question %s errored out: %v", q, err)
			continue
		}
		log.Infof("Q: %s", q)
		log.Infof("A: %s", a)
	}
}
