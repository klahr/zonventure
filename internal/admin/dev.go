package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// Dev tickets: an author plays the adventure's draft on a phone, on site,
// while it is edited — in the designer, or by an assistant through the
// MCP — and sees every change at once, with nothing published and no
// event restarted. Each author has one dev event per adventure, playing
// the adventure's dev row (graphstore.EnsureDev), with one team: theirs.

type devTicket struct {
	EventID     int64  `json:"event_id"`
	TeamID      int64  `json:"team_id"`
	TeamURL     string `json:"team_url"`
	PlayURL     string `json:"play_url"`
	VoucherCode string `json:"voucher_code"`
}

// CreateDevTicket serves POST /api/admin/adventures/{slug}/dev-ticket:
// the caller's dev ticket for the adventure, made the first time and the
// same one after — its link opens the draft on a phone.
func (h *Handlers) CreateDevTicket(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	slug := chi.URLParam(r, "slug")
	me, _ := auth.FromContext(r.Context())
	publicURL := strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
	if publicURL == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "dev tickets need ZV_PUBLIC_URL for their links")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dev ticket failed", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// The dev row starts as the draft — or the release, before any draft.
	var g *graph.Graph
	if d, err := graphstore.LoadDraft(ctx, tx, advID); err == nil {
		g = d.Graph
	} else if !errors.Is(err, graphstore.ErrNotFound) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the draft failed", err)
		return
	} else if p, err := graphstore.Published(ctx, tx, advID); err == nil {
		g = p.Graph
	} else {
		httpx.Error(w, http.StatusConflict, "the adventure has no graph yet: build its first step in the designer")
		return
	}
	devGraph, err := graphstore.EnsureDev(ctx, tx, advID, g)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dev graph failed", err)
		return
	}

	var eventID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE dev AND adventure_slug = $1 AND created_by_user_id = $2`, slug, me.ID).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		liveToken, terr := newLiveToken()
		if terr != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "token gen failed", terr)
			return
		}
		err = tx.QueryRowContext(ctx, `
			INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, started_at, live_token, name, attested_no_minors_at, graph_id, dev)
			SELECT $1, COALESCE(a.org_id, $2), $3, 'running', now_text(), $4, $5, now_text(), $6, TRUE
			FROM adventures a WHERE a.id = $7
			RETURNING id
		`, slug, orgOrZero(me.OrgID), me.ID, liveToken, "Dev · "+me.Username, devGraph, advID).Scan(&eventID)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dev event failed", err)
		return
	}
	var t devTicket
	t.EventID = eventID
	var voucher string
	err = tx.QueryRowContext(ctx, `SELECT id, token, COALESCE(voucher_code, '') FROM teams WHERE event_id = $1 ORDER BY id LIMIT 1`, eventID).Scan(&t.TeamID, &t.TeamURL, &voucher)
	if errors.Is(err, sql.ErrNoRows) {
		t.TeamID, t.TeamURL, voucher, err = createTicketTeam(ctx, tx, eventID, me.Username, 1)
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dev team failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dev ticket failed", err)
		return
	}
	// Placed on its first step, or at the start screen, like any team.
	if err := h.Engine.StartEvent(ctx, eventID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "placing the dev team failed", err)
		return
	}
	t.TeamURL = publicURL + "/team/" + t.TeamURL
	if voucher != "" {
		t.PlayURL = publicURL + "/play?c=" + voucher
		t.VoucherCode = auth.FormatVoucherCode(voucher)
	}
	httpx.JSON(w, http.StatusOK, t)
}

func orgOrZero(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

// SendDevTicket serves POST /api/admin/adventures/{slug}/dev-ticket/send
// {email}: the caller's dev ticket, sent to any address — to open on a
// phone, the author's or a colleague's — as a bought ticket's link is.
func (h *Handlers) SendDevTicket(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.devTeam(w, r)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	email, err := normaliseTeamEmail(req.Email)
	if err != nil || email == "" {
		httpx.Error(w, http.StatusBadRequest, "a valid email address is required")
		return
	}
	var token, voucher string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT token, COALESCE(voucher_code, '') FROM teams WHERE id = $1`, teamID).Scan(&token, &voucher); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	if err := h.sendTicketTeamLink(r.Context(), eventID, email, token, voucher, "", nil); err != nil {
		httpx.ServerErr(w, http.StatusBadGateway, "the email could not be sent", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// devTeam finds the caller's dev team for the adventure in the URL.
func (h *Handlers) devTeam(w http.ResponseWriter, r *http.Request) (teamID, eventID int64, ok bool) {
	if _, ok := h.graphTarget(w, r); !ok {
		return 0, 0, false
	}
	me, _ := auth.FromContext(r.Context())
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.id, ev.id FROM events ev JOIN teams t ON t.event_id = ev.id
		WHERE ev.dev AND ev.adventure_slug = $1 AND ev.created_by_user_id = $2
		ORDER BY t.id LIMIT 1
	`, chi.URLParam(r, "slug"), me.ID).Scan(&teamID, &eventID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no dev ticket for this adventure yet: make one first")
		return 0, 0, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return 0, 0, false
	}
	return teamID, eventID, true
}

// DevScreen serves GET /api/admin/adventures/{slug}/dev-screen: what the
// caller's dev phone shows now — the step and its view, as the phone has it.
func (h *Handlers) DevScreen(w http.ResponseWriter, r *http.Request) {
	teamID, _, ok := h.devTeam(w, r)
	if !ok {
		return
	}
	v, err := h.Engine.Snapshot(r.Context(), teamID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reading the dev screen failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"view": v})
}

// DevGoTo serves POST /api/admin/adventures/{slug}/dev-goto {node}: the
// dev phone moves to that step — back or on — and plays it afresh.
func (h *Handlers) DevGoTo(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.devTeam(w, r)
	if !ok {
		return
	}
	var req struct {
		Node string `json:"node"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	h.devMove(w, r.Context(), eventID, func() error { return h.Engine.DevMoveTo(r.Context(), teamID, req.Node) })
}

// DevReset serves POST /api/admin/adventures/{slug}/dev-reset: the dev
// phone starts the adventure over.
func (h *Handlers) DevReset(w http.ResponseWriter, r *http.Request) {
	teamID, eventID, ok := h.devTeam(w, r)
	if !ok {
		return
	}
	h.devMove(w, r.Context(), eventID, func() error { return h.Engine.DevReset(r.Context(), teamID) })
}

func (h *Handlers) devMove(w http.ResponseWriter, ctx context.Context, eventID int64, move func() error) {
	switch err := move(); {
	case errors.Is(err, play.ErrNoSuchStep):
		httpx.Error(w, http.StatusBadRequest, "no such step to show: name a puzzle, story, hub or end of the draft by its slug")
		return
	case errors.Is(err, play.ErrNoGraph):
		httpx.Error(w, http.StatusConflict, "the dev team has not been placed yet")
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "dev move failed", err)
		return
	}
	h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}

// pokeDevTeams tells the adventure's dev phones to look again, after its
// draft — which they play — was saved.
func (h *Handlers) pokeDevTeams(ctx context.Context, advID int64) {
	if h.Hub == nil {
		return
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT ev.id FROM events ev JOIN adventures a ON a.slug = ev.adventure_slug
		WHERE ev.dev AND a.id = $1
	`, advID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			h.Hub.Publish(ws.EventTopic(id), "changed", nil)
		}
	}
}

// AdventureChanged tells the adventure's dev phones to look again, after
// a change made outside these handlers — by the MCP.
func (h *Handlers) AdventureChanged(ctx context.Context, advID int64) {
	h.pokeDevTeams(ctx, advID)
}
