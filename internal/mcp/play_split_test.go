package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// The assistant builds a split with its graph tools, as the authoring
// guide shows, and playtests it blind: the screens say the team has
// split up, where each group is, and how to go on.
func TestPlaytestSplit(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('fork', 't', '', '', $1)`, orgID); err != nil {
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

	res, _ := editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"fork","ops":[
		{"op":"put_node","node":{"slug":"start","type":"start"}},
		{"op":"put_node","node":{"slug":"split-up","type":"split","title":"Split up","split":{"text":"Two ways.","paths":[{"port":"church","label":"Church party"},{"port":"well","label":"Well party"}]}}},
		{"op":"put_node","node":{"slug":"bell","type":"puzzle","title":"The bell","puzzle":{"blocks":[{"id":"b1","kind":"text","text":"How many bells?"}],"outputs":[{"port":"ok","trigger":"answer","points":5,"matchers":[{"id":"m1","kind":"exact","value":"3"}]}]}}},
		{"op":"put_node","node":{"slug":"water","type":"puzzle","title":"The well","puzzle":{"blocks":[{"id":"b1","kind":"text","text":"How deep?"}],"outputs":[{"port":"ok","trigger":"answer","points":5,"matchers":[{"id":"m1","kind":"exact","value":"9"}]}]}}},
		{"op":"put_node","node":{"slug":"meet","type":"join","title":"The square"}},
		{"op":"put_node","node":{"slug":"end","type":"end","end":{"outcome":"complete"}}},
		{"op":"connect","from":"start","from_port":"out","to":"split-up"},
		{"op":"connect","from":"split-up","from_port":"church","to":"bell"},
		{"op":"connect","from":"split-up","from_port":"well","to":"water"},
		{"op":"connect","from":"bell","from_port":"ok","to":"meet"},
		{"op":"connect","from":"water","from_port":"ok","to":"meet"},
		{"op":"connect","from":"meet","from_port":"out","to":"end"}
	]}`))
	if res.IsError {
		t.Fatalf("build: %s", textOf(res))
	}
	if out := textOf(res); strings.Contains(out, `"severity":"error"`) {
		t.Fatalf("the split should validate:\n%s", out)
	}

	res, _ = playStart(ctx, s, json.RawMessage(`{"slug":"fork","team_size":4}`))
	if res.IsError {
		t.Fatalf("start: %s", textOf(res))
	}
	screen := textOf(res)
	for _, want := range []string{"Two ways.", "you are with Church party", "- Church party: at Split up", "- Well party: at Split up", "continue to set off"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the split's screen lacks %q:\n%s", want, screen)
		}
	}
	id := screen[strings.Index(screen, "Session: ")+len("Session: "):]
	id = id[:strings.IndexByte(id, '\n')]
	act := func(args string) string {
		t.Helper()
		res, _ := playAct(ctx, s, json.RawMessage(`{"session":"`+id+`",`+args+`}`))
		if res.IsError {
			t.Fatalf("%s: %s", args, textOf(res))
		}
		return textOf(res)
	}
	if out := act(`"kind":"continue"`); !strings.Contains(out, "How many bells?") || !strings.Contains(out, "- Church party: at The bell") {
		t.Errorf("the church party's path:\n%s", out)
	}
	if out := act(`"kind":"answer","guess":"3"`); !strings.Contains(out, "you are with Well party") || !strings.Contains(out, "- Church party: waiting at The square") {
		t.Errorf("on to the other group:\n%s", out)
	}
	act(`"kind":"continue"`)
	if out := act(`"kind":"answer","guess":"9"`); !strings.Contains(out, "The adventure is over") {
		t.Errorf("together and finished:\n%s", out)
	}
}
