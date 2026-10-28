package admin

// Graph authoring endpoints, with dazyflow's draft-and-release model
// (see internal/graphstore).
//
//	GET   /api/admin/adventures/{slug}/graph                      the draft, and what is published
//	PUT   /api/admin/adventures/{slug}/graph                      autosave the draft (errors allowed)
//	POST  /api/admin/adventures/{slug}/graph/publish              release the draft {label}
//	GET   /api/admin/adventures/{slug}/graph/versions             version history
//	GET   /api/admin/adventures/{slug}/graph/versions/{version}   one version, to preview or restore
//	POST  /api/admin/adventures/{slug}/graph/versions/{version}/publish   roll back to it
//	PATCH /api/admin/adventures/{slug}/graph/versions/{version}   name it {label}
//	POST  /api/admin/adventures/{slug}/graph/validate             check without saving
//
// All four use requireEditAdventure: only someone who can edit the
// adventure may see its graph, because the graph carries every answer.
//
// Responses carry the validator's full report rather than the first
// error, so the editor can mark every problem on the canvas at once.
// Refusals use a stable "code" alongside the English "error", which is
// what a client should branch on.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/ws"
)

// maxGraphBodyBytes bounds a graph upload. Matches graph.ParseEnvelope's
// own cap, applied here too so an oversized body is refused before it is
// read into memory.
const maxGraphBodyBytes = 8 << 20

type graphResponse struct {
	Version  int            `json:"version,omitempty"`
	Envelope graph.Envelope `json:"envelope"`
	Report   valid.Report   `json:"report"`
	// Published describes the live version; nil when nothing is.
	Published *publishedInfo `json:"published,omitempty"`
	// Dirty is set when the draft differs from what is published in
	// anything a team would notice — dazyflow's "unpublished changes".
	Dirty bool `json:"dirty,omitempty"`
	// SavedAt is when the draft was last saved.
	SavedAt *time.Time `json:"saved_at,omitempty"`
}

type publishedInfo struct {
	Version int    `json:"version"`
	Label   string `json:"label,omitempty"`
}

type graphRefusal struct {
	Error  string        `json:"error"`
	Code   string        `json:"code"`
	Report *valid.Report `json:"report,omitempty"`
}

