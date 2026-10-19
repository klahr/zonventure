package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	imgpng "image/png"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
)

func TestApplyGraphOp(t *testing.T) {
	t.Parallel()
	g := &graph.Graph{}
	for _, op := range []string{
		`{"op":"put_node","node":{"slug":"start","type":"start"}}`,
		`{"op":"put_node","node":{"slug":"q","type":"puzzle","title":"Q"}}`,
		`{"op":"put_node","node":{"slug":"end","type":"end"}}`,
		`{"op":"connect","from":"start","from_port":"out","to":"q"}`,
		`{"op":"connect","from":"q","from_port":"correct","to":"end"}`,
		`{"op":"put_zone","zone":{"slug":"church","lat":59.3,"lng":18.0,"radius_m":30}}`,
	} {
		var o graphOp
		if err := json.Unmarshal([]byte(op), &o); err != nil {
			t.Fatal(err)
		}
		if err := applyGraphOp(g, o, nil); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if len(g.Nodes) != 3 || len(g.Edges) != 2 || len(g.Zones) != 1 {
		t.Fatalf("got %d nodes, %d edges, %d zones", len(g.Nodes), len(g.Edges), len(g.Zones))
	}
	if g.Nodes[1].X == g.Nodes[0].X {
		t.Error("an added node should be placed beside the others")
	}

	// Replacing keeps the node's place on the canvas; reconnecting a port
	// replaces its edge instead of adding a second one.
	x := g.Nodes[1].X
	mustApply(t, g, graphOp{Op: "put_node", Node: json.RawMessage(`{"slug":"q","type":"puzzle","title":"New"}`)})
	if g.Nodes[1].Title != "New" || g.Nodes[1].X != x {
		t.Errorf("replace: %+v", g.Nodes[1])
	}
	mustApply(t, g, graphOp{Op: "connect", From: "start", FromPort: "out", To: "end"})
	if len(g.Edges) != 2 {
		t.Errorf("reconnect should replace the edge, have %d", len(g.Edges))
	}

	mustApply(t, g, graphOp{Op: "delete_node", Slug: "end"})
	if len(g.Edges) != 0 {
		t.Errorf("deleting a node should drop its edges, have %+v", g.Edges)
	}

	// A print material is put whole by id, replaced by the same id.
	mustApply(t, g, graphOp{Op: "put_print_material", Print: json.RawMessage(`{"id":"letter","name":"Letter","delivery":"posted","files":{"sv":4}}`)})
	mustApply(t, g, graphOp{Op: "put_print_material", Print: json.RawMessage(`{"id":"letter","name":"Brev","delivery":"posted","files":{"sv":5}}`)})
	if len(g.PrintMaterials) != 1 || g.PrintMaterials[0].Name != "Brev" || g.PrintMaterials[0].Files["sv"] != 5 {
		t.Errorf("print materials = %+v", g.PrintMaterials)
	}
	mustApply(t, g, graphOp{Op: "delete_print_material", Slug: "letter"})
	if len(g.PrintMaterials) != 0 {
		t.Errorf("deleted print material is still there: %+v", g.PrintMaterials)
	}

	// main: true makes the theme put the main one, which can't be deleted
	// while it is.
	mustApply(t, g, graphOp{Op: "put_theme", Theme: json.RawMessage(`{"slug":"jungle","name":"Jungle","look":{"accent":"#2e7d32"}}`), Main: true})
	mustApply(t, g, graphOp{Op: "put_theme", Theme: json.RawMessage(`{"slug":"night-out","name":"Night out","look":{}}`)})
	if g.MainTheme != "jungle" || len(g.Themes) != 2 {
		t.Errorf("main theme %q, themes %+v", g.MainTheme, g.Themes)
	}
	if err := applyGraphOp(g, graphOp{Op: "delete_theme", Slug: "jungle"}, nil); err == nil {
		t.Error("deleting the main theme: expected an error")
	}
	mustApply(t, g, graphOp{Op: "delete_theme", Slug: "night-out"})

	// set_option changes only the settings it names.
	mustApply(t, g, graphOp{Op: "set_option", Options: json.RawMessage(`{"hide_score":true,"quiz_puzzles":true,"tour":true}`)})
	mustApply(t, g, graphOp{Op: "set_option", Options: json.RawMessage(`{"quiz_puzzles":false}`)})
	if !g.HideScore || g.QuizPuzzles || g.JournalByUnlock || !g.Tour {
		t.Errorf("options: hide_score %v, quiz_puzzles %v, journal_by_unlock %v", g.HideScore, g.QuizPuzzles, g.JournalByUnlock)
	}

	for name, op := range map[string]graphOp{
		"unknown field":        {Op: "put_node", Node: json.RawMessage(`{"slug":"x","type":"story","sparkle":1}`)},
		"no slug":              {Op: "put_node", Node: json.RawMessage(`{"type":"story"}`)},
		"connect to nothing":   {Op: "connect", From: "start", FromPort: "out", To: "ghost"},
		"disconnect nothing":   {Op: "disconnect", From: "start", FromPort: "out"},
		"delete missing zone":  {Op: "delete_zone", Slug: "ghost"},
		"print without id":     {Op: "put_print_material", Print: json.RawMessage(`{"name":"Letter"}`)},
		"delete missing print": {Op: "delete_print_material", Slug: "ghost"},
		"unknown op":           {Op: "rename"},
		"no options":           {Op: "set_option", Options: json.RawMessage(`{}`)},
		"unknown option":       {Op: "set_option", Options: json.RawMessage(`{"hide_points":true}`)},
		"zone with bad fields": {Op: "put_zone", Zone: json.RawMessage(`{"slug":"z","radius":3}`)},
	} {
		if err := applyGraphOp(g, op, nil); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestHereRadius(t *testing.T) {
	t.Parallel()
	for acc, want := range map[float64]float64{3: 15, 7.5: 15, 8: 30, 15: 30, 16: 60, 200: 60} {
		if got := hereRadius(acc); got != want {
			t.Errorf("hereRadius(%v) = %v, want %v", acc, got, want)
		}
	}
}

func mustApply(t *testing.T, g *graph.Graph, op graphOp) {
	t.Helper()
	if err := applyGraphOp(g, op, nil); err != nil {
		t.Fatalf("%s: %v", op.Op, err)
	}
}

// End to end: build a graph on an adventure that has none, one edit at a
// time, and check a failing batch saves nothing.
func TestEditAdventureGraph(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('field', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})

	res, _ := editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field","ops":[
		{"op":"put_node","node":{"slug":"start","type":"start"}},
		{"op":"put_node","node":{"slug":"end","type":"end"}},
		{"op":"connect","from":"start","from_port":"out","to":"end"}
	]}`))
	if res.IsError {
		t.Fatalf("first edit: %s", textOf(res))
	}
	var out editResult
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Nodes) != 2 || out.Report.HasErrors() {
		t.Fatalf("after first edit: %+v", out)
	}

	res, _ = editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field","ops":[
		{"op":"put_zone","zone":{"slug":"church","lat":59.3,"lng":18.0,"radius_m":30}},
		{"op":"delete_node","slug":"ghost"}
	]}`))
	if !res.IsError || !strings.Contains(textOf(res), "nothing was saved") {
		t.Fatalf("a failing batch should be refused, got %s", textOf(res))
	}
	res, _ = getAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field"}`))
	if strings.Contains(textOf(res), "church") {
		t.Error("the zone from the refused batch was saved")
	}

	// A zone "here" needs a fresh fix from the author's phone.
	if res, _ = editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field","ops":[{"op":"put_zone","here":true,"zone":{"slug":"here"}}]}`)); !res.IsError {
		t.Error("here without any reported position should be refused")
	}
	if _, err := db.Exec(`INSERT INTO creator_positions(user_id, lat, lng, accuracy_m) VALUES ($1, 59.33, 18.07, 12)`, userID); err != nil {
		t.Fatal(err)
	}
	res, _ = editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field","ops":[{"op":"put_zone","here":true,"zone":{"slug":"here","name":"Fountain"}}]}`))
	if res.IsError {
		t.Fatalf("here: %s", textOf(res))
	}
	res, _ = getAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field"}`))
	if !strings.Contains(textOf(res), `"lat":59.33,"lng":18.07,"radius_m":30`) {
		t.Errorf("zone should sit at the author's position with a 30 m radius: %s", textOf(res))
	}
	if _, err := db.Exec(`UPDATE creator_positions SET at = now() - interval '10 minutes'`); err != nil {
		t.Fatal(err)
	}
	if res, _ = editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field","ops":[{"op":"put_zone","here":true,"zone":{"slug":"here"}}]}`)); !res.IsError || !strings.Contains(textOf(res), "seconds old") {
		t.Errorf("a stale position should be refused, got %s", textOf(res))
	}

	// Undo steps back one edit_adventure_graph at a time; refused edits
	// were never recorded.
	res, _ = undoAdventureGraphEdit(ctx, s, json.RawMessage(`{"slug":"field"}`))
	if res.IsError {
		t.Fatalf("undo: %s", textOf(res))
	}
	if res, _ = getAdventureGraph(ctx, s, json.RawMessage(`{"slug":"field"}`)); strings.Contains(textOf(res), "Fountain") {
		t.Error("undo should have removed the zone added here")
	}
	if res, _ = undoAdventureGraphEdit(ctx, s, json.RawMessage(`{"slug":"field"}`)); res.IsError || !strings.Contains(textOf(res), `"nodes":[]`) {
		t.Errorf("second undo should go back to the empty graph: %s", textOf(res))
	}
	if res, _ = undoAdventureGraphEdit(ctx, s, json.RawMessage(`{"slug":"field"}`)); !res.IsError {
		t.Error("nothing left to undo should be an error")
	}

	// A photo uploads as graph media the draft can reference.
	var png bytes.Buffer
	if err := imgpng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	res, _ = uploadAdventureMedia(ctx, s, json.RawMessage(`{"slug":"field","filename":"door.png","data_b64":"`+base64.StdEncoding.EncodeToString(png.Bytes())+`"}`))
	if res.IsError || !strings.Contains(textOf(res), `"kind":"image"`) {
		t.Fatalf("upload: %s", textOf(res))
	}

	// Someone outside the org cannot edit it.
	other := auth.WithUser(context.Background(), auth.User{ID: userID + 1, Username: "x", Role: auth.RoleOrgAdmin, OrgID: new(int64)})
	if res, _ = editAdventureGraph(other, s, json.RawMessage(`{"slug":"field","ops":[{"op":"delete_node","slug":"end"}]}`)); !res.IsError {
		t.Error("a user from another org should be refused")
	}
}

