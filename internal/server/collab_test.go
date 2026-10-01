package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/server"
	"github.com/klahr/zonventure/internal/ws"
)

// Authors talk about an adventure's steps: a thread on a step, a reply
// that mentions a colleague, an @assistant task, resolving; a step's
// status and owner; and what each author last saw of the draft.
func TestCollab_ThreadsStatusSeen(t *testing.T) {
	ts, db, _, cleanup := seedPurchaseStack(t, server.Config{Hub: ws.NewHub()})
	defer cleanup()

	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures", map[string]string{"slug": "talk", "title": "Talk"}, nil); s != 201 {
		t.Fatalf("create adventure = %d", s)
	}
	// A colleague in the same organisation, to mention and give a step.
	var annaID int64
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('anna', 'x', 1) RETURNING id`).Scan(&annaID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) SELECT $1, org_id, 'org_admin' FROM adventures WHERE slug = 'talk'`, annaID); err != nil {
		t.Fatal(err)
	}

	var made struct {
		ThreadID int64 `json:"thread_id"`
	}
	if s := boss.do("POST", "/api/admin/adventures/talk/threads", map[string]string{"node": "riddle", "body": "Too easy? @anna"}, &made); s != 201 {
		t.Fatalf("create thread = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures/talk/threads", map[string]string{"body": "  "}, nil); s != 400 {
		t.Errorf("empty comment = %d, want 400", s)
	}
	reply := "/api/admin/adventures/talk/threads/" + itoa(made.ThreadID) + "/comments"
	if s := boss.do("POST", reply, map[string]string{"body": "@assistant make it harder"}, nil); s != 201 {
		t.Fatalf("reply = %d", s)
	}
	if s := boss.do("PUT", "/api/admin/adventures/talk/steps/riddle/status", map[string]any{"status": "review", "owner_id": annaID}, nil); s != 204 {
		t.Fatalf("set status = %d", s)
	}
	if s := boss.do("PUT", "/api/admin/adventures/talk/steps/riddle/status", map[string]any{"status": "done"}, nil); s != 400 {
		t.Errorf("unknown status = %d, want 400", s)
	}
	// Changing only the status keeps the owner.
	if s := boss.do("PUT", "/api/admin/adventures/talk/steps/riddle/status", map[string]any{"status": "ready"}, nil); s != 204 {
		t.Fatalf("set status again = %d", s)
	}

	var c struct {
		Threads []struct {
			Node         string `json:"node"`
			ForAssistant bool   `json:"for_assistant"`
			Comments     []struct {
				Body     string  `json:"body"`
				Mentions []int64 `json:"mentions"`
			} `json:"comments"`
		} `json:"threads"`
		Steps []struct {
			Node   string `json:"node"`
			Status string `json:"status"`
			Owner  *struct {
				Name string `json:"name"`
			} `json:"owner"`
		} `json:"steps"`
		People []struct {
			Name string `json:"name"`
		} `json:"people"`
	}
	if s := boss.do("GET", "/api/admin/adventures/talk/collab", nil, &c); s != 200 {
		t.Fatalf("collab = %d", s)
	}
	if len(c.Threads) != 1 || c.Threads[0].Node != "riddle" || !c.Threads[0].ForAssistant || len(c.Threads[0].Comments) != 2 {
		t.Fatalf("threads = %+v", c.Threads)
	}
	if m := c.Threads[0].Comments[0].Mentions; len(m) != 1 || m[0] != annaID {
		t.Errorf("mentions = %v, want anna", m)
	}
	if len(c.Steps) != 1 || c.Steps[0].Status != "ready" || c.Steps[0].Owner == nil || c.Steps[0].Owner.Name != "anna" {
		t.Errorf("steps = %+v", c.Steps)
	}
	if len(c.People) != 2 {
		t.Errorf("people = %+v, want boss and anna", c.People)
	}

	if s := boss.do("PATCH", "/api/admin/adventures/talk/threads/"+itoa(made.ThreadID), map[string]bool{"resolved": true}, nil); s != 204 {
		t.Fatalf("resolve = %d", s)
	}

	// Seen: nothing before the first visit; the draft as it was after.
	if s := boss.do("GET", "/api/admin/adventures/talk/seen", nil, nil); s != 204 {
		t.Errorf("seen before any visit = %d, want 204", s)
	}
	env := map[string]any{"format": "zonventure-adventure-v2", "graph": map[string]any{
		"nodes": []map[string]any{{"slug": "start", "type": "start"}, {"slug": "end", "type": "end"}},
		"edges": []map[string]any{{"from": "start", "from_port": "out", "to": "end"}},
	}}
	if s := boss.do("PUT", "/api/admin/adventures/talk/graph", env, nil); s != 200 {
		t.Fatalf("save = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures/talk/seen", nil, nil); s != 204 {
		t.Fatalf("mark seen = %d", s)
	}
	var seen struct {
		Graph struct {
			Nodes []struct {
				Slug string `json:"slug"`
			} `json:"nodes"`
		} `json:"graph"`
	}
	if s := boss.do("GET", "/api/admin/adventures/talk/seen", nil, &seen); s != 200 || len(seen.Graph.Nodes) != 2 {
		t.Errorf("seen = %d, %+v", s, seen)
	}
}

// A suggestion proposes a change to a step as a thread: refused when it
// changes nothing or is about another step, decided once. Reactions
// toggle, on comments only.
func TestCollab_SuggestionsAndReactions(t *testing.T) {
	ts, _, _, cleanup := seedPurchaseStack(t, server.Config{Hub: ws.NewHub()})
	defer cleanup()
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	if s := boss.do("POST", "/api/admin/adventures", map[string]string{"slug": "sugg", "title": "S"}, nil); s != 201 {
		t.Fatalf("create adventure = %d", s)
	}
	base := map[string]any{"slug": "riddle", "type": "story", "title": "Riddle"}
	proposed := map[string]any{"slug": "riddle", "type": "story", "title": "A better riddle"}
	threads := "/api/admin/adventures/sugg/threads"
	if s := boss.do("POST", threads, map[string]any{"node": "riddle", "suggestion": map[string]any{"base": base, "proposed": base}}, nil); s != 400 {
		t.Errorf("a suggestion changing nothing = %d, want 400", s)
	}
	if s := boss.do("POST", threads, map[string]any{"node": "other", "suggestion": map[string]any{"base": base, "proposed": proposed}}, nil); s != 400 {
		t.Errorf("a suggestion about another step = %d, want 400", s)
	}
	var made struct {
		ThreadID  int64 `json:"thread_id"`
		CommentID int64 `json:"comment_id"`
	}
	if s := boss.do("POST", threads, map[string]any{"node": "riddle", "suggestion": map[string]any{"base": base, "proposed": proposed}}, &made); s != 201 {
		t.Fatalf("suggest = %d", s)
	}
	thread := threads + "/" + itoa(made.ThreadID)
	if s := boss.do("PATCH", thread, map[string]string{"suggestion_state": "accepted"}, nil); s != 204 {
		t.Fatalf("accept = %d", s)
	}
	if s := boss.do("PATCH", thread, map[string]string{"suggestion_state": "rejected"}, nil); s != 409 {
		t.Errorf("deciding twice = %d, want 409", s)
	}

	react := "/api/admin/adventures/sugg/reactions"
	var on struct {
		On bool `json:"on"`
	}
	comment := "comment:" + itoa(made.CommentID)
	if s := boss.do("POST", react, map[string]string{"target": comment, "emoji": "🔥"}, &on); s != 200 || !on.On {
		t.Fatalf("react = %d, %+v", s, on)
	}
	if s := boss.do("POST", react, map[string]string{"target": comment, "emoji": "👍"}, nil); s != 200 {
		t.Fatalf("second emoji = %d", s)
	}
	if s := boss.do("POST", react, map[string]string{"target": comment, "emoji": "🍕"}, nil); s != 400 {
		t.Errorf("unknown emoji = %d, want 400", s)
	}
	if s := boss.do("POST", react, map[string]string{"target": "step:riddle", "emoji": "👍"}, nil); s != 400 {
		t.Errorf("reaction on a step = %d, want 400", s)
	}
	if s := boss.do("POST", react, map[string]string{"target": "comment:999999", "emoji": "👍"}, nil); s != 404 {
		t.Errorf("reaction to a missing comment = %d, want 404", s)
	}
	if s := boss.do("POST", react, map[string]string{"target": comment, "emoji": "🔥"}, &on); s != 200 || on.On {
		t.Errorf("second toggle = %d, %+v; want it off", s, on)
	}

	var c struct {
		Threads []struct {
			SuggestionState string          `json:"suggestion_state"`
			ResolvedAt      *string         `json:"resolved_at"`
			Suggestion      json.RawMessage `json:"suggestion"`
		} `json:"threads"`
		Reactions []struct {
			Target string  `json:"target"`
			Emoji  string  `json:"emoji"`
			Users  []int64 `json:"users"`
		} `json:"reactions"`
	}
	if s := boss.do("GET", "/api/admin/adventures/sugg/collab", nil, &c); s != 200 {
		t.Fatalf("collab = %d", s)
	}
	if len(c.Threads) != 1 || c.Threads[0].SuggestionState != "accepted" || c.Threads[0].ResolvedAt == nil || !strings.Contains(string(c.Threads[0].Suggestion), "A better riddle") {
		t.Errorf("threads = %+v", c.Threads)
	}
	if len(c.Reactions) != 1 || c.Reactions[0].Emoji != "👍" || len(c.Reactions[0].Users) != 1 {
		t.Errorf("reactions = %+v", c.Reactions)
	}

	// Deleting a comment takes its reactions along.
	if s := boss.do("DELETE", "/api/admin/adventures/sugg/comments/"+itoa(made.CommentID), nil, nil); s != 204 {
		t.Fatalf("delete = %d", s)
	}
	c.Reactions = nil
	boss.do("GET", "/api/admin/adventures/sugg/collab", nil, &c)
	if len(c.Reactions) != 0 {
		t.Errorf("reactions after delete = %+v", c.Reactions)
	}
}
