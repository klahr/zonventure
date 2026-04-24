package admin

import (
	"database/sql"

	"git.sr.ht/~klahr/zv/internal/auth"
)

/* ---------------- Authoring policy ----------------
 *
 * Since orgs can author their own adventures, every CRUD handler needs
 * to answer two questions: can the caller see this row, and can the
 * caller modify it. The rules are uniform across adventures and
 * orphan library puzzles:
 *
 *   owner = NULL   → admin-owned (global catalog / global library).
 *                    Visible to everyone; editable only by admins.
 *   owner = org X  → org-owned. Visible + editable to users of org X.
 *                    Admins can always edit as a last-resort override.
 *
 * Helpers are kept tiny and colocated with the role constants so that
 * future tweaks (eg. public-sharing flag, org-admin vs org-member
 * distinction) land in one place.
 */

// isAdmin reports whether the caller is a global admin.
func isAdmin(u auth.User) bool { return u.Role == auth.RoleAdmin }

// canEditOwned returns true when the caller can modify a resource
// whose ownership is expressed as a nullable org_id. NULL ownership
// means "admin-owned, global" so only admins can mutate those.
func canEditOwned(u auth.User, ownerOrg sql.NullInt64) bool {
	if isAdmin(u) {
		return true
	}
	if !ownerOrg.Valid {
		return false
	}
	return u.OrgID != nil && *u.OrgID == ownerOrg.Int64
}

// canSeeOwned is more permissive: globally-owned resources are the
// shared catalog and every authoring-tier user can read them. Org-owned
// resources are still private to the owning org.
func canSeeOwned(u auth.User, ownerOrg sql.NullInt64) bool {
	if isAdmin(u) {
		return true
	}
	if !ownerOrg.Valid {
		return true
	}
	return u.OrgID != nil && *u.OrgID == ownerOrg.Int64
}

// callerOrgID returns the caller's owning org_id as a NullInt64.
// Admins pass it as NULL (admin-created resources become part of the
// global catalog), and org users get their own org stamped onto the
// row they create.
func callerOrgID(u auth.User) sql.NullInt64 {
	if u.OrgID == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *u.OrgID, Valid: true}
}
