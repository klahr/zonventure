package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/klahr/zonventure/internal/auth"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

type graphDTO struct {
	Version  int            `json:"version"`
	Envelope graph.Envelope `json:"envelope"`
	Report   valid.Report   `json:"report"`
}

type refusalDTO struct {
	Error  string        `json:"error"`
	Code   string        `json:"code"`
	Report *valid.Report `json:"report"`
}

// doAny is client.do, but it decodes the body whatever the status. The
// shared helper skips error bodies on purpose; these tests need them,
// because a refusal's code and report are the contract being tested.
func (c *client) doAny(method, path string, body any, into any) int {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		if tok := c.csrfCookie(); tok != "" {
			req.Header.Set(auth.CSRFHeaderName, tok)
		}
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil && err != io.EOF {
			c.t.Fatalf("decode %s %s (%d): %v", method, path, resp.StatusCode, err)
		}
	}
	return resp.StatusCode
}

// The whole authoring loop over HTTP: an adventure with no graph, a
// conversion from its lanes, a save, a reload, a refused broken save.
func TestGraphAuthoringLoop(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	root := newClient(t, ts)
	if s := root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}

	// No graph yet.
	var refused refusalDTO
	if s := root.doAny("GET", "/api/admin/adventures/demo/graph", nil, &refused); s != 404 || refused.Code != "graph_not_found" {
		t.Fatalf("GET before any save = %d %+v; want 404 graph_not_found", s, refused)
	}

	// Convert the seeded three-page linear adventure. Not saved.
	var converted graphDTO
	if s := root.do("POST", "/api/admin/adventures/demo/graph/convert", nil, &converted); s != 200 {
		t.Fatalf("convert = %d", s)
	}
	if !converted.Report.OK() {
		t.Fatalf("the converted graph should validate: %v", converted.Report.Errors())
	}
	g := converted.Envelope.Graph
	// start + three pages + end.
	if len(g.Nodes) != 5 {
		t.Errorf("converted graph has %d nodes, want 5", len(g.Nodes))
	}
	// The seed's second page is gated at 50 m; that must become a zone.
	if len(g.Zones) != 1 || g.Zones[0].RadiusM != 50 {
		t.Errorf("zones = %+v; want the landmark's 50 m gate", g.Zones)
	}
	if s := root.do("GET", "/api/admin/adventures/demo/graph", nil, nil); s != 404 {
		t.Errorf("convert must not save; GET after convert = %d, want 404", s)
	}

	// Save it.
	var saved graphDTO
	if s := root.do("PUT", "/api/admin/adventures/demo/graph", converted.Envelope, &saved); s != 200 || saved.Version != 1 {
		t.Fatalf("save = %d, version %d", s, saved.Version)
	}

	// Reload it.
	var loaded graphDTO
	if s := root.do("GET", "/api/admin/adventures/demo/graph", nil, &loaded); s != 200 {
		t.Fatalf("reload = %d", s)
	}
	if loaded.Version != 1 || len(loaded.Envelope.Graph.Nodes) != 5 || loaded.Envelope.Format != graph.FormatV2 {
		t.Errorf("reloaded %+v", loaded)
	}

	// Validate without saving: a broken graph reports its problems.
	broken := converted.Envelope
	broken.Graph.Edges = nil
	var checked graphDTO
	if s := root.do("POST", "/api/admin/adventures/demo/graph/validate", broken, &checked); s != 200 {
		t.Fatalf("validate = %d", s)
	}
	if checked.Report.OK() {
		t.Error("a graph with no edges should not validate")
	}

	// And saving it is refused with the full report; no version created.
	refused = refusalDTO{}
	if s := root.doAny("PUT", "/api/admin/adventures/demo/graph", broken, &refused); s != 422 || refused.Code != "graph_invalid" {
		t.Fatalf("saving a broken graph = %d %+v; want 422 graph_invalid", s, refused)
	}
	if refused.Report == nil || len(refused.Report.Errors()) == 0 {
		t.Error("the refusal should carry every error, so the editor can mark them all")
	}
	loaded = graphDTO{}
	root.do("GET", "/api/admin/adventures/demo/graph", nil, &loaded)
	if loaded.Version != 1 {
		t.Errorf("a refused save must not create a version; latest is %d", loaded.Version)
	}

	// A second good save is version 2.
	if s := root.do("PUT", "/api/admin/adventures/demo/graph", converted.Envelope, &saved); s != 200 || saved.Version != 2 {
		t.Errorf("second save = %d, version %d; want 200, 2", s, saved.Version)
	}
}

func TestGraphRejectsMalformedEnvelopes(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()
	root := newClient(t, ts)
	root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil)

	for name, body := range map[string]any{
		"v1 format":     map[string]any{"format": "zonventure-adventure-v1", "graph": map[string]any{"nodes": []any{}, "edges": []any{}}},
		"unknown field": map[string]any{"format": graph.FormatV2, "graph": map[string]any{"nodes": []any{}, "edges": []any{}}, "extra": 1},
		"not an object": []int{1, 2, 3},
	} {
		var refused refusalDTO
		if s := root.doAny("PUT", "/api/admin/adventures/demo/graph", body, &refused); s != 400 || refused.Code != "graph_malformed" {
			t.Errorf("%s: got %d %+v; want 400 graph_malformed", name, s, refused)
		}
	}
}

// The graph carries every answer, so only someone who can edit the
// adventure may see it. The seeded demo is in the global catalog, which
// an org admin cannot edit.
func TestGraphRequiresEditRights(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/adventures/demo/graph"},
		{"POST", "/api/admin/adventures/demo/graph/convert"},
		{"POST", "/api/admin/adventures/demo/graph/validate"},
		{"PUT", "/api/admin/adventures/demo/graph"},
	} {
		if s := boss.do(c.method, c.path, map[string]any{}, nil); s != 403 {
			t.Errorf("%s %s as an org admin on a global adventure = %d, want 403", c.method, c.path, s)
		}
	}

	anon := newClient(t, ts)
	if s := anon.do("GET", "/api/admin/adventures/demo/graph", nil, nil); s != 401 && s != 403 {
		t.Errorf("anonymous GET = %d, want 401 or 403", s)
	}

	root := newClient(t, ts)
	root.do("POST", "/api/auth/login", map[string]string{"username": "root", "password": "rootpw"}, nil)
	if s := root.do("GET", "/api/admin/adventures/no-such-adventure/graph", nil, nil); s != 404 {
		t.Errorf("unknown adventure = %d, want 404", s)
	}
}
