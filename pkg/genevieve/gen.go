package genevieve

import (
	"context"
	"fmt"
	"sync"
)

type Genevieve struct {
	router *Router
}

func NewGenevieve(router *Router) *Genevieve {
	return &Genevieve{router: router}
}

// Query a specific provider
// TODO: Add metrics/observability for provider performance
func (g *Genevieve) Ask(ctx context.Context, provider string, prompt string) (string, error) {
	llm, ok := g.router.Get(provider)
	if !ok {
		// TODO: Use structured error types instead of fmt.Errorf
		return "", fmt.Errorf("provider %s not found", provider)
	}
	return llm.Complete(ctx, prompt)
}

// Broadcast to all providers (parallel fan-out)
// TODO: Return structured responses with metadata instead of plain strings
// TODO: Add retry logic for failed requests
// TODO: Add rate limiting for provider calls
// TODO: Add structured logging for observability
func (g *Genevieve) AskAll(ctx context.Context, prompt string) map[string]string {
	results := make(map[string]string)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, llm := range g.router.providers {
		wg.Add(1)
		go func(name string, llm LLM) {
			defer wg.Done()
			resp, err := llm.Complete(ctx, prompt)
			mu.Lock()
			if err != nil {
				// TODO: Use structured error types instead of plain strings
				results[name] = "error: " + err.Error()
			} else {
				results[name] = resp
			}
			mu.Unlock()
		}(name, llm)
	}

	wg.Wait()
	return results
}
