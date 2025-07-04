package genevieve

import (
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
func (g *Genevieve) Ask(provider string, prompt string) (string, error) {
	llm, ok := g.router.Get(provider)
	if !ok {
		return "", fmt.Errorf("provider %s not found", provider)
	}
	return llm.Complete(prompt)
}

// Broadcast to all providers (parallel fan-out)
func (g *Genevieve) AskAll(prompt string) map[string]string {
	results := make(map[string]string)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, llm := range g.router.providers {
		wg.Add(1)
		go func(name string, llm LLM) {
			defer wg.Done()
			resp, err := llm.Complete(prompt)
			mu.Lock()
			if err != nil {
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
