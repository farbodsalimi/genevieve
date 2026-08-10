package main

// Environment loading and provider wiring live here so main.go can be read as
// the workflow's topology and nothing else.

import (
	"context"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	"github.com/farbodsalimi/genevieve/pkg/llm"
	"github.com/farbodsalimi/genevieve/pkg/providers/openai"
)

const app = "genevieve"

type Config struct {
	Debug        bool   `required:"false" default:"false"`
	OpenAIAPIKey string `required:"true"`
}

// newProvider loads configuration, constructs the OpenAI client, and registers
// it with a router. It returns the router plus the provider name to address it
// by — the two values the graph nodes need.
func newProvider(ctx context.Context) (*llm.Router, string, error) {
	godotenv.Load()

	var config Config
	if err := envconfig.Process(app, &config); err != nil {
		return nil, "", err
	}

	client, err := openai.NewClient(ctx, config.OpenAIAPIKey, llm.WithModel("gpt-4o"))
	if err != nil {
		return nil, "", err
	}

	router := llm.NewRouter()
	if err := router.Register(client); err != nil {
		return nil, "", err
	}
	return router, client.Name(), nil
}
