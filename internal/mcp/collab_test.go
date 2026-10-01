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

// The assistant picks up a task an author gave it in a thread: it finds
// it, replies as the assistant, resolves it, and hands the step back
// for review.
func TestCollabTools_PickUpATask(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, userID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, org_id) VALUES ('talk', 't', '', '', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('boss', 'x', 1) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	person := withScopes(auth.WithUser(context.Background(), auth.User{ID: userID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID}), []string{"read", "write"})
	assistant := auth.WithAPIToken(person)

	// The author asks, in a thread on the riddle.
	res, _ := commentOnAdventure(person, s, json.RawMessage(`{"slug":"talk","node":"riddle","body":"@assistant two more hints please"}`))
	if res.IsError {
		t.Fatalf("ask: %s", textOf(res))
	}
	res, _ = commentOnAdventure(person, s, json.RawMessage(`{"slug":"talk","node":"end","body":"Nice ending."}`))
	if res.IsError {
		t.Fatalf("other thread: %s", textOf(res))
	}

	res, _ = listAdventureThreads(assistant, s, json.RawMessage(`{"slug":"talk","tasks_only":true}`))
	var tasks struct {
		Threads []struct {
			ID   int64  `json:"id"`
			Node string `json:"node"`
		} `json:"threads"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &tasks); err != nil || len(tasks.Threads) != 1 || tasks.Threads[0].Node != "riddle" {
		t.Fatalf("tasks = %s", textOf(res))
	}
	id := tasks.Threads[0].ID

	res, _ = commentOnAdventure(assistant, s, json.RawMessage(`{"slug":"talk","thread_id":`+itoa(id)+`,"body":"Added two hints."}`))
	if res.IsError {
		t.Fatalf("reply: %s", textOf(res))
	}
	if res, _ = resolveAdventureThread(assistant, s, json.RawMessage(`{"slug":"talk","thread_id":`+itoa(id)+`}`)); res.IsError {
		t.Fatalf("resolve: %s", textOf(res))
	}
	if res, _ = setStepStatus(assistant, s, json.RawMessage(`{"slug":"talk","node":"riddle","status":"review","owner":"@boss"}`)); res.IsError {
		t.Fatalf("status: %s", textOf(res))
	}
	if res, _ = setStepStatus(assistant, s, json.RawMessage(`{"slug":"talk","node":"riddle","owner":"nobody"}`)); !res.IsError || !strings.Contains(textOf(res), "boss") {
		t.Errorf("unknown owner: %s", textOf(res))
	}

	// A proposed change is a suggestion thread on the step, not an edit.
	if _, err := db.Exec(`INSERT INTO adventure_graph_drafts (adventure_id, graph) SELECT id, '{"nodes":[{"slug":"riddle","type":"story","title":"Riddle"}],"edges":[]}' FROM adventures WHERE slug = 'talk'`); err != nil {
		t.Fatal(err)
	}
	res, _ = suggestStepChange(assistant, s, json.RawMessage(`{"slug":"talk","node":{"slug":"riddle","type":"story","title":"Harder riddle"},"note":"Clearer title."}`))
	if res.IsError {
		t.Fatalf("suggest: %s", textOf(res))
	}
	if res, _ = suggestStepChange(assistant, s, json.RawMessage(`{"slug":"talk","node":{"slug":"nope","type":"story"}}`)); !res.IsError {
		t.Error("a suggestion for a missing step was accepted")
	}

	// Done: no open tasks; the reply is the assistant's; the step waits for review.
	res, _ = listAdventureThreads(person, s, json.RawMessage(`{"slug":"talk","tasks_only":true}`))
	if !strings.Contains(textOf(res), `"threads":[]`) {
		t.Errorf("the task is still open: %s", textOf(res))
	}
	res, _ = listAdventureThreads(person, s, json.RawMessage(`{"slug":"talk"}`))
	if out := textOf(res); !strings.Contains(out, `"suggestion_state":"open"`) || !strings.Contains(out, "Harder riddle") {
		t.Errorf("no open suggestion: %s", out)
	}
	res, _ = listAdventureThreads(person, s, json.RawMessage(`{"slug":"talk","open_only":false,"node":"riddle"}`))
	if out := textOf(res); !strings.Contains(out, `"assistant":true`) || !strings.Contains(out, `"status":"review"`) || !strings.Contains(out, `"name":"boss"`) {
		t.Errorf("after: %s", out)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