// The assistant's own playtest: build start → puzzle → end, then play it.
func TestSimulateAdventure(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('sim', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})

	res, _ := createGraphAdventure(ctx, s, json.RawMessage(`{"slug":"made","title":"Made","brief":{"theme":"pirates","minutes":60}}`))
	if res.IsError {
		t.Fatalf("create: %s", textOf(res))
	}
	var brief []byte
	if err := db.QueryRow(`SELECT brief FROM adventures WHERE slug = 'made'`).Scan(&brief); err != nil || !strings.Contains(string(brief), `"theme": "pirates"`) {
		t.Errorf("brief not stored: %s %v", brief, err)
	}
	if res, _ = getAdventureGraph(ctx, s, json.RawMessage(`{"slug":"made"}`)); res.IsError || strings.Contains(textOf(res), `"severity"`) {
		t.Errorf("a new adventure's draft should be valid: %s", textOf(res))
	}
	if res, _ = createGraphAdventure(ctx, s, json.RawMessage(`{"slug":"made","title":"Again"}`)); !res.IsError {
		t.Error("a taken slug should be refused")
	}

	res, _ = editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"sim","ops":[
		{"op":"put_node","node":{"slug":"start","type":"start"}},
		{"op":"put_node","node":{"slug":"q","type":"puzzle","puzzle":{
			"blocks":[{"id":"b1","kind":"text","text":"What colour is the door?"}],
			"outputs":[{"port":"right","trigger":"answer","matchers":[{"id":"m1","kind":"exact","value":"red"}]}]}}},
		{"op":"put_node","node":{"slug":"end","type":"end"}},
		{"op":"connect","from":"start","from_port":"out","to":"q"},
		{"op":"connect","from":"q","from_port":"right","to":"end"}
	]}`))
	if res.IsError {
		t.Fatalf("build: %s", textOf(res))
	}
	res, _ = simulateAdventure(ctx, s, json.RawMessage(`{"slug":"sim","actions":[
		{"kind":"start"},
		{"kind":"answer","node":"q","guess":"blue"},
		{"kind":"answer","node":"q","guess":"red"}
	]}`))
	if res.IsError {
		t.Fatalf("simulate: %s", textOf(res))
	}
	var out struct {
		Steps []simStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 3 {
		t.Fatalf("want 3 steps, got %s", textOf(res))
	}
	if out.Steps[0].At != "q" || out.Steps[1].Accepted || !out.Steps[2].Accepted || !out.Steps[2].Finished {
		t.Errorf("unexpected playthrough: %s", textOf(res))
	}
}
