package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/httpx"
	"git.sr.ht/~klahr/zv/internal/mdx"
	"github.com/go-chi/chi/v5"
)

// previewStateDTO mirrors player.State so play.html's render() path works
// unchanged on a preview session. Adds `preview: true` plus an optional
// `preview_debug` block (when ?debug=1) listing the author-side fields
// the player never sees — accepted answers, gate coordinates, notes.
type previewStateDTO struct {
	Status         string            `json:"status"`
	AdventureSlug  string            `json:"adventure_slug,omitempty"`
	AdventureTitle string            `json:"adventure_title,omitempty"`
	TeamName       string            `json:"team_name,omitempty"`
	PageIndex      int               `json:"page_index"`
	PageCount      int               `json:"page_count"`
	Title          string            `json:"title,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	Hints          []previewHintDTO  `json:"hints,omitempty"`
	Images         []previewImageDTO `json:"images,omitempty"`
	// Audio mirrors player.State.Audio so play.html's audio
	// renderer (which only fires when s.audio is an array) paints
	// in preview just like a live run. Without this, audio attached
	// to a puzzle is invisible everywhere except the editor's own
	// per-clip strip — uploaded but unreachable.
	Audio          []previewAudioDTO `json:"audio,omitempty"`
	Colors         *adventure.Colors `json:"colors,omitempty"`
	Preview        bool              `json:"preview"`
	// SpoilerFree marks a buyer / share-holder preview where the
	// accept list and notes have been stripped out. The play-side
	// reads this to swap the answer dock for a "Next puzzle"
	// walkthrough button — typing an answer would be useless when
	// there's no client-side accept list to validate against, and
	// silently treating every input as correct would be misleading.
	SpoilerFree bool `json:"spoiler_free,omitempty"`
	// Accept is the accepted-answer list authors test against in
	// preview. Stripped for spoiler-free previews — buyers who walk
	// through an adventure they bought shouldn't be handed the
	// answer key alongside the prompt.
	Accept   []adventure.AcceptEntry `json:"accept,omitempty"`
	NoAnswer bool             `json:"no_answer,omitempty"`
	Gate     *gatePreviewDTO  `json:"gate,omitempty"`
	Debug    *previewDebugDTO `json:"preview_debug,omitempty"`
	// OrgLogoURL + OrgName mirror the player.State fields so the
	// preview's brand strip matches what real participants see. The
	// preview uses the caller's active org rather than the
	// adventure's owning org — for admin-owned global adventures the
	// caller's org is what gets stamped on a real run anyway.
	OrgLogoURL string `json:"org_logo_url,omitempty"`
	OrgName    string `json:"org_name,omitempty"`
}

// gatePreviewDTO mirrors player.gateDTO so the player render() path
// treats a preview state the same as a real session for purposes of
// rendering the gated submit dock. `reached` is always false in
// preview — the arranger drives the local gate check via the debug
// strip's "Set test location" affordance instead.
type gatePreviewDTO struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM float64 `json:"radius_m"`
	Reached bool    `json:"reached"`
}

// previewHintDTO mirrors player.hintDTO. In preview mode the delay
// gate is deliberately bypassed (unlocks_in_seconds is always 0) —
// the author's goal here is to read their own hint copy, not to
// validate the timer itself. If they want to test delay behaviour
// end-to-end, they do it in a real run.
//
// Images is the same shape as player.hintDTO.Images so play.html's
// hint-sheet renderer paints unchanged in preview. The server-side
// reveal-gate doesn't apply in preview (the route is admin-only),
// but the URL still points at /adventure-assets/hint-image/{id} so
// any author who copies a preview link gets a working image — staff
// sessions are always allowed by canViewHintImage.
type previewHintDTO struct {
	TextHTML         string            `json:"text_html"`
	UnlocksInSeconds int               `json:"unlocks_in_seconds"`
	Images           []previewImageDTO `json:"images,omitempty"`
	Audio            []previewAudioDTO `json:"audio,omitempty"`
}

type previewImageDTO struct {
	URL string `json:"url"`
	Alt string `json:"alt,omitempty"`
}

// previewAudioDTO mirrors player.audioDTO so play.html's audio
// renderer treats a preview clip the same as a live one. URL points
// at the same /adventure-assets/audio/{id} (or /hint-audio/{id})
// endpoint a real player would hit — preview is admin-only, so the
// reveal-gate the live route enforces is moot here.
type previewAudioDTO struct {
	URL         string `json:"url"`
	MimeType    string `json:"mime_type"`
	Description string `json:"description,omitempty"`
}

type previewDebugDTO struct {
	Accept  []adventure.AcceptEntry `json:"accept"`
	Lat     float64                 `json:"lat"`
	Lng     float64                 `json:"lng"`
	RadiusM *float64                `json:"radius_m,omitempty"`
	Notes   string                  `json:"notes,omitempty"`
}

// PreviewAdventure returns one puzzle of an adventure in the shape the
// player page expects, so arrangers can QA an adventure without having
// to spin up a real run or walk to a gate. The puzzle is selected via
// ?page=<N> (0-indexed over the enabled pages), defaulting to 0.
//
// Gating is intentionally skipped: the preview always renders the
// prompt even for gated puzzles, so the arranger can read through the
// content end-to-end. The debug panel (?debug=1) exposes the gate
// coordinates + radius so they know what a player would need to reach.
func (h *Handlers) PreviewAdventure(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	adv, err := h.Lib.GetContext(r.Context(), slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if adv == nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return
	}
	// Access tiers — author / buyer / share-holder / nothing.
	// Authors get the full preview (accept list, debug panel) for
	// QA; buyers and share-holders get a spoiler-free walkthrough
	// (no accept list, no debug); anyone else 403s. This closes the
	// content-leak path where a curious arranger from any org could
	// previously hit /preview on a published adventure and read off
	// every accepted answer in a loop.
	access, accessErr := h.previewAccess(r.Context(), slug)
	if accessErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "access lookup failed")
		return
	}
	if access == previewAccessNone {
		httpx.Error(w, http.StatusForbidden,
			"preview not available — authors get full preview, buyers and share-holders get a spoiler-free walkthrough")
		return
	}
	spoilerFree := access != previewAccessAuthor
	// Mirror the player flow: disabled pages are invisible.
	pages := make([]adventure.Page, 0, len(adv.Pages))
	for _, p := range adv.Pages {
		if !p.Disabled {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		httpx.Error(w, http.StatusBadRequest, "adventure has no enabled puzzles")
		return
	}
	idx := 0
	if q := r.URL.Query().Get("page"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 || n >= len(pages) {
			httpx.Error(w, http.StatusBadRequest, "page out of range")
			return
		}
		idx = n
	}
	page := pages[idx]
	out := previewStateDTO{
		Status:         "running",
		AdventureSlug:  adv.Slug,
		AdventureTitle: adv.Meta.Title,
		PageIndex:      idx,
		PageCount:      len(pages),
		Title:          page.Title,
		// Match the player flow: run the prose through goldmark on
		// egress so the preview reads exactly like /play will. The raw
		// markdown stays in the debug panel (accept/notes) for authors
		// who need to see it verbatim.
		Prompt:      mdx.Render(page.Prompt),
		NoAnswer:    len(page.Accept) == 0,
		Preview:     true,
		SpoilerFree: spoilerFree,
	}
	// Authors get the accept list for type-and-check QA; buyers
	// don't (the field omits via `omitempty` when nil).
	if !spoilerFree {
		out.Accept = page.Accept
	}
	if !adv.Colors.Empty() {
		c := adv.Colors
		out.Colors = &c
	}
	if len(page.Images) > 0 {
		imgs := make([]previewImageDTO, 0, len(page.Images))
		for _, im := range page.Images {
			imgs = append(imgs, previewImageDTO{
				URL: "/adventure-assets/image/" + strconv.FormatInt(im.ID, 10),
				Alt: im.AltText,
			})
		}
		out.Images = imgs
	}
	if len(page.Audio) > 0 {
		audio := make([]previewAudioDTO, 0, len(page.Audio))
		for _, a := range page.Audio {
			audio = append(audio, previewAudioDTO{
				URL:         "/adventure-assets/audio/" + strconv.FormatInt(a.ID, 10),
				MimeType:    a.MimeType,
				Description: a.Description,
			})
		}
		out.Audio = audio
	}
	if len(page.Hints) > 0 {
		hints := make([]previewHintDTO, 0, len(page.Hints))
		for _, h := range page.Hints {
			ph := previewHintDTO{
				TextHTML:         mdx.Render(h.Text),
				UnlocksInSeconds: 0,
			}
			if len(h.Images) > 0 {
				imgs := make([]previewImageDTO, 0, len(h.Images))
				for _, im := range h.Images {
					imgs = append(imgs, previewImageDTO{
						URL: "/adventure-assets/hint-image/" + strconv.FormatInt(im.ID, 10),
						Alt: im.AltText,
					})
				}
				ph.Images = imgs
			}
			if len(h.Audio) > 0 {
				audio := make([]previewAudioDTO, 0, len(h.Audio))
				for _, a := range h.Audio {
					audio = append(audio, previewAudioDTO{
						URL:         "/adventure-assets/hint-audio/" + strconv.FormatInt(a.ID, 10),
						MimeType:    a.MimeType,
						Description: a.Description,
					})
				}
				ph.Audio = audio
			}
			hints = append(hints, ph)
		}
		out.Hints = hints
	}
	// Mirror the play-page brand strip in preview. Pulled off the
	// caller's active org so an arranger previewing a global-catalog
	// (admin-owned) adventure still sees their own brand mark — same
	// thing that would happen on a real run their org schedules.
	if me, ok := auth.FromContext(r.Context()); ok && me.OrgID != nil {
		var (
			orgName string
			hasLogo int
		)
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT name, (logo_content IS NOT NULL AND length(logo_content) > 0)::int
			FROM organizations WHERE id = $1
		`, *me.OrgID).Scan(&orgName, &hasLogo); err == nil {
			out.OrgName = orgName
			if hasLogo != 0 {
				out.OrgLogoURL = fmt.Sprintf("/org-logo/%d", *me.OrgID)
			}
		}
	}
	// Gate echoes the page's geofence so the player dock renders the
	// "I'm here" button in preview when the puzzle is no-answer gated.
	// Always reached=false: in preview the arranger drives the gate
	// check locally (debug-strip "Set test location") rather than
	// hitting the server, so the dock starts in the same state a real
	// player would see on arrival.
	if page.Location.RadiusM != nil && *page.Location.RadiusM > 0 {
		out.Gate = &gatePreviewDTO{
			Lat:     page.Location.Lat,
			Lng:     page.Location.Lng,
			RadiusM: *page.Location.RadiusM,
			Reached: false,
		}
	}
	// Debug panel is author-only — exposes the accept list, raw
	// gate coords, and the author's private notes. Hidden in
	// spoiler-free previews even when ?debug=1 is present, so a
	// buyer can't toggle their way to the answer key.
	if !spoilerFree && r.URL.Query().Get("debug") == "1" {
		out.Debug = &previewDebugDTO{
			Accept:  page.Accept,
			Lat:     page.Location.Lat,
			Lng:     page.Location.Lng,
			RadiusM: page.Location.RadiusM,
			Notes:   page.Notes,
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// previewAccessLevel is the answer to "what kind of preview can
// this caller see for this adventure" — picked once at the top of
// PreviewAdventure and used to gate the response shape.
type previewAccessLevel int

const (
	// previewAccessNone — caller has no relationship to the adventure.
	// Returns 403 from PreviewAdventure.
	previewAccessNone previewAccessLevel = iota
	// previewAccessAuthor — caller can edit the adventure (owning-org
	// author or global admin via canAuthor). Full preview, accept
	// list included, debug panel honoured.
	previewAccessAuthor
	// previewAccessLicensed — caller's org holds an assignment for or
	// share of the adventure but doesn't own it. Spoiler-free preview:
	// no accept list, no debug, no answer dock — just the prompt,
	// hints, media, and gate coordinates so they can plan their
	// event without being handed the answer key.
	previewAccessLicensed
)

// previewAccess looks up which preview level the calling session is
// entitled to for the given adventure slug. Order matters: an author
// who *also* holds an assignment (e.g. an admin who bought slots in
// a different org's adventure) gets author-level preview because
// canAuthor passes — strictly more useful than the buyer flavour.
func (h *Handlers) previewAccess(ctx context.Context, slug string) (previewAccessLevel, error) {
	me, _ := auth.FromContext(ctx)
	var orgID sql.NullInt64
	err := h.DB.QueryRowContext(ctx,
		`SELECT org_id FROM adventures WHERE slug = $1`, slug).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return previewAccessNone, nil
	}
	if err != nil {
		return previewAccessNone, err
	}
	if canAuthor(me, orgID) {
		return previewAccessAuthor, nil
	}
	// Below this line we need an org context to look up assignments
	// and shares — admins fall through canAuthor above, so anyone
	// without an OrgID here is a non-admin without org membership
	// (shouldn't happen via the routed surface, but bail safely).
	if me.OrgID == nil {
		return previewAccessNone, nil
	}
	var n int
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM adventure_assignments
		WHERE adventure_slug = $1 AND org_id = $2
	`, slug, *me.OrgID).Scan(&n); err != nil {
		return previewAccessNone, err
	}
	if n > 0 {
		return previewAccessLicensed, nil
	}
	if err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM adventure_shares
		WHERE adventure_slug = $1 AND org_id = $2
	`, slug, *me.OrgID).Scan(&n); err != nil {
		return previewAccessNone, err
	}
	if n > 0 {
		return previewAccessLicensed, nil
	}
	return previewAccessNone, nil
}
