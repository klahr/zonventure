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
	"github.com/klahr/zonventure/internal/graph/match"
)

func TestGraphTools_AreRegisteredWithCorrectScope(t *testing.T) {
	s := &Server{tools: newRegistry()}
	s.registerGraphTools()

	// Only saving and publishing write — the same split the HTTP API makes.
	want := map[string]string{
		"get_adventure_graph":      scopeRead,
		"validate_adventure_graph": scopeRead,
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

// End to end through the real admin handler: save a graph, publish it,
// and have a broken one refused — the loop an assistant uses.
func TestGraphTools_SavePublish(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var advID int64
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('mcp-graph', 't', '', '', $1) RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}

	// A real user row: the saved version records who made it, through a
	// foreign key, so a made-up id would be rejected by the database.
	var userID int64
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}

	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})
	ctx = withScopes(ctx, []string{"read", "write"})

	good := graph.Envelope{Format: graph.FormatV2, Graph: graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "red", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Blocks:  graph.LegacyBlocks("?", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "red"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: graph.PortOut, To: "red"},
			{From: "red", FromPort: "solved", To: "end"},
		},
	}}

	env, _ := json.Marshal(good)
	res, _ := saveAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph","envelope":`+string(env)+`}`))
	if res.IsError || !strings.Contains(textOf(res), `"dirty":true`) {
		t.Fatalf("save draft: %s", textOf(res))
	}
	res, _ = publishAdventureGraph(ctx, s, json.RawMessage(`{"slug":"mcp-graph","label":"First"}`))
	if res.IsError || !strings.Contains(textOf(res), `"published":{"version":1,"label":"First"}`) {
		t.Fatalf("publish: %s", textOf(res))
	}

	// A broken draft saves, but publishing it is refused with the
	// machine-readable code.
	broken := good
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
