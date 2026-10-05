package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// An organisation with nothing visible gets an empty JSON array, not
// null, so a client can't mistake "no adventures" for a failure.
func TestListAdventures_EmptyIsArray(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Empty') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	ctx := auth.WithUser(context.Background(), auth.User{ID: 1, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID})

	res, _ := listAdventures(ctx, s, json.RawMessage(`{}`))
	if res.IsError {
		t.Fatalf("list_adventures: %s", textOf(res))
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(textOf(res)), &list); err != nil || list == nil {
		t.Fatalf("list_adventures = %q, want a JSON array", textOf(res))
	}
	if len(list) != 0 {
		t.Fatalf("got %d adventures, want 0", len(list))
	}
}
