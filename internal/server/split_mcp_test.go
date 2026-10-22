package server_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/server"
)

// citadelOps builds a split with the assistant's graph tools: the tower
// party rings the bells, the crypt party counts the tombs and the slits,
// and they meet at the gate.
func citadelOps() []any {
	puzzle := func(slug, answer string) map[string]any {
		return map[string]any{"op": "put_node", "node": map[string]any{"slug": slug, "type": "puzzle", "title": slug, "puzzle": map[string]any{
			"blocks":  []any{map[string]any{"id": "b1", "kind": "text", "text": slug + "?"}},
			"outputs": []any{map[string]any{"port": "ok", "trigger": "answer", "points": 1, "matchers": []any{map[string]any{"id": "m1", "kind": "exact", "value": answer}}}},
		}}}
	}
	node := func(n map[string]any) map[string]any { return map[string]any{"op": "put_node", "node": n} }
	wire := func(from, port, to string) map[string]any {
		return map[string]any{"op": "connect", "from": from, "from_port": port, "to": to}
	}
	return []any{
		node(map[string]any{"slug": "start", "type": "start"}),
		node(map[string]any{"slug": "split", "type": "split", "title": "Storm the citadel", "split": map[string]any{"text": "Some up the tower, the rest down to the crypt.",
			"paths": []any{map[string]any{"port": "tower", "label": "Climb the tower"}, map[string]any{"port": "crypt", "label": "Crypt"}}}}),
		puzzle("bells", "3"), puzzle("tombs", "7"), puzzle("slits", "12"),
		node(map[string]any{"slug": "gate", "type": "join", "title": "The gate"}),
		node(map[string]any{"slug": "end", "type": "end", "end": map[string]any{"outcome": "complete"}}),
		wire("start", "out", "split"), wire("split", "tower", "bells"), wire("split", "crypt", "tombs"),
		wire("bells", "ok", "gate"), wire("tombs", "ok", "slits"), wire("slits", "ok", "gate"), wire("gate", "out", "end"),
	}
}

