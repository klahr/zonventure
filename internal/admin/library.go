package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/mdx"
)

/* ---------------- Puzzle library ----------------
 *
 * Every puzzle (adventure_pages row) is listed here, whether it's
 * currently attached to an adventure or floating in the library with a
 * NULL adventure_id. The library is the canonical "all puzzles" view;
 * adventures become one of many places a puzzle *could* appear.
 *
 * Adding a library puzzle to an adventure is a deep COPY (including
 * images), so edits in the adventure-side copy don't mutate the library
 * original. This matches the authoring flow: the library is a pool of
 * templates or one-off captures; adventures assemble their own frozen
 * versions.
 */

// libraryPuzzleDTO is one row in GET /api/admin/puzzles. The adventure
// fields are populated only when this puzzle is attached, so library
// puzzles ship with zero-values there (and the client can distinguish
// "assigned to 'demo'" from "in library" by nil/empty slug).
type libraryPuzzleDTO struct {
	ID             int64    `json:"id"`
	Title          string   `json:"title"`
	Prompt         string   `json:"prompt"`
	PromptHTML     string   `json:"prompt_html,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Accept         []string `json:"accept"`
	Lat            float64  `json:"lat"`
	Lng            float64  `json:"lng"`
	RadiusM        *float64 `json:"radius_m,omitempty"`
	Gated          bool     `json:"gated"`
	Disabled       bool     `json:"disabled"`
	HasImages      bool     `json:"has_images"`
	AdventureSlug  string   `json:"adventure_slug,omitempty"`
	AdventureTitle string   `json:"adventure_title,omitempty"`
	AdventureIdx   *int     `json:"adventure_idx,omitempty"`
}

// ListPuzzles returns every puzzle in the system (scoped to the
// caller's visibility) with a light adventure assignment annotation.
// Admins see everything; org users see admin-owned library stubs
// plus anything owned by their org.
func (h *Handlers) ListPuzzles(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.FromContext(r.Context())
	query := `
		SELECT p.id, p.title, p.prompt, p.notes, p.accept,
		       p.lat, p.lng, p.radius_m, p.disabled,
		       (EXISTS(SELECT 1 FROM adventure_page_images WHERE page_id = p.id))::int,
		       p.adventure_id, COALESCE(a.slug, ''), COALESCE(a.title, ''), p.idx
		FROM adventure_pages p
		LEFT JOIN adventures a ON a.id = p.adventure_id
	`
	var args []any
	if !isAdmin(me) {
		// Visibility rule: attached pages ride the adventure's org;
		// orphan pages ride their own org. NULL on either side means
		// admin-owned (global).
		query += `
			WHERE (
			    (p.adventure_id IS NOT NULL AND (a.org_id IS NULL OR a.org_id = $1))
			 OR (p.adventure_id IS NULL     AND (p.org_id IS NULL OR p.org_id = $2))
			)
		`
		args = append(args, *me.OrgID, *me.OrgID)
	}
	query += `
		ORDER BY
		    CASE WHEN p.adventure_id IS NULL THEN 0 ELSE 1 END,
		    a.slug, p.idx, p.id DESC
	`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "list failed", err)
		return
	}
	defer rows.Close()
	out := []libraryPuzzleDTO{}
	for rows.Next() {
		var (
			p         libraryPuzzleDTO
			acceptJS  string
			radius    sql.NullFloat64
			disabled  int
			advID     sql.NullInt64
			advIdx    int
			hasImages int
		)
		if err := rows.Scan(&p.ID, &p.Title, &p.Prompt, &p.Notes, &acceptJS,
			&p.Lat, &p.Lng, &radius, &disabled, &hasImages,
			&advID, &p.AdventureSlug, &p.AdventureTitle, &advIdx); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "scan failed", err)
			return
		}
		// The listing DTO uses []string (regex flag isn't surfaced on
		// the card view). Parse via the tolerant helper so both legacy
		// and modern shapes load, then flatten to text-only.
		if parsed, perr := adventure.ParseAcceptJSON([]byte(acceptJS)); perr == nil {
			p.Accept = make([]string, len(parsed))
			for i, e := range parsed {
				p.Accept[i] = e.Text
			}
		}
		if p.Accept == nil {
			p.Accept = []string{}
		}
		if radius.Valid {
			v := radius.Float64
			p.RadiusM = &v
			p.Gated = v > 0
		}
		p.Disabled = disabled != 0
		p.HasImages = hasImages != 0
		// Pre-render markdown so the library grid can drop the HTML
		// straight in without one /markdown/preview request per card.
		p.PromptHTML = mdx.Render(p.Prompt)
		if advID.Valid {
			i := advIdx
			p.AdventureIdx = &i
		} else {
			p.AdventureSlug = ""
			p.AdventureTitle = ""
		}
		out = append(out, p)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"puzzles": out})
}

// createLibraryPuzzleRequest is the body of POST /api/admin/puzzles.
// Every field is optional except the location — the typical flow is a
// phone tap that captures GPS and opens the editor to fill in the rest.
type createLibraryPuzzleRequest struct {
	Title   string   `json:"title"`
	Prompt  string   `json:"prompt"`
	Notes   string   `json:"notes"`
	Accept  []string `json:"accept"`
	Lat     float64  `json:"lat"`
	Lng     float64  `json:"lng"`
	RadiusM *float64 `json:"radius_m,omitempty"`
}

// CreateLibraryPuzzle inserts an orphan puzzle with NULL adventure_id
// so it shows up in the library without belonging to any adventure.
// Returns the newly-created page id so the client can open the editor.
func (h *Handlers) CreateLibraryPuzzle(w http.ResponseWriter, r *http.Request) {
	var req createLibraryPuzzleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		httpx.Error(w, http.StatusBadRequest, "coordinates out of range")
		return
	}
	if req.RadiusM != nil && *req.RadiusM <= 0 {
		httpx.Error(w, http.StatusBadRequest, "radius_m must be > 0")
		return
	}
	acceptJS, _ := json.Marshal(req.Accept)
	if req.Accept == nil {
		acceptJS = []byte(`[]`)
	}
	var radius any
	if req.RadiusM != nil {
		radius = *req.RadiusM
	}
	// idx=0 for library rows is fine — the UNIQUE(adventure_id, idx)
	// constraint treats each NULL adventure_id as distinct, so many
	// library rows may share idx=0 without conflict. org_id stamps
	// ownership: admins create into the global library (NULL), org
	// users create into their own org's library.
	me, _ := auth.FromContext(r.Context())
	orgArg := any(nil)
	if org := callerOrgID(me); org.Valid {
		orgArg = org.Int64
	}
	var id int64
	if err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept,
		                             lat, lng, radius_m, notes, org_id)
		VALUES (NULL, 0, $1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, req.Title, req.Prompt, string(acceptJS), req.Lat, req.Lng, radius, req.Notes, orgArg).Scan(&id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "insert failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

// puzzleDetailDTO is one-puzzle detail as returned by GET
// /api/admin/puzzles/{id}. Shape matches pageView with the addition of
// the adventure assignment so the library/editor can display it.
type puzzleDetailDTO struct {
	pageView
	AdventureSlug  string `json:"adventure_slug,omitempty"`
	AdventureTitle string `json:"adventure_title,omitempty"`
}

// GetPuzzle returns a single puzzle — attached or library — with its
// full image list and adventure assignment metadata. Used by the
// deep-link editor entry point from the Puzzles view.
func (h *Handlers) GetPuzzle(w http.ResponseWriter, r *http.Request) {
	pageID, err := strconv.ParseInt(chi.URLParam(r, "pageID"), 10, 64)
	if err != nil || pageID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad page id")
		return
	}
	var (
		p        puzzleDetailDTO
		acceptJS string
		radius   sql.NullFloat64
		disabled int
		advID    sql.NullInt64
		advSlug  sql.NullString
		advTitle sql.NullString
		advOrg   sql.NullInt64
		pageOrg  sql.NullInt64
	)
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT p.id, p.idx, p.title, p.prompt, p.notes, p.accept,
		       p.lat, p.lng, p.radius_m, p.disabled,
		       p.adventure_id, a.slug, a.title, a.org_id, p.org_id
		FROM adventure_pages p
		LEFT JOIN adventures a ON a.id = p.adventure_id
		WHERE p.id = $1
	`, pageID).Scan(&p.ID, &p.Index, &p.Title, &p.Prompt, &p.Notes, &acceptJS,
		&p.Lat, &p.Lng, &radius, &disabled, &advID, &advSlug, &advTitle, &advOrg, &pageOrg)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "puzzle not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	// Visibility check: attached → adventure's org; orphan → page's own.
	me, _ := auth.FromContext(r.Context())
	owner := pageOrg
	if advID.Valid {
		owner = advOrg
	}
	if !canSeeOwned(me, owner) {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}
	// Tolerate legacy `[]string` accept rows (pre "Add regex answers"
	// commit) so a re-opened old puzzle keeps its answers in the
	// editor instead of looking blank.
	if parsed, perr := adventure.ParseAcceptJSON([]byte(acceptJS)); perr == nil {
		p.Accept = parsed
	}
	if p.Accept == nil {
		p.Accept = []adventure.AcceptEntry{}
	}
	if radius.Valid {
		v := radius.Float64
		p.RadiusM = &v
		p.Gated = v > 0
	}
	p.Disabled = disabled != 0
	p.PromptHTML = mdx.Render(p.Prompt)
	p.AdventureSlug = advSlug.String
	p.AdventureTitle = advTitle.String
	imgs, err := loadPageImages(r.Context(), h.DB, pageID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "images failed", err)
		return
	}
	p.Images = imgs
	hints, err := loadPageHints(r.Context(), h.DB, pageID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "hints failed", err)
		return
	}
	p.Hints = hints
	httpx.JSON(w, http.StatusOK, p)
}