// GetAdventureGraph returns the draft — or, when there is none yet, the
// published version as the starting draft — with a fresh validation
// report and the release state.
func (h *Handlers) GetAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	resp, found, err := h.graphState(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	if !found {
		httpx.JSON(w, http.StatusNotFound, graphRefusal{
			Error: "this adventure has no graph yet", Code: "graph_not_found",
		})
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// graphState assembles the draft-and-release view of an adventure.
func (h *Handlers) graphState(ctx context.Context, advID int64) (graphResponse, bool, error) {
	var resp graphResponse
	pub, err := graphstore.Published(ctx, h.DB, advID)
	if err != nil && !errors.Is(err, graphstore.ErrNotFound) {
		return resp, false, err
	}
	if err == nil {
		resp.Published = &publishedInfo{Version: pub.Version, Label: pub.Label}
	}
	draft, err := graphstore.LoadDraft(ctx, h.DB, advID)
	switch {
	case err == nil:
		resp.Envelope = graph.Envelope{Format: graph.FormatV2, Graph: *draft.Graph}
		resp.SavedAt = &draft.UpdatedAt
		resp.Dirty = resp.Published == nil || graphstore.Differs(draft.Graph, pub.Graph)
	case errors.Is(err, graphstore.ErrNotFound):
		if resp.Published == nil {
			return resp, false, nil
		}
		resp.Envelope = graph.Envelope{Format: graph.FormatV2, Graph: *pub.Graph}
	default:
		return resp, false, err
	}
	resp.Report = h.withSight(ctx, advID, &resp.Envelope.Graph, valid.Check(&resp.Envelope.Graph))
	return resp, true, nil
}

// PutAdventureGraph autosaves the draft. Errors do not block — the
// report says what they are — because an author mid-edit always has
// some. Only media from another adventure is refused.
func (h *Handlers) PutAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	env, ok := readEnvelope(w, r)
	if !ok {
		return
	}
	// X-Draft-Base is the saved_at the caller loaded: with it, a save
	// that would overwrite someone else's later one is refused.
	var base *time.Time
	if b := r.Header.Get("X-Draft-Base"); b != "" {
		t, err := time.Parse(time.RFC3339Nano, b)
		if err != nil {
			httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: "X-Draft-Base must be a saved_at", Code: "graph_malformed"})
			return
		}
		base = &t
	}
	// An assistant is shown working on what it changed, so read what
	// it is changing from. Without a draft, everything is new.
	var before *graph.Graph
	if auth.ViaAPIToken(r.Context()) {
		if was, err := graphstore.LoadDraft(r.Context(), h.DB, advID); err == nil {
			before = was.Graph
		}
	}
	// Removing a language deletes all its texts at once: like the other
	// steps that are hard to take back, it takes the password.
	langs, err := graphstore.DraftLanguages(r.Context(), h.DB, advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading draft failed", err)
		return
	}
	for _, l := range langs {
		if _, kept := env.Graph.Translations[l]; !kept {
			if !h.requirePasswordConfirm(w, r) {
				return
			}
			break
		}
	}
	kept := h.keptOver(r, advID, &env.Graph, base)
	d, report, err := graphstore.SaveDraftIf(r.Context(), h.DB, advID, h.userID(r), &env.Graph, base)
	if errors.Is(err, graphstore.ErrDraftChanged) {
		httpx.JSON(w, http.StatusConflict, graphRefusal{Error: err.Error(), Code: "draft_changed"})
		return
	}
	if errors.Is(err, graphstore.ErrForeignMedia) {
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{Error: err.Error(), Code: "media_foreign"})
		return
	}
	if errors.Is(err, graphstore.ErrMediaKind) {
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{Error: err.Error(), Code: "media_kind"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving draft failed", err)
		return
	}
	h.publishGraphSaved(r, advID, d.UpdatedAt, before, &env.Graph, kept)
	if d.DevSynced {
		h.pokeDevTeams(r.Context(), advID)
	}
	// Answer from what was just saved and checked, rather than reading
	// the draft back and checking it again: the designer saves every
	// time the author pauses.
	resp := graphResponse{Envelope: *env, SavedAt: &d.UpdatedAt, Report: h.withSight(r.Context(), advID, &env.Graph, report)}
	pub, err := graphstore.Published(r.Context(), h.DB, advID)
	if err != nil && !errors.Is(err, graphstore.ErrNotFound) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	if err == nil {
		resp.Published = &publishedInfo{Version: pub.Version, Label: pub.Label}
	}
	resp.Dirty = resp.Published == nil || graphstore.Differs(&env.Graph, pub.Graph)
	httpx.JSON(w, http.StatusOK, resp)
}

// assistantPresence is how long an assistant stays shown on an adventure
// after its latest save.
const assistantPresence = 2 * time.Minute

// touchedSteps lists the steps of after that are new or differ from
// before, or whose wires do — new ones first, as what an assistant is
// working on is most likely what it just added.
func touchedSteps(before, after *graph.Graph) []string {
	was := map[string]string{}
	wires := map[string]string{}
	if before != nil {
		for _, n := range before.Nodes {
			b, _ := json.Marshal(n)
			was[n.Slug] = string(b)
		}
		for _, e := range before.Edges {
			wires[e.From] += e.FromPort + ">" + e.To + ";"
		}
	}
	now := map[string]string{}
	for _, e := range after.Edges {
		now[e.From] += e.FromPort + ">" + e.To + ";"
	}
	var added, changed []string
	for _, n := range after.Nodes {
		b, _ := json.Marshal(n)
		old, ok := was[n.Slug]
		switch {
		case !ok:
			added = append(added, n.Slug)
		case old != string(b) || wires[n.Slug] != now[n.Slug]:
			changed = append(changed, n.Slug)
		}
	}
	return append(added, changed...)
}

