package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/farbodsalimi/genevieve/internal/runs"
	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/farbodsalimi/genevieve/pkg/graph"
)

type aState struct {
	Log []string `json:"log"`
}
type aUpdate struct{ Entry string }

func aReducer() graph.Reducer[aState, aUpdate] {
	return graph.ReducerFunc[aState, aUpdate](func(s aState, u aUpdate) (aState, error) {
		log := append(append([]string{}, s.Log...), u.Entry)
		return aState{Log: log}, nil
	})
}

func aNode(name string) graph.Node[aState, aUpdate] {
	return graph.NodeFunc[aState, aUpdate](func(context.Context, aState) (aUpdate, error) {
		return aUpdate{Entry: name}, nil
	})
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	m := runs.NewManager(storage.NewMemoryStore())
	build := func(cp graph.Checkpointer[aState]) (*graph.Runner[aState, aUpdate], error) {
		return graph.NewBuilder(aReducer()).
			AddNode("a", aNode("a")).AddNode("b", aNode("b")).
			AddEdge("a", "b").SetEntryPoint("a").SetTerminal("b").
			Compile(graph.WithCheckpointer(cp), graph.WithCheckpointInterval(1))
	}
	if err := runs.RegisterGraph(m, "demo", build, func(b []byte) (aState, error) {
		var s aState
		if len(b) == 0 {
			return s, nil
		}
		return s, json.Unmarshal(b, &s)
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv := httptest.NewServer(New(m).Routes())
	t.Cleanup(srv.Close)
	return srv
}

func TestAPI_GraphsAndTopology(t *testing.T) {
	srv := newTestServer(t)
	var names []string
	getJSON(t, srv.URL+"/graphs", &names)
	if len(names) != 1 || names[0] != "demo" {
		t.Fatalf("graphs = %v, want [demo]", names)
	}
	var topo graph.Topology
	getJSON(t, srv.URL+"/graphs/demo/topology", &topo)
	if topo.Entry != "a" {
		t.Fatalf("topology entry = %q", topo.Entry)
	}
}

func TestAPI_RunLifecycle(t *testing.T) {
	srv := newTestServer(t)

	// Start.
	resp, err := http.Post(srv.URL+"/runs", "application/json",
		strings.NewReader(`{"kind":"graph","name":"demo","initial":{}}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	var started struct{ ID string }
	_ = json.NewDecoder(resp.Body).Decode(&started)
	resp.Body.Close()
	if started.ID == "" {
		t.Fatal("no run id returned")
	}

	// Poll GET until it succeeds.
	deadline := time.Now().Add(2 * time.Second)
	var rec struct {
		Record storage.RunRecord `json:"record"`
	}
	for time.Now().Before(deadline) {
		getJSON(t, srv.URL+"/runs/"+started.ID, &rec)
		if rec.Record.Status == storage.StatusSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.Record.Status != storage.StatusSucceeded {
		t.Fatalf("run status = %q, want succeeded", rec.Record.Status)
	}

	// List includes it.
	var list []storage.RunRecord
	getJSON(t, srv.URL+"/runs", &list)
	if len(list) != 1 || list[0].ID != started.ID {
		t.Fatalf("list = %+v", list)
	}

	// History has snapshots.
	var hist []storage.Snapshot
	getJSON(t, srv.URL+"/runs/"+started.ID+"/history", &hist)
	if len(hist) != 2 {
		t.Fatalf("history = %d, want 2", len(hist))
	}

	// Unknown run -> 404.
	r, _ := http.Get(srv.URL + "/runs/missing")
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("missing run status = %d, want 404", r.StatusCode)
	}
	r.Body.Close()
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
