// Package api exposes the run Manager over HTTP and serves the dashboard page.
// The dashboard is a plain API client: it polls these endpoints, so there is no
// streaming or server-side rendering here.
package api

import (
	"encoding/json"
	"errors"
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/farbodsalimi/genevieve/internal/runs"
	"github.com/farbodsalimi/genevieve/internal/storage"
)

//go:embed index.html
var indexHTML []byte

// API wires the run Manager to HTTP routes.
type API struct {
	mgr *runs.Manager
}

// New returns an API over mgr.
func New(mgr *runs.Manager) *API { return &API{mgr: mgr} }

// Routes returns the HTTP handler: dashboard page, run endpoints, and graph
// inspection.
func (a *API) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", a.handleIndex)

	r.Route("/runs", func(r chi.Router) {
		r.Post("/", a.handleStart)
		r.Get("/", a.handleList)
		r.Get("/{id}", a.handleGet)
		r.Get("/{id}/history", a.handleHistory)
		r.Post("/{id}/resume", a.handleResume)
		r.Post("/{id}/cancel", a.handleCancel)
	})

	r.Route("/graphs", func(r chi.Router) {
		r.Get("/", a.handleGraphs)
		r.Get("/{name}/topology", a.handleTopology)
	})
	return r
}

func (a *API) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

type startRequest struct {
	Name    string          `json:"name"`
	Kind    string          `json:"kind"` // optional, informational
	Initial json.RawMessage `json:"initial"`
}

func (a *API) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	id, err := a.mgr.Start(r.Context(), req.Name, req.Initial)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	f := storage.Filter{
		Kind:   r.URL.Query().Get("kind"),
		Status: storage.Status(r.URL.Query().Get("status")),
	}
	recs, err := a.mgr.List(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	rec, snap, err := a.mgr.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": rec, "state": snap.State})
}

func (a *API) handleHistory(w http.ResponseWriter, r *http.Request) {
	hist, err := a.mgr.History(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

func (a *API) handleResume(w http.ResponseWriter, r *http.Request) {
	if err := a.mgr.Resume(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) handleCancel(w http.ResponseWriter, r *http.Request) {
	if err := a.mgr.Cancel(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) handleGraphs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.mgr.Graphs())
}

func (a *API) handleTopology(w http.ResponseWriter, r *http.Request) {
	topo, ok := a.mgr.Topology(chi.URLParam(r, "name"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("no such graph"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(topo)
}

// statusFor maps domain errors to HTTP status codes.
func statusFor(err error) int {
	var nf *storage.NotFoundError
	switch {
	case errors.As(err, &nf):
		return http.StatusNotFound
	case errors.Is(err, runs.ErrNotResumable):
		return http.StatusBadRequest
	case errors.Is(err, runs.ErrAlreadyRunning):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