// CopyPuzzleToAdventure deep-copies an existing puzzle (including
// images) into the target adventure at the next idx. The source row is
// untouched — this is how you pull a library puzzle into an adventure,
// and it also works for duplicating a puzzle across adventures.
//
// Permissions: caller must be able to see the source and edit the
// target. A user can read admin-owned puzzles, so they can copy
// public-catalog puzzles into their own adventures.
func (h *Handlers) CopyPuzzleToAdventure(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	srcID, err := strconv.ParseInt(chi.URLParam(r, "pageID"), 10, 64)
	if err != nil || srcID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad page id")
		return
	}
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return
	}
	// Check source visibility. Load its owning org (attached → adventure,
	// orphan → own org_id).
	var (
		srcAdvID   sql.NullInt64
		srcAdvOrg  sql.NullInt64
		srcPageOrg sql.NullInt64
	)
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT p.adventure_id, a.org_id, p.org_id
		FROM adventure_pages p
		LEFT JOIN adventures a ON a.id = p.adventure_id
		WHERE p.id = $1
	`, srcID).Scan(&srcAdvID, &srcAdvOrg, &srcPageOrg); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "source puzzle not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "source lookup failed")
		return
	}
	me, _ := auth.FromContext(r.Context())
	srcOwner := srcPageOrg
	if srcAdvID.Valid {
		srcOwner = srcAdvOrg
	}
	if !canSeeOwned(me, srcOwner) {
		httpx.Error(w, http.StatusForbidden, "cannot see that puzzle")
		return
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback()

	// Read the source row.
	var (
		title, prompt, notes, accept string
		lat, lng                     float64
		radius                       sql.NullFloat64
		disabled                     int
	)
	if err := tx.QueryRowContext(r.Context(), `
		SELECT title, prompt, notes, accept, lat, lng, radius_m, disabled
		FROM adventure_pages WHERE id = $1
	`, srcID).Scan(&title, &prompt, &notes, &accept, &lat, &lng, &radius, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "source puzzle not found")
			return
		}
		httpx.ServerErr(w, http.StatusInternalServerError, "source lookup failed", err)
		return
	}

	// Pick the next idx in the target adventure.
	var nextIdx int
	if err := tx.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(idx)+1, 0) FROM adventure_pages WHERE adventure_id = $1`, advID).
		Scan(&nextIdx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "next idx lookup failed")
		return
	}
	var radiusArg any
	if radius.Valid {
		radiusArg = radius.Float64
	}
	var newID int64
	if err := tx.QueryRowContext(r.Context(), `
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, notes, accept,
		                             lat, lng, radius_m, disabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, advID, nextIdx, title, prompt, notes, accept, lat, lng, radiusArg, disabled).Scan(&newID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "insert copy failed", err)
		return
	}

	// Copy every image blob verbatim. The images table references the
	// page, so a straight INSERT ... SELECT does the deep copy.
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content, alt_text)
		SELECT $1, idx, mime_type, size_bytes, content, alt_text
		FROM adventure_page_images WHERE page_id = $2
	`, newID, srcID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "image copy failed", err)
		return
	}
	// Same blob-for-blob copy for audio attachments.
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO adventure_page_audio(page_id, idx, mime_type, size_bytes, content, description)
		SELECT $1, idx, mime_type, size_bytes, content, description
		FROM adventure_page_audio WHERE page_id = $2
	`, newID, srcID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "audio copy failed", err)
		return
	}
	// Hints + per-hint images: insert each source hint into the new
	// page with RETURNING, then copy its images blob-for-blob into
	// the new hint. INSERT...SELECT works for the hint row itself,
	// but we need the freshly-minted destination hint id paired with
	// the source hint id to remap the images FK, so we walk the rows
	// in Go. N is small (≤10 hints in practice) and this whole tx
	// already touches the same connection — the extra round-trips
	// are noise next to the blob copy itself.
	srcHints, err := tx.QueryContext(r.Context(), `
		SELECT id, idx, text, delay_seconds FROM adventure_page_hints
		WHERE page_id = $1 ORDER BY idx
	`, srcID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "hints read failed", err)
		return
	}
	type srcHintRow struct {
		id           int64
		idx          int
		text         string
		delaySeconds int
	}
	var hintRows []srcHintRow
	for srcHints.Next() {
		var row srcHintRow
		if err := srcHints.Scan(&row.id, &row.idx, &row.text, &row.delaySeconds); err != nil {
			srcHints.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "hints read failed", err)
			return
		}
		hintRows = append(hintRows, row)
	}
	if err := srcHints.Err(); err != nil {
		srcHints.Close()
		httpx.ServerErr(w, http.StatusInternalServerError, "hints read failed", err)
		return
	}
	srcHints.Close()
	for _, row := range hintRows {
		var newHintID int64
		if err := tx.QueryRowContext(r.Context(), `
			INSERT INTO adventure_page_hints(page_id, idx, text, delay_seconds)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, newID, row.idx, row.text, row.delaySeconds).Scan(&newHintID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "hint copy failed", err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_page_hint_images(hint_id, idx, mime_type, size_bytes, content, alt_text)
			SELECT $1, idx, mime_type, size_bytes, content, alt_text
			FROM adventure_page_hint_images WHERE hint_id = $2
		`, newHintID, row.id); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "hint image copy failed", err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO adventure_page_hint_audio(hint_id, idx, mime_type, size_bytes, content, description)
			SELECT $1, idx, mime_type, size_bytes, content, description
			FROM adventure_page_hint_audio WHERE hint_id = $2
		`, newHintID, row.id); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "hint audio copy failed", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": newID, "index": nextIdx})
}
