package main

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	"github.com/farbodsalimi/genevieve/pkg/genevieve"
	"github.com/farbodsalimi/genevieve/pkg/providers/anthropic"
	"github.com/farbodsalimi/genevieve/pkg/providers/google"
	"github.com/farbodsalimi/genevieve/pkg/providers/openai"
)

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
			log.Println("Panic occurred:", r)
			log.Println("Stack trace:\n", string(debug.Stack()))
		}
	}()

	godotenv.Load()

	var config Config
	err := envconfig.Process(app, &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	ctx := context.Background()

	openaiClient := openai.NewClient(ctx, config.OpenAIAPIKey)
	anthropicClient := anthropic.NewClient(ctx, config.AnthropicAPIKey)
	geminiClient := google.NewClient(ctx, config.GeminiAPIKey)

	router := genevieve.NewRouter()
	router.Register(openaiClient)
	router.Register(anthropicClient)
	router.Register(geminiClient)

	assistant := genevieve.NewGenevieve(router)

	// Prompt
	prompt := fmt.Sprintf(
		"Recommend me 3 books on climate change. Deliver the response in plain text without any Markdown or formatting. Provide the output as raw text. This is the schema for your output: %s",
		schema,
	)

	// Ask a specific provider
	resp, err := assistant.Ask(openaiClient.Name(), prompt)
	fmt.Println(resp, err)

	// Ask all providers
	results := assistant.AskAll(prompt)
	for provider, result := range results {
		fmt.Printf("[%s]: %s\n", provider, result)
	}
}

const schema = `{
  "$schema": "http://json-schema.org/draft-04/schema#",
  "type": "object",
  "properties": {
    "books": {
      "type": "array",
      "items": [
        {
          "type": "object",
          "properties": {
            "title": {
              "type": "string"
            },
            "author": {
              "type": "string"
            }
          },
          "required": [
            "title",
            "author"
          ]
        }
      ]
    }
  },
  "required": [
    "books"
  ]
}`
