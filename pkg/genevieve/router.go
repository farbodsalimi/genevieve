package genevieve

// TODO: Add thread safety - Router is not safe for concurrent registration/access
type Router struct {
	providers map[string]LLM
}

func NewRouter() *Router {
	return &Router{providers: make(map[string]LLM)}
}

// TODO: Add validation to prevent duplicate provider registrations
// TODO: Add validation for nil LLM or empty provider names
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