// keptSteps says whose edits a save overwrote on purpose: after an edit
// conflict the saver kept their own version of Steps over UserID's.
type keptSteps struct {
	UserID int64    `json:"user_id"`
	Steps  []string `json:"steps"`
}

// keptOver reads X-Kept-Steps — the saver's chosen steps, each URL
// encoded, comma separated — and finds whose save they overwrite: the
// draft's last saver, when the save is based on that draft. Steps not in
// g are dropped; nil when nobody else is overwritten.
func (h *Handlers) keptOver(r *http.Request, advID int64, g *graph.Graph, base *time.Time) *keptSteps {
	head := r.Header.Get("X-Kept-Steps")
	if head == "" || base == nil {
		return nil
	}
	var steps []string
	for _, s := range strings.Split(head, ",") {
		slug, err := url.PathUnescape(s)
		if _, ok := g.Node(slug); ok && err == nil && !slices.Contains(steps, slug) {
			steps = append(steps, slug)
		}
	}
	if len(steps) == 0 {
		return nil
	}
	var (
		by sql.NullInt64
		at time.Time
	)
	err := h.DB.QueryRowContext(r.Context(), `SELECT updated_by_user_id, updated_at FROM adventure_graph_drafts WHERE adventure_id = $1`, advID).Scan(&by, &at)
	if err != nil || !by.Valid || !at.Equal(*base) || by.Int64 == h.userID(r) {
		return nil
	}
	return &keptSteps{UserID: by.Int64, Steps: steps}
}

// publishGraphSaved tells designers open on the adventure that its
// draft changed, by whom, and whether through an API token — which is
// how an assistant edits. An assistant's save also makes it present on
// the adventure for a while, on the steps it touched, saying what it is
// doing when it said so (X-Edit-Note).
func (h *Handlers) publishGraphSaved(r *http.Request, advID int64, savedAt time.Time, before, after *graph.Graph, kept *keptSteps) {
	me, _ := auth.FromContext(r.Context())
	assistant := auth.ViaAPIToken(r.Context())
	msg := map[string]any{
		"saved_at":  savedAt,
		"by":        me.Username,
		"user_id":   me.ID,
		"assistant": assistant,
	}
	if kept != nil {
		msg["kept"] = kept
	}
	if assistant {
		note := strings.TrimSpace(r.Header.Get("X-Edit-Note"))
		if len([]rune(note)) > 200 {
			note = string([]rune(note)[:200])
		}
		touched := touchedSteps(before, after)
		msg["touched"], msg["note"] = touched, note
		peer := ws.Peer{ID: ws.AssistantPeerID(me.ID), UserID: me.ID, Name: me.Username, Kind: ws.PeerAssistant, Touched: touched, Note: note}
		if len(touched) > 0 {
			peer.Focus = touched[0]
		}
		h.Hub.Visit(ws.AdventureTopic(advID), peer, assistantPresence)
	}
	h.Hub.Publish(ws.AdventureTopic(advID), "graph_saved", msg)
}

type publishRequest struct {
	Label string `json:"label,omitempty"`
}

// PublishAdventureGraph releases the saved draft. It publishes what is
// stored, not the request body, so the release is exactly what the
// author last saw saved.
func (h *Handlers) PublishAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	var req publishRequest
	if r.ContentLength != 0 && !httpx.DecodeJSON(w, r, &req) {
		return
	}
	draft, err := graphstore.LoadDraft(r.Context(), h.DB, advID)
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.JSON(w, http.StatusConflict, graphRefusal{Error: "there is no draft to publish", Code: "draft_not_found"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading draft failed", err)
		return
	}
	_, report, err := graphstore.Publish(r.Context(), h.DB, advID, h.userID(r), draft.Graph, strings.TrimSpace(req.Label))
	if !h.publishRefused(w, report, err) {
		h.adoptMainTheme(r.Context(), advID, draft.Graph)
		h.publishGraphSaved(r, advID, draft.UpdatedAt, draft.Graph, draft.Graph, nil)
		h.writeGraphState(w, r, advID)
	}
}