// The assistant builds, reads, edits and plays a split with its own
// tools, and the dev phone it can move and read shows the split.
func TestMCPAuthorsASplit(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "1")
	ts, _, cleanup := newTestServerWith(t, server.Config{PublicURL: "http://zv.test"})
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	call := mcpTool(t, ts, pub)
	must := func(name string, args any) string {
		t.Helper()
		text, isErr := call(name, args)
		if isErr {
			t.Fatalf("%s: %s", name, text)
		}
		return text
	}

	out := must("edit_adventure_graph", map[string]any{"slug": "demo", "ops": citadelOps()})
	if strings.Contains(out, `"severity":"error"`) {
		t.Fatalf("the split should validate:\n%s", out)
	}
	if out := must("get_adventure_graph", map[string]any{"slug": "demo"}); strings.Contains(out, `"severity":"error"`) || !strings.Contains(out, `"type":"split"`) {
		t.Fatalf("the stored draft: %s", out)
	}

	// Reading a split back: its paths and where each leads.
	step := must("get_step", map[string]any{"slug": "demo", "node": "split"})
	for _, want := range []string{`"type":"split"`, `"port":"tower"`, `"tower":"bells"`, `"crypt":"tombs"`} {
		if !strings.Contains(step, want) {
			t.Errorf("get_step of the split lacks %s:\n%s", want, step)
		}
	}
	// Changing words never renames a path: "tower" is in the label and is
	// the port, and only the label changes.
	must("edit_adventure_graph", map[string]any{"slug": "demo", "ops": []any{
		map[string]any{"op": "set_text", "slug": "split", "find": "tower", "replace": "spire", "all": true},
	}})
	step = must("get_step", map[string]any{"slug": "demo", "node": "split"})
	if !strings.Contains(step, `"label":"Climb the spire"`) || !strings.Contains(step, `"port":"tower"`) || strings.Contains(step, `"port":"spire"`) {
		t.Fatalf("set_text on a split: the label changes and the path stays wired:\n%s", step)
	}
	// patch_node finds a path by its port, as it does an output.
	must("edit_adventure_graph", map[string]any{"slug": "demo", "ops": []any{
		map[string]any{"op": "patch_node", "slug": "split", "patch": map[string]any{"split": map[string]any{"paths": []any{map[string]any{"port": "crypt", "label": "Down to the crypt"}}}}},
	}})
	step = must("get_step", map[string]any{"slug": "demo", "node": "split"})
	if !strings.Contains(step, `"label":"Down to the crypt"`) || !strings.Contains(step, `"label":"Climb the spire"`) {
		t.Fatalf("patch_node of one path should keep the other:\n%s", step)
	}

	// Simulated, the team plays every group in turn, each step saying
	// which group plays next and where all of them are.
	sim := must("simulate_adventure", map[string]any{"slug": "demo", "actions": []any{
		map[string]any{"kind": "start"},
		map[string]any{"kind": "continue", "node": "split"},
		map[string]any{"kind": "answer", "node": "bells", "guess": "3"},
		map[string]any{"kind": "continue", "node": "split"},
		map[string]any{"kind": "answer", "node": "tombs", "guess": "7"},
		map[string]any{"kind": "answer", "node": "slits", "guess": "12"},
	}})
	var res struct {
		Steps []struct {
			At       string `json:"at"`
			Finished bool   `json:"finished"`
			Group    string `json:"group"`
			Groups   []struct {
				Group   string `json:"group"`
				At      string `json:"at"`
				Waiting bool   `json:"waiting"`
			} `json:"groups"`
			Refusal json.RawMessage `json:"refusal"`
		} `json:"steps"`
		NotRun int `json:"not_run"`
	}
	if err := json.Unmarshal([]byte(sim), &res); err != nil {
		t.Fatal(err)
	}
	if res.NotRun != 0 || len(res.Steps) != 6 {
		t.Fatalf("simulation stopped early:\n%s", sim)
	}
	if s := res.Steps[1]; s.At != "bells" || s.Group != "Climb the spire" || len(s.Groups) != 2 {
		t.Errorf("after the spire set off: %+v", s)
	}
	if s := res.Steps[2]; s.At != "split" || s.Group != "Down to the crypt" || !s.Groups[0].Waiting || s.Groups[0].At != "gate" {
		t.Errorf("after the spire answered: %+v", s)
	}
	if s := res.Steps[5]; !s.Finished || s.Groups != nil {
		t.Errorf("the end: %+v", s)
	}
	must("publish_adventure_graph", map[string]any{"slug": "demo"})

	// The dev phone, moved onto the split, splits there and says so.
	must("create_dev_ticket", map[string]any{"slug": "demo"})
	var tk struct {
		TeamURL string `json:"team_url"`
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/dev-ticket", nil, &tk); s != 200 {
		t.Fatalf("dev ticket = %d", s)
	}
	_, page := getRaw(t, newClient(t, ts), strings.TrimPrefix(tk.TeamURL, "http://zv.test"))
	m := regexp.MustCompile(`/play\?t=([A-Za-z0-9_-]+)`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no magic link on the dev team's page")
	}
	phone := newClient(t, ts)
	if s := phone.do("GET", "/play?t="+m[1], nil, nil); s != 200 {
		t.Fatalf("magic link = %d", s)
	}
	must("dev_go_to", map[string]any{"slug": "demo", "node": "split"})
	screen := must("get_dev_screen", map[string]any{"slug": "demo"})
	for _, want := range []string{`step "split"`, "you are with Climb the spire", "- Down to the crypt: at Storm the citadel", "continue to set off"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the dev screen at the split lacks %q:\n%s", want, screen)
		}
	}
	if s := phone.doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, nil); s != 200 {
		t.Fatalf("the dev phone sets off = %d", s)
	}
	screen = must("get_dev_screen", map[string]any{"slug": "demo"})
	if !strings.Contains(screen, `step "bells"`) || !strings.Contains(screen, "Climb the spire") {
		t.Errorf("the dev screen after setting off:\n%s", screen)
	}
}

