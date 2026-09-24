package admin

import (
	"database/sql"
	"testing"

	"github.com/klahr/zonventure/internal/auth"
)

// TestCanAuthor walks the truth table for the authoring gate. The
// short story:
//
//   - the platform admin never passes — they curate, not author
//   - org_admin passes iff they are in the owning org
//   - org_member never passes (they can run events; they cannot edit
//     the underlying adventure content)
//   - ownerless rows are nobody's to author
//
// This is the single test that prevents a future refactor from
// quietly re-collapsing the org_admin/org_member distinction.
func TestCanAuthor(t *testing.T) {
	orgA := int64(1)
	orgB := int64(2)
	owned := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	orphan := sql.NullInt64{}

	type tcase struct {
		name  string
		user  auth.User
		owner sql.NullInt64
		want  bool
	}
	for _, c := range []tcase{
		{
			"global admin rejected on org content",
			auth.User{Role: auth.RoleAdmin},
			owned(orgA),
			false,
		},
		{
			"global admin rejected on ownerless rows",
			auth.User{Role: auth.RoleAdmin},
			orphan,
			false,
		},
		{
			"org_admin in owning org",
			auth.User{Role: auth.RoleOrgAdmin, OrgID: &orgA},
			owned(orgA),
			true,
		},
		{
			"org_admin in different org rejected",
			auth.User{Role: auth.RoleOrgAdmin, OrgID: &orgB},
			owned(orgA),
			false,
		},
		{
			"org_admin can't author orphan rows",
			auth.User{Role: auth.RoleOrgAdmin, OrgID: &orgA},
			orphan,
			false,
		},
		{
			"org_member in owning org REJECTED — runs events but can't author",
			auth.User{Role: auth.RoleOrgMember, OrgID: &orgA},
			owned(orgA),
			false,
		},
		{
			"org_member in different org rejected",
			auth.User{Role: auth.RoleOrgMember, OrgID: &orgB},
			owned(orgA),
			false,
		},
		{
			"org_member can't author orphan rows",
			auth.User{Role: auth.RoleOrgMember, OrgID: &orgA},
			orphan,
			false,
		},
		{
			"unauthenticated-shaped user rejected",
			auth.User{},
			owned(orgA),
			false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := canAuthor(c.user, c.owner); got != c.want {
				t.Errorf("canAuthor = %v, want %v", got, c.want)
			}
		})
	}
}

// TestCanEditOwnedStillPermissive confirms canEditOwned hasn't been
// accidentally collapsed into canAuthor. It's deliberately the
// permissive "any org member can read/write" gate kept around for
// non-authoring flows.
func TestCanEditOwnedStillPermissive(t *testing.T) {
	orgA := int64(1)
	owned := sql.NullInt64{Int64: orgA, Valid: true}
	memberInOrg := auth.User{Role: auth.RoleOrgMember, OrgID: &orgA}
	if !canEditOwned(memberInOrg, owned) {
		t.Errorf("canEditOwned(org_member, owned-by-their-org) should be true; " +
			"if you want to tighten this, switch the call site to canAuthor instead")
	}
	if canAuthor(memberInOrg, owned) {
		t.Errorf("canAuthor(org_member, owned-by-their-org) should be false — " +
			"this is the whole point of the split")
	}
}
