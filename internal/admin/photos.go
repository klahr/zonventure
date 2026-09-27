package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// TeamPhoto is a picture a team took for a photo block, as the
// organisers review it.
type TeamPhoto struct {
	ID         int64      `json:"id"`
	TeamID     int64      `json:"team_id"`
	TeamName   string     `json:"team_name"`
	Node       string     `json:"node"`
	NodeTitle  string     `json:"node_title,omitempty"`
	Block      string     `json:"block"`
	Prompt     string     `json:"prompt,omitempty"`
	Worth      int        `json:"worth"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

func (h *Handlers) engine() *play.Engine {
	if h.Engine != nil {
		return h.Engine
	}
	return play.New(h.DB)
}

// ListEventPhotos lists the pictures the event's teams took, those
// waiting for review first, then newest first. Challenges is how many
// photo blocks the event's graph has, so the page can say there are
// none yet rather than hide the list.
//
// GET /api/admin/events/{eventID}/photos
func (h *Handlers) ListEventPhotos(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	g, err := h.engine().EventGraph(ctx, h.DB, eventID)
	if err != nil && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading graph failed", err)
		return
	}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT p.id, p.team_id, t.name, p.node, p.block_id, p.worth, p.status, p.created_at, p.reviewed_at
		FROM team_photos p JOIN teams t ON t.id = p.team_id
		WHERE p.event_id = $1
		ORDER BY p.status = 'pending' DESC, p.id DESC`, eventID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading photos failed", err)
		return
	}
	defer rows.Close()
	photos := []TeamPhoto{}
	for rows.Next() {
		var p TeamPhoto
		var reviewed sql.NullTime
		if err := rows.Scan(&p.ID, &p.TeamID, &p.TeamName, &p.Node, &p.Block, &p.Worth, &p.Status, &p.CreatedAt, &reviewed); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading photos failed", err)
			return
		}
		if reviewed.Valid {
			p.ReviewedAt = &reviewed.Time
		}
		if g != nil {
			if n, ok := g.Node(p.Node); ok {
				p.NodeTitle = n.Title
				if i := slices.IndexFunc(n.AllBlocks(), func(b graph.Block) bool { return b.ID == p.Block }); i >= 0 {
					p.Prompt = n.AllBlocks()[i].Text
				}
			}
		}
		photos = append(photos, p)
	}
	if err := rows.Err(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading photos failed", err)
		return
	}
	challenges := 0
	if g != nil {
		for _, n := range g.Nodes {
			for _, b := range n.Blocks() {
				if b.Kind == graph.BlockPhoto {
					challenges++
				}
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"photos": photos, "challenges": challenges})
}

func photoID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "bad photo id")
		return 0, false
	}
	return id, true
}

// EventPhotoImage serves one of the event's team pictures.
//
// GET /api/admin/events/{eventID}/photos/{photoID}/image
func (h *Handlers) EventPhotoImage(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	id, ok := photoID(w, r)
	if !ok {
		return
	}
	var content []byte
	var mime string
	err := h.DB.QueryRowContext(r.Context(), `SELECT content, mime FROM team_photos WHERE id = $1 AND event_id = $2`, id, eventID).
		Scan(&content, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "photo not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading photo failed", err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

// ReviewEventPhoto approves or denies a team's picture, or sets it back
// to waiting. An approved picture adds its block's points to the team's
// score, and any other verdict takes back what it had added, so a
// changed mind costs nothing. A team has one approved picture per block
// at most; a denied one lets it take another.
//
// POST /api/admin/events/{eventID}/photos/{photoID}/review {"status": "approved"|"denied"|"pending"}
func (h *Handlers) ReviewEventPhoto(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.messagesEvent(w, r)
	if !ok {
		return
	}
	id, ok := photoID(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Status != "approved" && req.Status != "denied" && req.Status != "pending" {
		httpx.Error(w, http.StatusBadRequest, "status must be approved, denied or pending")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "begin failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		teamID               int64
		node, block, current string
		worth, awarded       int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT team_id, node, block_id, worth, awarded, status FROM team_photos
		WHERE id = $1 AND event_id = $2 FOR UPDATE`, id, eventID).Scan(&teamID, &node, &block, &worth, &awarded, &current)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "photo not found")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading photo failed", err)
		return
	}
	if req.Status == "approved" && current != "approved" {
		var other bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM team_photos
			               WHERE team_id = $1 AND node = $2 AND block_id = $3 AND status = 'approved' AND id <> $4)`,
			teamID, node, block, id).Scan(&other); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading photos failed", err)
			return
		}
		if other {
			httpx.Error(w, http.StatusConflict, "the team already has an approved photo for this challenge")
			return
		}
	}
	award := 0
	if req.Status == "approved" {
		award = worth
	}
	var reviewer any
	var actor activity.Actor
	if me, ok := auth.FromContext(ctx); ok {
		reviewer, actor = me.ID, activity.UserActor(me.ID)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE team_photos SET status = $1, awarded = $2, reviewed_at = now(), reviewed_by = $3 WHERE id = $4`,
		req.Status, award, reviewer, id); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reviewing photo failed", err)
		return
	}
	delta := award - awarded
	if err := play.AdjustScore(ctx, tx, teamID, float64(delta)); err != nil && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "adjusting score failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
		return
	}
	if current != req.Status {
		activity.Record(ctx, h.DB, eventID, &teamID, actor, activity.PhotoReviewed,
			map[string]any{"node": node, "block": block, "photo": id, "status": req.Status, "points": delta})
	}
	if h.Hub != nil {
		h.Hub.Publish(ws.EventTopic(eventID), "changed", nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status, "points": delta})
}
