package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/farbodsalimi/genevieve/internal/api"
	"github.com/farbodsalimi/genevieve/internal/runs"
)

func main() {
	addr := ":3000"

	store := pickStore()
	mgr := runs.NewManager(store)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Mount("/", api.New(mgr).Routes())

	log.Printf("genevieve runs API + dashboard on http://localhost%s/", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}
