package genevieve

import (
	"maps"
	"sync"
)

type Router struct {
	mu        sync.RWMutex
	providers map[string]LLM
}

func NewRouter() *Router {
	return &Router{providers: make(map[string]LLM)}
}

func (r *Router) Register(llm LLM) error {
	if llm == nil {
		return NewNilProviderError()
	}

	name := llm.Name()
	if name == "" {
		return NewEmptyProviderNameError()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		return NewDuplicateProviderError(name)
	}

	r.providers[name] = llm
	return nil
}

func (r *Router) Get(name string) (LLM, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	llm, ok := r.providers[name]
	return llm, ok
}

func (r *Router) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetAll returns a snapshot of all registered providers
// This is safe for concurrent use and creates a copy of the provider map
func (r *Router) GetAll() map[string]LLM {
	r.mu.RLock()
	defer r.mu.RUnlock()
	providers := make(map[string]LLM, len(r.providers))
	maps.Copy(providers, r.providers)
	return providers
}
