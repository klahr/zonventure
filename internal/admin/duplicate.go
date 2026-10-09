package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
)

// DuplicateAdventure clones a source adventure into the caller's
// active organisation. The clone is independent: pages, hints,
// accept-lists, images, audio, and the cover avatar all copy across
// so an arranger editing the copy can't break the source for anyone
// else.
//
// Authorisation:
//
//   - Caller must be authenticated and have an active org.
//   - Caller must be org_admin in their active org — duplicating
//     creates a new authored row, which org_member is not allowed
//     to do (canAuthor on the *destination*).
//   - Caller must be able to see the source (staffCanSeeAdventure) —
//     a published catalog adventure, one owned by the caller's own
//     org, or one shared / assigned to it. Cloning another org's
//     private work is deliberately rejected.
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
			"duplicate requires an active organization")
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
	if !h.staffCanSeeAdventure(r.Context(), me, sourceOrg, h.adventurePublished(r.Context(), sourceSlug), sourceSlug) {
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
// accept-lists, images, audio, and the graph (published and draft,
// with its media) all carry across; the new
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
			slug, title, description, author, intro_text, outro_text, buyer_note,
			accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
			font, heading_font, corners, cards, card_outline, backdrop, look_style, brief,
			org_id, created_by_user_id, published, language,
			forked_from_slug,
			avatar_mime_type, avatar_size_bytes, avatar_content)
		SELECT
			$1, $2, description, author, intro_text, outro_text, buyer_note,
			accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
			font, heading_font, corners, cards, card_outline, backdrop, look_style, brief,
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

	if err := cloneGraph(ctx, tx, sourceAdvID, newAdvID, byUserID); err != nil {
		return err
	}
	if err := clonePicture(ctx, tx, sourceAdvID, newAdvID, byUserID); err != nil {
		return err
	}

	return tx.Commit()
}

// clonePicture copies the media of the source's own look — its picture,
// its sounds and its cover — and points the copy's look at its own copies: the
// graph's media are copied with the graph, but the look is not in it.
func clonePicture(ctx context.Context, tx *sql.Tx, sourceAdvID, newAdvID, byUserID int64) error {
	var picture, right, wrong, cover int64
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(CASE WHEN backdrop->>'kind' = 'picture' THEN (backdrop->>'media')::bigint END, 0),
		       COALESCE((look_style->>'sound_right')::bigint, 0),
		       COALESCE((look_style->>'sound_wrong')::bigint, 0),
		       COALESCE((look_style->>'cover')::bigint, 0)
		FROM adventures WHERE id = $1
	`, sourceAdvID).Scan(&picture, &right, &wrong, &cover)
	if err != nil {
		return fmt.Errorf("load look media: %w", err)
	}
	var ids []int64
	for _, id := range []int64{picture, right, wrong, cover} {
		if id > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	idMap, err := copyGraphMedia(ctx, tx, sourceAdvID, newAdvID, byUserID, ids)
	if err != nil {
		return err
	}
	for _, u := range []struct {
		old         int64
		column, key string
	}{{picture, "backdrop", "media"}, {right, "look_style", "sound_right"}, {wrong, "look_style", "sound_wrong"}, {cover, "look_style", "cover"}} {
		if u.old == 0 {
			continue
		}
		q := fmt.Sprintf(`UPDATE adventures SET %[1]s = jsonb_set(%[1]s, '{%[2]s}', to_jsonb($1::bigint)) WHERE id = $2`, u.column, u.key)
		if _, err := tx.ExecContext(ctx, q, idMap[u.old], newAdvID); err != nil {
			return fmt.Errorf("point look media: %w", err)
		}
	}
	return nil
}

// cloneGraph copies the source's published graph and draft, with the
// media they refer to, into the new adventure. The copy's draft is the
// source's draft, or its published graph when it has none; a published
// source is published for the copy too.
func cloneGraph(ctx context.Context, tx *sql.Tx, sourceAdvID, newAdvID, byUserID int64) error {
	var published, draft *graph.Graph
	label := ""
	pub, err := graphstore.Published(ctx, tx, sourceAdvID)
	switch {
	case err == nil:
		published, label = pub.Graph, pub.Label
	case !errors.Is(err, graphstore.ErrNotFound):
		return fmt.Errorf("load published graph: %w", err)
	}
	d, err := graphstore.LoadDraft(ctx, tx, sourceAdvID)
	switch {
	case err == nil:
		draft = d.Graph
	case !errors.Is(err, graphstore.ErrNotFound):
		return fmt.Errorf("load draft graph: %w", err)
	}
	if published == nil && draft == nil {
		return nil
	}

	var ids []int64
	for _, g := range []*graph.Graph{published, draft} {
		if g != nil {
			ids = append(ids, g.MediaIDs()...)
		}
	}
	idMap, err := copyGraphMedia(ctx, tx, sourceAdvID, newAdvID, byUserID, ids)
	if err != nil {
		return err
	}
	if draft == nil {
		// Decoded fresh by Published; nothing else holds it.
		draft = published
	}
	if published != nil {
		remapGraphMedia(published, idMap)
	}
	if draft != published {
		remapGraphMedia(draft, idMap)
	}

	if _, _, err := graphstore.SaveDraft(ctx, tx, newAdvID, byUserID, draft); err != nil {
		return fmt.Errorf("save draft graph: %w", err)
	}
	if published != nil {
		// A release the validator has since learned to refuse stays
		// behind: the copy keeps it as its draft for the author to fix.
		var invalid *graphstore.InvalidError
		if _, _, err := graphstore.Publish(ctx, tx, newAdvID, byUserID, published, label); err != nil && !errors.As(err, &invalid) {
			return fmt.Errorf("publish graph: %w", err)
		}
	}
	return nil
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
