package llm

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
func (g *Genevieve) Ask(
	ctx context.Context,
	provider string,
	req GenerateRequest,
) (GenerateResponse, error) {
	model, ok := g.router.Get(provider)
	if !ok {
		return GenerateResponse{}, NewProviderNotFoundError(provider)
	}
	return model.Generate(ctx, req)
}

// Broadcast to all providers (parallel fan-out)
func (g *Genevieve) AskAll(ctx context.Context, req GenerateRequest) map[string]Result {
	results := make(map[string]Result)
	var wg sync.WaitGroup
	var mu sync.Mutex

	providers := g.router.GetAll()
	for name, llm := range providers {
		wg.Add(1)
		go func(name string, model LLM) {
			defer wg.Done()
			resp, err := model.Generate(ctx, req)
			mu.Lock()
			results[name] = Result{Response: resp, Err: err}
			mu.Unlock()
		}(name, llm)
	}

	wg.Wait()
	return results
}
