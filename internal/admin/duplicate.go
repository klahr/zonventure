package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// DuplicateAdventure clones a source adventure into the caller's
// active organisation. The clone is independent: pages, hints,
// accept-lists, images, audio, and the cover avatar all copy across
// so an arranger editing the copy can't break the source for anyone
// else.
//
// Authorisation:
//
//   - Caller must be authenticated and have an active org (admin
//     callers don't go through this endpoint; they create directly).
//   - Caller must be org_admin in their active org — duplicating
//     creates a new authored row, which org_member is not allowed
//     to do (canAuthor on the *destination*).
//   - Caller must canSeeOwned the source — i.e., either it's a
//     global catalog row (admin-owned and published) or it's owned
//     by the caller's own org. Cross-org cloning is deliberately
//     rejected.
//
// The cloned slug defaults to "<source>-<random6>" so duplicating
// is idempotent without retry probes; the arranger renames via the
// existing slug-edit flow if they want a friendlier value. Title
// defaults to the source's title with " (copy)" appended unless the
// caller provided their own.
func (h *Handlers) DuplicateAdventure(w http.ResponseWriter, r *http.Request) {
	sourceSlug := chi.URLParam(r, "slug")
	if sourceSlug == "" {
		httpx.Error(w, http.StatusBadRequest, "slug required")
		return
	}
	me, _ := auth.FromContext(r.Context())
	if me.OrgID == nil {
		httpx.Error(w, http.StatusBadRequest,
			"duplicate requires an active organization (admins create adventures directly)")
		return
	}
	// canAuthor on the *destination*: the caller must be allowed to
	// create authored rows in their own org. Re-uses the same
	// helper that gates other authoring endpoints so the policy
	// stays one place.
	dest := sql.NullInt64{Int64: *me.OrgID, Valid: true}
	if !canAuthor(me, dest) {
		httpx.Error(w, http.StatusForbidden,
			"only org admins can duplicate adventures")
		return
	}

	// Source visibility check.
	sourceOrg, err := h.adventureOwner(r.Context(), sourceSlug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "source adventure not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !canSeeOwned(me, sourceOrg) {
		httpx.Error(w, http.StatusForbidden, "source adventure is not visible to this organization")
		return
	}

	var req duplicateAdventureRequest
	// Optional body. Empty body = use defaults.
	if r.ContentLength > 0 {
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
	}

	newSlug := strings.TrimSpace(deref(req.Slug))
	if newSlug == "" {
		s, err := generateDuplicateSlug(sourceSlug)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "slug generation failed")
			return
		}
		newSlug = s
	}
	if !validSlug(newSlug) {
		httpx.Error(w, http.StatusBadRequest,
			"slug must be lowercase letters, digits, and dashes")
		return
	}

	if err := h.cloneAdventure(r.Context(), sourceSlug, newSlug, *me.OrgID, me.ID, deref(req.Title)); err != nil {
		if errors.Is(err, errSlugTaken) {
			httpx.Error(w, http.StatusConflict, "slug already in use")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "duplicate failed", err)
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{
		"slug": newSlug,
	})
}

type duplicateAdventureRequest struct {
	// Title overrides the cloned adventure's title. When empty the
	// clone uses "<source-title> (copy)".
	Title *string `json:"title,omitempty"`
	// Slug overrides the cloned adventure's slug. When empty the
	// server generates "<source>-<random6>" — predictable shape,
	// collision-free in practice.
	Slug *string `json:"slug,omitempty"`
}

var errSlugTaken = errors.New("duplicate: slug taken")

