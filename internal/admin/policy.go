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
 *   owner = org X  → org-owned. Visible to every user of org X.
 *                    Authoring (write) requires admin or org_admin in
 *                    that org — see canAuthor below. org_member sees
 *                    the row but can't edit puzzle content; they can
 *                    still run events, manage teams, and intervene
 *                    live. The split exists so a corporate customer
 *                    can hand "operate the day-of event" to an intern
 *                    without granting them rights to rewrite puzzle
 *                    copy or republish the adventure.
 *
 * Helpers are kept tiny and colocated with the role constants so that
 * future tweaks (eg. public-sharing flag, per-event facilitator
 * grants) land in one place.
 */

// isAdmin reports whether the caller is a global admin.
func isAdmin(u auth.User) bool { return u.Role == auth.RoleAdmin }

// canAuthor returns true when the caller may modify authored content
// (adventures, pages, hints, accept-lists, puzzle media). Admins
// always pass; org-bound users pass only when they are org_admin in
// the owning org. org_member is intentionally rejected — they can
// see and use the adventure to run events, but not rewrite its
// content.
//
// NULL ownership means "admin-owned, global catalog" so only admins
// can mutate those rows regardless of the caller's org role.
func canAuthor(u auth.User, ownerOrg sql.NullInt64) bool {
	if isAdmin(u) {
		return true
	}
	if !ownerOrg.Valid {
		return false
	}
	if u.Role != auth.RoleOrgAdmin {
		return false
	}
	return u.OrgID != nil && *u.OrgID == ownerOrg.Int64
}

// canEditOwned is the legacy "any org member can edit" gate kept for
// flows where the read/write distinction collapses (still used by
// some non-authoring helpers). New authoring code should call
// canAuthor instead.
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
