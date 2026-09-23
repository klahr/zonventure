package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
)

func TestGraphTools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerGraphTools()

	// Only saving writes. Convert builds a graph but does not store it,
	// so it is a read — the same split the HTTP API makes.
	want := map[string]string{
		"get_adventure_graph":      scopeRead,
		"validate_adventure_graph": scopeRead,
		"convert_adventure_graph":  scopeRead,
		"save_adventure_graph":     scopeWrite,
		"publish_adventure_graph":  scopeWrite,
	}
	for name, w := range want {
		if got, ok := s.tools.scopes[name]; !ok || got != w {
			t.Errorf("tool %q scope = %q (registered=%v), want %q", name, got, ok, w)
		}
	}
	if len(s.tools.scopes) != len(want) {
		t.Errorf("registry has %d tools, want %d", len(s.tools.scopes), len(want))
	}
}

func TestGraphTools_ArgValidation(t *testing.T) {
	s := &Server{tools: newRegistry()}
	ctx := context.Background()
	for name, call := range map[string]func() (ToolCallResult, error){
		"missing slug": func() (ToolCallResult, error) {
			return getAdventureGraph(ctx, s, json.RawMessage(`{}`))
		},
		"bad json": func() (ToolCallResult, error) {
			return getAdventureGraph(ctx, s, json.RawMessage(`{`))
		},
		"save without envelope": func() (ToolCallResult, error) {
			return saveAdventureGraph(ctx, s, json.RawMessage(`{"slug":"x"}`))
		},
		"validate without envelope": func() (ToolCallResult, error) {
			return validateAdventureGraph(ctx, s, json.RawMessage(`{"slug":"x"}`))
		},
		"nil admin": func() (ToolCallResult, error) {
			return convertAdventureGraph(ctx, s, json.RawMessage(`{"slug":"x"}`))
		},
	} {
		res, err := call()
		if err != nil {
			t.Errorf("%s: protocol error %v", name, err)
			continue
		}
		if !res.IsError {
			t.Errorf("%s: expected a tool error, got %q", name, textOf(res))
		}
	}
}

// End to end through the real admin handler: convert an adventure's
// lanes, save the result, and read it back — the loop an assistant uses.
func TestGraphTools_ConvertSaveGet(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author) VALUES ('mcp-graph', 't', '', '') RETURNING id`).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	var laneID int64
	if err := db.QueryRow(`INSERT INTO adventure_lanes(adventure_id, idx, mode) VALUES ($1, 0, 'linear') RETURNING id`, advID).Scan(&laneID); err != nil {
		t.Fatal(err)
	}
	for i, ans := range []string{"red", "blue"} {
		if _, err := db.Exec(`
			INSERT INTO adventure_pages(adventure_id, lane_id, idx, title, prompt, accept, lat, lng)
			VALUES ($1, $2, $3, $4, '?', $5, 0, 0)
		`, advID, laneID, i, "P"+ans, `[{"text":"`+ans+`"}]`); err != nil {
			t.Fatal(err)
		}
	}

	// A real user row: the saved version records who made it, through a
	// foreign key, so a made-up id would be rejected by the database.
	var userID int64
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified, is_admin) VALUES ('root', 'x', 1, true) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "root", IsAdmin: true, Role: "admin"})
	ctx = withScopes(ctx, []string{"read", "write"})

	res, _ := convertAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph"}`))
	if res.IsError {
		t.Fatalf("convert: %s", textOf(res))
	}
	var converted struct {
		Envelope graph.Envelope `json:"envelope"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &converted); err != nil {
		t.Fatalf("decoding convert result: %v\n%s", err, textOf(res))
	}
	if n := len(converted.Envelope.Graph.Nodes); n != 4 {
		t.Fatalf("converted graph has %d nodes, want 4 (start, two pages, end)", n)
	}

	env, _ := json.Marshal(converted.Envelope)
	res, _ = saveAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph","envelope":`+string(env)+`}`))
	if res.IsError || !strings.Contains(textOf(res), `"dirty":true`) {
		t.Fatalf("save draft: %s", textOf(res))
	}
	res, _ = publishAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph","label":"First"}`))
	if res.IsError || !strings.Contains(textOf(res), `"published":{"version":1,"label":"First"}`) {
		t.Fatalf("publish: %s", textOf(res))
	}

	// A broken draft saves, but publishing it is refused with the
	// machine-readable code.
	broken := converted.Envelope
	broken.Graph.Edges = nil
	env, _ = json.Marshal(broken)
	if res, _ = saveAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph","envelope":`+string(env)+`}`)); res.IsError {
		t.Fatalf("a broken draft should still save: %s", textOf(res))
	}
	res, _ = publishAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph"}`))
	if !res.IsError || !strings.Contains(textOf(res), "graph_invalid") {
		t.Errorf("publishing a broken draft should be refused with graph_invalid, got %q", textOf(res))
	}
}