// The organiser's assistant sees a split team's groups, and moves one
// stuck group on by naming its step — never the split itself.
func TestMCPRescuesAGroup(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "1")
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := pub.doAny("PUT", "/api/admin/adventures/demo/graph", graph.Envelope{Format: graph.FormatV2, Graph: splitPlayGraph()}, nil); s != 200 {
		t.Fatalf("save graph = %d", s)
	}
	if s := pub.doAny("POST", "/api/admin/adventures/demo/graph/publish", nil, nil); s != 200 {
		t.Fatalf("publish = %d", s)
	}
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	var ev struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &ev); s != 201 {
		t.Fatalf("create event = %d", s)
	}
	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			Token string `json:"token"`
		} `json:"members"`
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/teams", map[string]any{"name": "Crew", "member_names": []string{"Ann", "Bo"}}, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	var phones []*client
	for _, m := range team.Members {
		c := newClient(t, ts)
		if s := c.do("GET", "/play?t="+m.Token, nil, nil); s != 200 {
			t.Fatalf("magic link = %d", s)
		}
		phones = append(phones, c)
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(ev.ID)+"/start", nil, nil); s != 204 {
		t.Fatalf("start = %d", s)
	}
	call := mcpTool(t, ts, arr)
	// Both groups on the split: unlocking it must say which.
	if text, isErr := call("unlock_team", map[string]any{"team_id": team.ID}); !isErr || !strings.Contains(text, "which_group") {
		t.Errorf("unlocking a split team at its split without naming a group should be refused: %v %s", isErr, text)
	}
	if s := phones[1].doAny("POST", "/api/player/play/continue", map[string]string{"node": "split"}, nil); s != 200 {
		t.Fatalf("the well's group sets off = %d", s)
	}
	summary, isErr := call("get_event_state_summary", map[string]any{"event_id": ev.ID})
	if isErr || !strings.Contains(summary, `"step":"split"`) || !strings.Contains(summary, `{"label":"Church","node":"split"}`) || !strings.Contains(summary, `{"label":"Well","node":"pb"}`) {
		t.Fatalf("the summary should list the groups: %v %s", isErr, summary)
	}
	// The reply says where the well's group went — the join — not where
	// the church's group, the one the team's own view shows, stands.
	if text, isErr := call("unlock_team", map[string]any{"team_id": team.ID, "node": "pb"}); isErr || !strings.Contains(text, `"node":"join"`) {
		t.Fatalf("unlocking the well's group: %v %s", isErr, text)
	}
	summary, _ = call("get_event_state_summary", map[string]any{"event_id": ev.ID})
	if !strings.Contains(summary, `{"label":"Well","node":"join"}`) || !strings.Contains(summary, `{"label":"Church","node":"split"}`) {
		t.Errorf("after the rescue only the well's group moved: %s", summary)
	}
	if text, isErr := call("unlock_team", map[string]any{"team_id": team.ID, "node": "pb"}); !isErr || !strings.Contains(text, "team_moved") {
		t.Errorf("a second rescue naming the step it left: %v %s", isErr, text)
	}
	// A group named alone: the church's, set off from the split.
	if text, isErr := call("unlock_team", map[string]any{"team_id": team.ID, "group": "a"}); isErr {
		t.Fatalf("unlocking the church's group by name: %s", text)
	}
	if summary, _ = call("get_event_state_summary", map[string]any{"event_id": ev.ID}); !strings.Contains(summary, `{"label":"Church","node":"pa"}`) {
		t.Errorf("the church's group should be on its puzzle: %s", summary)
	}
	// Every group off the split, a rescue naming none must still say
	// which: nothing moved since the caller looked.
	if text, isErr := call("unlock_team", map[string]any{"team_id": team.ID}); !isErr || !strings.Contains(text, "which_group") {
		t.Errorf("unlocking a split team naming no group or step: %v %s", isErr, text)
	}
}