// ListAdventureGraphVersions returns the version history, newest first.
func (h *Handlers) ListAdventureGraphVersions(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	vs, err := graphstore.Versions(r.Context(), h.DB, advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading history failed", err)
		return
	}
	if vs == nil {
		vs = []graphstore.Version{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"versions": vs})
}

// GetAdventureGraphVersion returns one version, for previewing it.
func (h *Handlers) GetAdventureGraphVersion(w http.ResponseWriter, r *http.Request) {
	advID, version, ok := h.versionTarget(w, r)
	if !ok {
		return
	}
	s, err := graphstore.ByVersion(r.Context(), h.DB, advID, version)
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.JSON(w, http.StatusNotFound, graphRefusal{Error: "no such version", Code: "version_not_found"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading version failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, graphResponse{
		Version:  s.Version,
		Envelope: graph.Envelope{Format: graph.FormatV2, Graph: *s.Graph},
		Report:   h.withSight(r.Context(), advID, s.Graph, valid.Check(s.Graph)),
	})
}

// PublishAdventureGraphVersion rolls back: an older version becomes the
// published one. The draft is left alone.
func (h *Handlers) PublishAdventureGraphVersion(w http.ResponseWriter, r *http.Request) {
	advID, version, ok := h.versionTarget(w, r)
	if !ok {
		return
	}
	var req publishRequest
	if r.ContentLength != 0 && !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if l := strings.TrimSpace(req.Label); l != "" {
		if err := graphstore.SetLabel(r.Context(), h.DB, advID, version, l); err != nil && !errors.Is(err, graphstore.ErrNotFound) {
			httpx.ServerErr(w, http.StatusInternalServerError, "naming version failed", err)
			return
		}
	}
	s, err := graphstore.PublishVersion(r.Context(), h.DB, advID, version)
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.JSON(w, http.StatusNotFound, graphRefusal{Error: "no such version", Code: "version_not_found"})
		return
	}
	var report valid.Report
	var invalid *graphstore.InvalidError
	if errors.As(err, &invalid) {
		report = invalid.Report
	}
	if !h.publishRefused(w, report, err) {
		h.adoptMainTheme(r.Context(), advID, s.Graph)
		h.writeGraphState(w, r, advID)
	}
}

// adoptMainTheme makes the main theme of g, just published, the
// adventure's look: the one the store, emails and the pages before play
// show, with its starter's cover picture. A new cover waits for review as when set by hand. A graph
// without a main theme leaves the look as it is.
func (h *Handlers) adoptMainTheme(ctx context.Context, advID int64, g *graph.Graph) {
	th := g.Theme(g.MainTheme)
	if th == nil {
		return
	}
	var slug string
	err := h.DB.QueryRowContext(ctx, `SELECT slug FROM adventures WHERE id = $1`, advID).Scan(&slug)
	if err == nil {
		_, err = applyAdventureMetaUpdate(ctx, h.DB, slug, lookUpdate(th.Look))
	}
	if err == nil {
		_, err = h.DB.ExecContext(ctx, `UPDATE adventures SET trail_theme = $2 WHERE id = $1`, advID, th.Starter)
	}
	if err != nil {
		logx.Warn("admin.graph.main_theme", "adventure_id", advID, "err", err.Error())
	}
}

// lookUpdate sets every field of the adventure's look to l's.
func lookUpdate(l adventure.Colors) updateAdventureRequest {
	backdrop, style := l.Backdrop, l.Style
	if backdrop == nil {
		backdrop = &adventure.Backdrop{}
	}
	if style == nil {
		style = &adventure.Style{}
	}
	return updateAdventureRequest{
		AccentColor: &l.Accent, AccentSecondaryColor: &l.AccentSecondary, BackgroundColor: &l.Background,
		TextColor: &l.Text, SubmitBgColor: &l.SubmitBackground, Font: &l.Font, HeadingFont: &l.HeadingFont,
		Corners: &l.Corners, Cards: &l.Cards, CardOutline: &l.CardOutline, Backdrop: backdrop, Style: style,
	}
}

