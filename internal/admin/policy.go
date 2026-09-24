package admin

import (
	"context"
	"database/sql"
	"errors"

	"github.com/klahr/zonventure/internal/auth"
)

// ErrForbidden is the policy-level "you can't do that" error returned
// by Authorize* helpers. Sentinel so callers outside HTTP land (the
// MCP server most notably) can map it to a 401/403 instead of
// inspecting message strings.
var ErrForbidden = errors.New("forbidden")

// AuthorizeAdventureRead checks the caller is allowed to read the
// named adventure under the same visibility rules ListAdventures
// applies: the published catalog visible to every org, org-owned
// visible to that org's members, plus the share/grant cascade. Returns the adventure id and the owning org
// on success.
//
// On miss returns sql.ErrNoRows (so callers can distinguish "no such
// adventure" from "exists but you can't see it"); on policy denial
// returns ErrForbidden.
func AuthorizeAdventureRead(ctx context.Context, db *sql.DB, slug string, user auth.User) (int64, sql.NullInt64, error) {
	h := &Handlers{DB: db}
	org, err := h.adventureOwner(ctx, slug)
	if err != nil {
		return 0, sql.NullInt64{}, err
	}
	// Reuse the staff-side visibility cascade so the MCP read path
	// honours the same grant + share table the arranger UI does.
	if !h.staffCanSeeAdventure(ctx, user, org, h.adventurePublished(ctx, slug), slug) {
		return 0, sql.NullInt64{}, ErrForbidden
	}
	advID, err := adventureIDBySlug(ctx, db, slug)
	if err != nil {
		return 0, sql.NullInt64{}, err
	}
	return advID, org, nil
}

// AuthorizeAdventureEdit is the canAuthor counterpart: passes only
// for callers who can mutate authored content (org_admins of the
// owning org). org_member is rejected — they can
// run events but not rewrite puzzle copy. Same error contract as
// AuthorizeAdventureRead.
func AuthorizeAdventureEdit(ctx context.Context, db *sql.DB, slug string, user auth.User) (int64, sql.NullInt64, error) {
	h := &Handlers{DB: db}
	org, err := h.adventureOwner(ctx, slug)
	if err != nil {
		return 0, sql.NullInt64{}, err
	}
	if !canAuthor(user, org) {
		return 0, sql.NullInt64{}, ErrForbidden
	}
	advID, err := adventureIDBySlug(ctx, db, slug)
	if err != nil {
		return 0, sql.NullInt64{}, err
	}
	return advID, org, nil
}

/* ---------------- Authoring policy ----------------
 *
 * Since orgs can author their own adventures, every CRUD handler needs
 * to answer two questions: can the caller see this row, and can the
 * caller modify it. The rules are uniform across adventures and
 * orphan library puzzles:
 *
 *   owner = org X  → org-owned. Visible to every user of org X.
 *                    Authoring (write) requires org_admin in that org
 *                    — see canAuthor below. org_member sees the row
 *                    but can't edit puzzle content; they can still run
 *                    events, manage teams, and intervene live. The
 *                    split exists so a corporate customer can hand
 *                    "operate the day-of event" to an intern without
 *                    granting them rights to rewrite puzzle copy or
 *                    republish the adventure.
 *   owner = the publisher org → also the platform catalog: published
 *                    adventures and library puzzles are visible to
 *                    every org (see catalog.go).
 *
 * The platform admin authors nothing and runs no events: they manage
 * organisations and curate the catalog through the admin-only
 * endpoints, so none of these helpers let them through.
 */

// isAdmin reports whether the caller is a global admin.
func isAdmin(u auth.User) bool { return u.Role == auth.RoleAdmin }

// canAuthor returns true when the caller may modify authored content
// (adventures, pages, hints, accept-lists, puzzle media): org_admin in
// the owning org. org_member is intentionally rejected — they can see
// and use the adventure to run events, but not rewrite its content.
func canAuthor(u auth.User, ownerOrg sql.NullInt64) bool {
	return u.Role == auth.RoleOrgAdmin && canEditOwned(u, ownerOrg)
}

// canEditOwned is the "any member of the owning org" gate kept for
// flows where the read/write distinction collapses (still used by
// some non-authoring helpers). New authoring code should call
// canAuthor instead.
func canEditOwned(u auth.User, ownerOrg sql.NullInt64) bool {
	return ownerOrg.Valid && u.OrgID != nil && *u.OrgID == ownerOrg.Int64
}

// callerOrgID returns the caller's active org_id as a NullInt64 — the
// owner stamped onto a row they create. NULL for callers outside any
// org, which canAuthor then refuses.
func callerOrgID(u auth.User) sql.NullInt64 {
	if u.OrgID == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *u.OrgID, Valid: true}
}

// staffCanSeeAdventure mirrors the visibility cascade ListAdventures
// uses (and AdventureDetail re-implements) so any staff-side surface
// that exposes adventure data — JSON DTOs, image bytes, audio bytes —
// applies the same gate. Returns true when the caller is allowed to
// read this adventure: member of the owning org, or the row is a
// published catalog adventure, or the caller's org holds an
// assignment / share grant for the slug.
//
// Read-only; does not gate writes (canAuthor handles those). DB-error
// on the grant lookup is treated as "not visible" — failing closed
// matches the rest of the asset gates.
func (h *Handlers) staffCanSeeAdventure(ctx context.Context, me auth.User, ownerOrg sql.NullInt64, published bool, slug string) bool {
	if me.OrgID == nil {
		return false
	}
	if canEditOwned(me, ownerOrg) {
		return true
	}
	if published && h.isCatalogOwner(ctx, ownerOrg) {
		return true
	}
	var n int
	if err := h.DB.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM adventure_assignments WHERE adventure_slug = $1 AND org_id = $2)
		+ (SELECT COUNT(*) FROM adventure_shares      WHERE adventure_slug = $1 AND org_id = $2)
	`, slug, *me.OrgID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// staffCanSeePuzzle: a puzzle attached to an adventure is visible
// wherever the adventure is; a library puzzle is visible to its own
// org, and the publisher's library to every org.
func (h *Handlers) staffCanSeePuzzle(ctx context.Context, me auth.User, advSlug sql.NullString, advOrg, pageOrg sql.NullInt64) bool {
	if me.OrgID == nil {
		return false
	}
	if advSlug.Valid {
		return h.staffCanSeeAdventure(ctx, me, advOrg, h.adventurePublished(ctx, advSlug.String), advSlug.String)
	}
	return canEditOwned(me, pageOrg) || h.isCatalogOwner(ctx, pageOrg)
}

// adventurePublished reads the catalog flag; a miss reads as false.
func (h *Handlers) adventurePublished(ctx context.Context, slug string) bool {
	var published bool
	_ = h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(published, 0) <> 0 FROM adventures WHERE slug = $1`, slug).Scan(&published)
	return published
}
