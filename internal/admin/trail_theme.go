package admin

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/httpx"
)

// A party adventure's look: one of the starter themes (play-ui's
// STARTER_THEMES), picked in the creator. The look is saved as the
// adventure's own colours, fonts, backdrop and style — validated as any
// adventure's are (applyAdventureMetaUpdate) — and the theme's id kept
// to show it chosen. The id is the creator's; the server only bounds it.

var trailThemeID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// trailTransition is how every step of a party adventure comes in,
// whatever its theme: its cards turn like pages. The creator offers no
// other.
const trailTransition = "flip"

// trailLook is a whole look, as play.Brand writes it.
type trailLook struct {
	Accent          string              `json:"accent"`
	AccentSecondary string              `json:"accent_secondary"`
	Background      string              `json:"background"`
	Text            string              `json:"text"`
	SubmitBg        string              `json:"submit_bg"`
	Font            string              `json:"font"`
	HeadingFont     string              `json:"heading_font"`
	Corners         string              `json:"corners"`
	Cards           string              `json:"cards"`
	CardOutline     bool                `json:"card_outline"`
	Backdrop        *adventure.Backdrop `json:"backdrop"`
	Style           *adventure.Style    `json:"style"`
}

type trailThemeRequest struct {
	ID   string    `json:"id"`
	Look trailLook `json:"look"`
}

// applyTrailTheme gives the adventure slug the look in req, writing the
// error when it can't. A picture backdrop is refused: a party adventure
// has no pictures of its own to show.
func (h *Handlers) applyTrailTheme(w http.ResponseWriter, ctx context.Context, slug string, req trailThemeRequest) bool {
	if !trailThemeID.MatchString(req.ID) {
		httpx.Error(w, http.StatusBadRequest, "no such theme")
		return false
	}
	l := req.Look
	backdrop := l.Backdrop
	if backdrop == nil {
		backdrop = &adventure.Backdrop{}
	}
	if backdrop.Kind == "picture" {
		httpx.Error(w, http.StatusBadRequest, "a party adventure's backdrop can't be a picture")
		return false
	}
	style := l.Style
	if style == nil {
		style = &adventure.Style{}
	}
	style.Transition = trailTransition
	// A cover picture of the adventure's own outlasts a change of theme:
	// it is shown in place of the theme's.
	if style.Cover == 0 {
		cover, _, err := h.adventureCover(ctx, slug, false)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "theme failed", err)
			return false
		}
		style.Cover = cover
	}
	_, err := applyAdventureMetaUpdate(ctx, h.DB, slug, updateAdventureRequest{
		AccentColor: &l.Accent, AccentSecondaryColor: &l.AccentSecondary, BackgroundColor: &l.Background,
		TextColor: &l.Text, SubmitBgColor: &l.SubmitBg, Font: &l.Font, HeadingFont: &l.HeadingFont,
		Corners: &l.Corners, Cards: &l.Cards, CardOutline: &l.CardOutline, Backdrop: backdrop, Style: style,
	})
	var ie *importError
	if errors.As(err, &ie) {
		httpx.Error(w, ie.status, ie.Error())
		return false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "theme failed", err)
		return false
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE adventures SET trail_theme = $2 WHERE slug = $1`, slug, req.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "theme failed", err)
		return false
	}
	return true
}

// PutTrailTheme sets a party adventure's look, from the console.
//
// PUT /api/admin/adventures/{slug}/cards/theme
func (h *Handlers) PutTrailTheme(w http.ResponseWriter, r *http.Request) {
	var req trailThemeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	t, ok := h.trailFor(w, r)
	if !ok {
		return
	}
	if h.applyTrailTheme(w, r.Context(), t.dto.Slug, req) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// PutTrailDraftTheme sets a public party adventure's look: by its draft
// secret, or by the host link of the event it plays as.
//
// PUT /api/trail/drafts/{id}/theme
// PUT /api/party/{token}/cards/theme
func (h *Handlers) PutTrailDraftTheme(w http.ResponseWriter, r *http.Request) {
	var req trailThemeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	var (
		p  publicTrail
		ok bool
	)
	if chi.URLParam(r, "token") != "" {
		p, ok = h.hostTrail(w, r)
	} else {
		p, ok = h.draftFor(w, r)
	}
	if !ok {
		return
	}
	if p.State() == "ended" {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": "this party adventure has ended", "code": "ended"})
		return
	}
	if !allowTrailSave(p.ID) {
		w.Header().Set("Retry-After", "60")
		httpx.Error(w, http.StatusTooManyRequests, "saving too often; try again in a minute")
		return
	}
	if h.applyTrailTheme(w, r.Context(), p.Slug, req) {
		w.WriteHeader(http.StatusNoContent)
	}
}