// cloneAdventure does the transactional copy. Pages, hints,
// accept-lists, images, and audio all carry across; the new
// adventure is owned by destOrgID, starts unpublished, and is
// explicitly NOT a template (so duplicating a template doesn't
// create another template — only admins promote a row to template
// status).
func (h *Handlers) cloneAdventure(ctx context.Context, sourceSlug, newSlug string, destOrgID int64, byUserID int64, titleOverride string) error {
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck — Commit takes over on success

	// Source row.
	var (
		sourceAdvID int64
		sourceTitle string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, title FROM adventures WHERE slug = $1
	`, sourceSlug).Scan(&sourceAdvID, &sourceTitle); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	newTitle := titleOverride
	if newTitle == "" {
		newTitle = sourceTitle + " (copy)"
	}

	// Insert new adventure. INSERT-SELECT preserves the meta
	// columns (description, author, intro/outro, brand colors,
	// avatar bytes) without round-tripping them. Override:
	//   - slug + title from caller / generator
	//   - org_id = caller's org
	//   - created_by_user_id = caller
	//   - published = 0 (always start unpublished)
	var newAdvID int64
	// `forked_from_slug` records the source slug so the billing rule
	// at event-creation time can distinguish original org content
	// (free) from Catalog forks (billable when Stripe is wired). The
	// reference is frozen at fork time — if the source is later
	// renamed or deleted, this column stays put.
	err = tx.QueryRowContext(ctx, `
		INSERT INTO adventures(
			slug, title, description, author, intro_text, outro_text,
			accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
			org_id, created_by_user_id, published, language,
			forked_from_slug,
			avatar_mime_type, avatar_size_bytes, avatar_content)
		SELECT
			$1, $2, description, author, intro_text, outro_text,
			accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
			$3, $4, 0, language,
			slug,
			avatar_mime_type, avatar_size_bytes, avatar_content
		FROM adventures WHERE id = $5
		RETURNING id
	`, newSlug, newTitle, destOrgID, byUserID, sourceAdvID).Scan(&newAdvID)
	if err != nil {
		// Postgres unique-violation error message contains the
		// constraint name. Treat as 409 — the caller picked a slug
		// that's already in use.
		if isUniqueViolation(err) {
			return errSlugTaken
		}
		return fmt.Errorf("insert adventure: %w", err)
	}

	// Pages — track old→new id mapping so hints and per-page assets
	// can rebind to the new page rows.
	pageRows, err := tx.QueryContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 ORDER BY idx`, sourceAdvID)
	if err != nil {
		return fmt.Errorf("load pages: %w", err)
	}
	var oldPageIDs []int64
	for pageRows.Next() {
		var id int64
		if err := pageRows.Scan(&id); err != nil {
			pageRows.Close()
			return err
		}
		oldPageIDs = append(oldPageIDs, id)
	}
	pageRows.Close()
	if err := pageRows.Err(); err != nil {
		return err
	}

	pageMap := make(map[int64]int64, len(oldPageIDs))
	for _, oldID := range oldPageIDs {
		var newID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_pages(
				adventure_id, idx, title, prompt, accept,
				lat, lng, radius_m, disabled, notes, org_id)
			SELECT $1, idx, title, prompt, accept, lat, lng, radius_m, disabled, notes, $2
			FROM adventure_pages WHERE id = $3
			RETURNING id
		`, newAdvID, destOrgID, oldID).Scan(&newID)
		if err != nil {
			return fmt.Errorf("clone page %d: %w", oldID, err)
		}
		pageMap[oldID] = newID
	}

	// Per-page assets: images and audio. INSERT-SELECT copies the
	// blob columns straight over without an extra round-trip
	// through the application.
	for oldID, newID := range pageMap {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content, alt_text)
			SELECT $1, idx, mime_type, size_bytes, content, alt_text
			FROM adventure_page_images WHERE page_id = $2
		`, newID, oldID); err != nil {
			return fmt.Errorf("clone page images: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO adventure_page_audio(page_id, idx, mime_type, size_bytes, content, description)
			SELECT $1, idx, mime_type, size_bytes, content, description
			FROM adventure_page_audio WHERE page_id = $2
		`, newID, oldID); err != nil {
			return fmt.Errorf("clone page audio: %w", err)
		}

		// Hints belong to a page — copy them, then their per-hint
		// images and audio. Same id-mapping pattern as pages.
		hintRows, err := tx.QueryContext(ctx,
			`SELECT id FROM adventure_page_hints WHERE page_id = $1 ORDER BY idx`, oldID)
		if err != nil {
			return fmt.Errorf("load hints: %w", err)
		}
		var oldHintIDs []int64
		for hintRows.Next() {
			var hid int64
			if err := hintRows.Scan(&hid); err != nil {
				hintRows.Close()
				return err
			}
			oldHintIDs = append(oldHintIDs, hid)
		}
		hintRows.Close()
		if err := hintRows.Err(); err != nil {
			return err
		}
		for _, oldHID := range oldHintIDs {
			var newHID int64
			err := tx.QueryRowContext(ctx, `
				INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
				SELECT $1, idx, text, delay_seconds
				FROM adventure_page_hints WHERE id = $2
				RETURNING id
			`, newID, oldHID).Scan(&newHID)
			if err != nil {
				return fmt.Errorf("clone hint %d: %w", oldHID, err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content, alt_text)
				SELECT $1, idx, mime_type, size_bytes, content, alt_text
				FROM adventure_page_hint_images WHERE hint_id = $2
			`, newHID, oldHID); err != nil {
				return fmt.Errorf("clone hint images: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO adventure_page_hint_audio(hint_id, idx, mime_type, size_bytes, content, description)
				SELECT $1, idx, mime_type, size_bytes, content, description
				FROM adventure_page_hint_audio WHERE hint_id = $2
			`, newHID, oldHID); err != nil {
				return fmt.Errorf("clone hint audio: %w", err)
			}
		}
	}

	return tx.Commit()
}

// generateDuplicateSlug returns "<source>-<random6>". The random
// suffix is base32-lowercase from 4 bytes of crypto rand, giving
// ~32^6 ≈ 10^9 collision space — high enough that we don't bother
// probing for uniqueness before insert; if a collision somehow
// happens, the unique-index returns 409 to the caller.
func generateDuplicateSlug(source string) (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	suffix := strings.ToLower(strings.TrimRight(
		base32.StdEncoding.EncodeToString(b), "="))
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	return source + "-" + suffix, nil
}

// isUniqueViolation matches the Postgres error string for a
// constraint violation — pgx surfaces it as a wrapped error whose
// Error() contains "SQLSTATE 23505". Reflection-free check: cheaper
// than pulling in pgconn just for a single error-class branch.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// deref returns the pointed-at string or "" when the pointer is nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// DuplicatePage clones one puzzle inside an adventure (or one
// orphan library puzzle) and returns the new row's id + idx. The
// "Google Slides duplicate" UX — useful when an arranger has
// crafted one good landmark/trivia/photo puzzle and wants four
// more just like it.
//
// For attached pages the clone lands at sourceIdx + 1, with every
// later page's idx shifted up by 1 to make room. PostgreSQL's
// UNIQUE(adventure_id, idx) is non-deferrable here, so we go
// through a temporary +1_000_000 offset and pull back rather than
// risk a mid-statement collision.
//
// For orphan library puzzles (adventure_id IS NULL) idx is
// position-within-an-adventure-only, so the clone gets the same
// idx as the source — the partial unique index doesn't constrain
// NULL-adventure rows.
//
// Auth: requireEditPage. With the role tightening shipped in
// canAuthor, that means only admin / org_admin in the owning org
// can duplicate; org_member is rejected at the policy hop.
func (h *Handlers) DuplicatePage(w http.ResponseWriter, r *http.Request) {
	pageID, ok := pageIDFromURL(r)
	if !ok {
		return
	}
	if !h.requireEditPage(w, r, pageID) {
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "begin failed")
		return
	}
	defer tx.Rollback() //nolint:errcheck — Commit takes over on success

	var (
		advID     sql.NullInt64
		sourceIdx int
	)
	if err := tx.QueryRowContext(r.Context(),
		`SELECT adventure_id, idx FROM adventure_pages WHERE id = $1`, pageID).
		Scan(&advID, &sourceIdx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "page not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	newIdx := sourceIdx
	if advID.Valid {
		// Two-phase shift: push the would-be-conflicting rows out
		// to a high range first so the upcoming insert at
		// sourceIdx+1 doesn't collide, then pull them back down by
		// (offset - 1) so they end up at old_idx + 1.
		const offset = 1_000_000
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE adventure_pages SET idx = idx + $1
			WHERE adventure_id = $2 AND idx > $3
		`, offset, advID.Int64, sourceIdx); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "shift failed", err)
			return
		}
		newIdx = sourceIdx + 1
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept,
				lat, lng, radius_m, disabled, notes, org_id)
			SELECT adventure_id, $1, title, prompt, accept,
			       lat, lng, radius_m, disabled, notes, org_id
			FROM adventure_pages WHERE id = $2
		`, newIdx, pageID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "insert page", err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE adventure_pages SET idx = idx - $1
			WHERE adventure_id = $2 AND idx > $3
		`, offset-1, advID.Int64, offset+sourceIdx); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "unshift failed", err)
			return
		}
	} else {
		// Orphan library puzzle — same idx is fine; the partial
		// unique index only fires when adventure_id is non-NULL.
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept,
				lat, lng, radius_m, disabled, notes, org_id)
			SELECT adventure_id, idx, title, prompt, accept,
			       lat, lng, radius_m, disabled, notes, org_id
			FROM adventure_pages WHERE id = $1
		`, pageID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "insert page", err)
			return
		}
	}

	// Locate the new row by (adventure_id, idx) for attached pages,
	// or by being the most-recent orphan with the same content for
	// library puzzles. Cleaner than RETURNING because the SELECT-
	// based INSERT above doesn't expose the new id without an
	// explicit RETURNING clause; we run the lookup separately to
	// keep the INSERT body symmetrical with the other clone paths.
	var newPageID int64
	if advID.Valid {
		if err := tx.QueryRowContext(r.Context(), `
			SELECT id FROM adventure_pages WHERE adventure_id = $1 AND idx = $2
		`, advID.Int64, newIdx).Scan(&newPageID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "locate clone", err)
			return
		}
	} else {
		// MAX(id) is safe because we just inserted the row inside
		// this same transaction; no other writer can interleave.
		if err := tx.QueryRowContext(r.Context(),
			`SELECT MAX(id) FROM adventure_pages WHERE adventure_id IS NULL`).
			Scan(&newPageID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "locate clone", err)
			return
		}
	}

	// Per-page assets.
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content, alt_text)
		SELECT $1, idx, mime_type, size_bytes, content, alt_text
		FROM adventure_page_images WHERE page_id = $2
	`, newPageID, pageID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "clone images", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO adventure_page_audio(page_id, idx, mime_type, size_bytes, content, description)
		SELECT $1, idx, mime_type, size_bytes, content, description
		FROM adventure_page_audio WHERE page_id = $2
	`, newPageID, pageID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "clone audio", err)
		return
	}

	// Hints + their media. Same id-mapping approach as
	// cloneAdventure: load source hint ids, insert each, copy the
	// hint's images and audio.
	hintRows, err := tx.QueryContext(r.Context(),
		`SELECT id FROM adventure_page_hints WHERE page_id = $1 ORDER BY idx`, pageID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "load hints", err)
		return
	}
	var oldHintIDs []int64
	for hintRows.Next() {
		var hid int64
		if err := hintRows.Scan(&hid); err != nil {
			hintRows.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "scan hint", err)
			return
		}
		oldHintIDs = append(oldHintIDs, hid)
	}
	hintRows.Close()
	if err := hintRows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "iter hints", err)
		return
	}
	for _, oldHID := range oldHintIDs {
		var newHID int64
		if err := tx.QueryRowContext(r.Context(), `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			SELECT $1, idx, text, delay_seconds
			FROM adventure_page_hints WHERE id = $2
			RETURNING id
		`, newPageID, oldHID).Scan(&newHID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "clone hint", err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content, alt_text)
			SELECT $1, idx, mime_type, size_bytes, content, alt_text
			FROM adventure_page_hint_images WHERE hint_id = $2
		`, newHID, oldHID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "clone hint images", err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_page_hint_audio(hint_id, idx, mime_type, size_bytes, content, description)
			SELECT $1, idx, mime_type, size_bytes, content, description
			FROM adventure_page_hint_audio WHERE hint_id = $2
		`, newHID, oldHID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "clone hint audio", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id":  newPageID,
		"idx": newIdx,
	})
}
