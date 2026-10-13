package auth

import (
	"context"
	"database/sql"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
)

// A user logged in with no organisation who is then added to one gets
// that org on their next request, without logging in again.
func TestLookup_AdoptsOrgAddedDuringSession(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)
	tok, err := CompleteLogin(ctx, db, User{ID: uid}, nil, "", "", sql.NullInt64{})
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if u, ok, err := Lookup(ctx, db, tok); err != nil || !ok || u.Role != RoleLobby {
		t.Fatalf("before: %+v %v %v, want lobby", u, ok, err)
	}
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Added Org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`, uid, orgID); err != nil {
		t.Fatal(err)
	}
	u, ok, err := Lookup(ctx, db, tok)
	if err != nil || !ok || u.Role != RoleOrgMember || u.OrgID == nil || *u.OrgID != orgID {
		t.Fatalf("after: %+v %v %v, want org_member of %d", u, ok, err, orgID)
	}
}

// SoleMember tells the account-deletion warning whether the org goes too.
func TestListMemberships_SoleMember(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	uid := mustSeedUser(t, db)
	var orgID, other int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Solo Org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, uid, orgID); err != nil {
		t.Fatal(err)
	}
	if ms, err := ListMemberships(ctx, db, uid); err != nil || len(ms) != 1 || !ms[0].SoleMember {
		t.Fatalf("alone: %+v %v, want one sole membership", ms, err)
	}
	if err := db.QueryRow(`INSERT INTO users(username) VALUES ('bob') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`, other, orgID); err != nil {
		t.Fatal(err)
	}
	if ms, err := ListMemberships(ctx, db, uid); err != nil || len(ms) != 1 || ms[0].SoleMember {
		t.Fatalf("with bob: %+v %v, want not sole", ms, err)
	}
}
