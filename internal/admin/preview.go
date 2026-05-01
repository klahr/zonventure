package admin

import (
	"net/http"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/adventure"
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
	Colors         *adventure.Colors `json:"colors,omitempty"`
	Preview        bool              `json:"preview"`
	// Accept is always included (the preview endpoint is admin-only,
	// so the author already owns this list) so the play-side client
	// can verify answers even when the debug panel is collapsed.
	// Without it, previewing-without-debug would treat every answer
	// as correct.
	Accept   []adventure.AcceptEntry `json:"accept"`
	NoAnswer bool             `json:"no_answer,omitempty"`
	Gate     *gatePreviewDTO  `json:"gate,omitempty"`
	Debug    *previewDebugDTO `json:"preview_debug,omitempty"`
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
}

type previewImageDTO struct {
	URL string `json:"url"`
	Alt string `json:"alt,omitempty"`
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
		Prompt:   mdx.Render(page.Prompt),
		Accept:   page.Accept,
		NoAnswer: len(page.Accept) == 0,
		Preview:  true,
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
			hints = append(hints, ph)
		}
		out.Hints = hints
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
	if r.URL.Query().Get("debug") == "1" {
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