// LabelAdventureGraphVersion names a version.
func (h *Handlers) LabelAdventureGraphVersion(w http.ResponseWriter, r *http.Request) {
	advID, version, ok := h.versionTarget(w, r)
	if !ok {
		return
	}
	var req publishRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	err := graphstore.SetLabel(r.Context(), h.DB, advID, version, strings.TrimSpace(req.Label))
	if errors.Is(err, graphstore.ErrNotFound) {
		httpx.JSON(w, http.StatusNotFound, graphRefusal{Error: "no such version", Code: "version_not_found"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "naming version failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

// publishRefused writes the refusal for a failed publish and reports
// whether it did.
func (h *Handlers) publishRefused(w http.ResponseWriter, report valid.Report, err error) bool {
	var invalid *graphstore.InvalidError
	switch {
	case err == nil:
		return false
	case errors.As(err, &invalid):
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{
			Error: "the adventure has errors and was not published", Code: "graph_invalid", Report: &report,
		})
	case errors.Is(err, graphstore.ErrForeignMedia):
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{Error: err.Error(), Code: "media_foreign"})
	case errors.Is(err, graphstore.ErrMediaKind):
		httpx.JSON(w, http.StatusUnprocessableEntity, graphRefusal{Error: err.Error(), Code: "media_kind"})
	default:
		httpx.ServerErr(w, http.StatusInternalServerError, "publishing failed", err)
	}
	return true
}

func (h *Handlers) writeGraphState(w http.ResponseWriter, r *http.Request, advID int64) {
	resp, _, err := h.graphState(r.Context(), advID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handlers) versionTarget(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return 0, 0, false
	}
	v, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || v <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad version")
		return 0, 0, false
	}
	return advID, v, true
}

func (h *Handlers) userID(r *http.Request) int64 {
	if me, ok := auth.FromContext(r.Context()); ok {
		return me.ID
	}
	return 0
}

// ValidateAdventureGraph checks a graph without saving it. This is what
// the editor calls as the author works, so problems appear on the canvas
// before they press save.
func (h *Handlers) ValidateAdventureGraph(w http.ResponseWriter, r *http.Request) {
	advID, ok := h.graphTarget(w, r)
	if !ok {
		return
	}
	env, ok := readEnvelope(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, graphResponse{Envelope: *env, Report: h.withSight(r.Context(), advID, &env.Graph, valid.Check(&env.Graph))})
}

// withSight adds valid.Sight's warnings to a report when the adventure
// says it can be played without sight (Facts.Blind), so the author sees
// what still stands in such a player's way. A failed read leaves the
// report as it was: these are only warnings.
func (h *Handlers) withSight(ctx context.Context, advID int64, g *graph.Graph, report valid.Report) valid.Report {
	var raw []byte
	var f adventure.Facts
	if err := h.DB.QueryRowContext(ctx, `SELECT facts FROM adventures WHERE id = $1`, advID).Scan(&raw); err != nil || json.Unmarshal(raw, &f) != nil || !f.Blind {
		return report
	}
	report.Issues = append(slices.Clip(report.Issues), valid.Sight(g)...)
	return report
}

// graphTarget authorises the caller against the adventure in the URL and
// returns its id.
func (h *Handlers) graphTarget(w http.ResponseWriter, r *http.Request) (int64, bool) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return 0, false
	}
	advID, err := adventureIDBySlug(r.Context(), h.DB, slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return 0, false
	}
	return advID, true
}

func readEnvelope(w http.ResponseWriter, r *http.Request) (*graph.Envelope, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGraphBodyBytes))
	if err != nil {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, graphRefusal{
			Error: "graph is too large", Code: "graph_too_large",
		})
		return nil, false
	}
	env, err := graph.ParseEnvelope(body)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, graphRefusal{Error: err.Error(), Code: "graph_malformed"})
		return nil, false
	}
	return env, true
}
