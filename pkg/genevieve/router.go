package genevieve

type Router struct {
	providers map[string]LLM
}

func NewRouter() *Router {
	return &Router{providers: make(map[string]LLM)}
}

func (r *Router) Register(llm LLM) {
	r.providers[llm.Name()] = llm
}

func (r *Router) Get(name string) (LLM, bool) {
	llm, ok := r.providers[name]
	return llm, ok
}

func (r *Router) ListProviders() []string {
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
