package genevieve

import (
	"context"
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
		return "", NewProviderNotFoundError(provider)
	}
	return llm.Complete(ctx, prompt)
}

// Broadcast to all providers (parallel fan-out)
func (g *Genevieve) AskAll(ctx context.Context, prompt string) map[string]Result {
	results := make(map[string]Result)
	var wg sync.WaitGroup
	var mu sync.Mutex

	providers := g.router.GetAll()
	for name, llm := range providers {
		wg.Add(1)
		go func(name string, llm LLM) {
			defer wg.Done()
			resp, err := llm.Complete(ctx, prompt)
			mu.Lock()
			results[name] = Result{Response: resp, Err: err}
			mu.Unlock()
		}(name, llm)
	}

	wg.Wait()
	return results
}
