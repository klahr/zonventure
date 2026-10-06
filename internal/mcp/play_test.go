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

func TestPlayTools_AreRegisteredRead(t *testing.T) {
	t.Parallel()
	s := &Server{tools: newRegistry()}
	s.registerPlayTools()
	for _, name := range []string{"playtest_start", "playtest_act", "playtest_end"} {
		if got := s.tools.scopes[name]; got != scopeRead {
			t.Errorf("%s scope = %q, want read", name, got)
		}
	}
}

func TestPlayHelpers(t *testing.T) {
	t.Parallel()
	if got := morse("sos ab"); got != "... --- ... / .- -..." {
		t.Errorf("morse = %q", got)
	}
	a, b := scramble("x", "hello"), scramble("x", "hello")
	if strings.Join(a, "") != strings.Join(b, "") || len(a) != 5 {
		t.Errorf("scramble not stable: %v %v", a, b)
	}
	if got := heading(59.0, 18.0, 59.1, 18.0); got != "north" {
		t.Errorf("heading = %q", got)
	}
	if got := clock(3725); got != "1:02:05" {
		t.Errorf("clock = %q", got)
	}
}

// A blind playtest: the team must walk to the door, cannot see the
// answer, waits for the hint, guesses wrong, then right.
func TestPlaytest(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()

	var orgID, userID, otherID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('door', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct {
		name string
		id   *int64
	}{{"boss", &userID}, {"other", &otherID}} {
		if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ($1, 'x', 1) RETURNING id`, u.name).Scan(u.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, *u.id, orgID); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})

	res, _ := editAdventureGraph(ctx, s, json.RawMessage(`{"slug":"door","ops":[
		{"op":"put_zone","zone":{"slug":"gate","name":"The gate","lat":59.3300,"lng":18.0600,"radius_m":25}},
		{"op":"put_zone","zone":{"slug":"square","name":"The square","lat":59.3320,"lng":18.0600,"radius_m":25}},
		{"op":"put_node","node":{"slug":"start","type":"start"}},
		{"op":"put_node","node":{"slug":"q","type":"puzzle","puzzle":{
			"zone":"square",
			"blocks":[{"id":"b1","kind":"text","text":"What colour is the door?"},{"id":"b2","kind":"shake","text":"It glows when shaken"}],
			"hints":[{"text":"Think of a fiery red.","delay_seconds":300}],
			"outputs":[{"port":"right","trigger":"answer","points":10,"matchers":[{"id":"m1","kind":"exact","value":"vermilion"}]}]}}},
		{"op":"put_node","node":{"slug":"end","type":"end"}},
		{"op":"connect","from":"start","from_port":"out","to":"q"},
		{"op":"connect","from":"q","from_port":"right","to":"end"}
	]}`))
	if res.IsError {
		t.Fatalf("build: %s", textOf(res))
	}

	res, _ = playStart(ctx, s, json.RawMessage(`{"slug":"door","team_size":4}`))
	if res.IsError {
		t.Fatalf("start: %s", textOf(res))
	}
	screen := textOf(res)
	if !strings.Contains(screen, "What colour is the door?") || strings.Contains(screen, "vermilion") {
		t.Fatalf("first screen should show the question and not the answer:\n%s", screen)
	}
	if strings.Contains(screen, "It glows") {
		t.Errorf("a gadget's text should wait until it is used:\n%s", screen)
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

	if out := act(`"kind":"answer","guess":"red"`); !strings.Contains(out, "not at the place") {
		t.Errorf("answering away from the square should be refused:\n%s", out)
	}
	act(`"kind":"move","place":"The gate"`)
	if out := act(`"kind":"move","place":"square"`); !strings.Contains(out, "Walked to The square: 222 m") || !strings.Contains(out, "You are at The square") {
		t.Errorf("walk:\n%s", out)
	}
	if out := act(`"kind":"use","block":"b2"`); !strings.Contains(out, "It shows: It glows when shaken") {
		t.Errorf("used gadget:\n%s", out)
	}
	if out := act(`"kind":"hint"`); !strings.Contains(out, "Not possible") {
		t.Errorf("the hint should not be open yet:\n%s", out)
	}
	act(`"kind":"wait","seconds":300`)
	if out := act(`"kind":"hint"`); !strings.Contains(out, "Hint: Think of a fiery red.") {
		t.Errorf("hint:\n%s", out)
	}
	if out := act(`"kind":"answer","guess":"red"`); !strings.Contains(out, "Wrong.") {
		t.Errorf("wrong answer:\n%s", out)
	}
	if out := act(`"kind":"answer","guess":"Vermilion"`); !strings.Contains(out, "Right!") || !strings.Contains(out, "The adventure is over") {
		t.Errorf("right answer:\n%s", out)
	}

	// Only the caller who started it may play or end it.
	otherCtx := auth.WithUser(context.Background(), auth.User{ID: otherID, Username: "other", Role: auth.RoleOrgAdmin, OrgID: &orgID})
	if res, _ := playAct(otherCtx, s, json.RawMessage(`{"session":"`+id+`","kind":"look"}`)); !res.IsError {
		t.Error("another user played someone else's session")
	}

	res, _ = playEnd(ctx, s, json.RawMessage(`{"session":"`+id+`"}`))
	if res.IsError {
		t.Fatalf("end: %s", textOf(res))
	}
	var sum struct {
		Finished bool    `json:"finished"`
		Wrong    int     `json:"wrong_answers"`
		Hints    int     `json:"hints"`
		Walked   float64 `json:"walked_m"`
		Played   float64 `json:"played_seconds"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &sum); err != nil {
		t.Fatal(err)
	}
	if !sum.Finished || sum.Wrong != 1 || sum.Hints != 1 || sum.Walked < 200 || sum.Played < 300 {
		t.Errorf("summary %+v", sum)
	}
	if res, _ := playAct(ctx, s, json.RawMessage(`{"session":"`+id+`","kind":"look"}`)); !res.IsError {
		t.Error("an ended session still plays")
	}
}
