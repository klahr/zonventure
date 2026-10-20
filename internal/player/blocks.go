package player

// The blocks whose view depends on the player asking, not only on the
// team — a split secret's part, a vote's own choice — and the photo a
// team takes for a photo block. The session engine knows teams, not
// players, so these live beside it: the view it builds is finished here,
// per slot, before it leaves the server.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/imageutil"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// Photo size limits: what a phone may send, and the longest side kept.
// Larger than an avatar's, so an organiser can tell who is in it.
const (
	maxPhotoBytes = 12 << 20
	photoMaxDim   = 1600
)

// teamPlayers are the ids of the team's players who count as playing:
// see play.Players.
func teamPlayers(ctx context.Context, db *sql.DB, teamID, me int64) ([]int64, error) {
	return play.Players(ctx, db, teamID, me)
}

// decorateView finishes a view for the player asking: who holds each
// role, their part of a split secret, a vote's tally and their own vote,
// and the team's latest photo for a photo block.
func decorateView(ctx context.Context, db *sql.DB, teamID, slotID int64, v *play.View) error {
	v.HidePlaces()
	if err := decorateRoles(ctx, db, teamID, slotID, v); err != nil {
		return err
	}
	if err := decorateItems(ctx, db, teamID, v); err != nil {
		return err
	}
	var split, vote, photo bool
	for _, b := range v.Blocks {
		if b.Locked {
			continue
		}
		split = split || b.Kind == graph.BlockSplit
		vote = vote || b.Kind == graph.BlockVote || b.Kind == graph.BlockAccuse
		photo = photo || b.Kind == graph.BlockPhoto
	}
	if !split && !vote && !photo {
		return nil
	}
	v.Blocks = slices.Clone(v.Blocks)
	var players []int64
	if split || vote {
		var err error
		if players, err = teamPlayers(ctx, db, teamID, slotID); err != nil {
			return err
		}
	}
	for i := range v.Blocks {
		b := &v.Blocks[i]
		if b.Locked {
			continue
		}
		switch b.Kind {
		case graph.BlockSplit:
			me := slices.Index(players, slotID)
			b.Parts = session.SplitParts(b.Parts, me, len(players))
			b.Player, b.Players = me+1, len(players)
		case graph.BlockVote:
			tally, mine, err := voteTally(ctx, db, teamID, v.Node, b.ID, slotID, len(b.Options))
			if err != nil {
				return err
			}
			b.Tally, b.Voted, b.Voters = tally, mine, len(players)
		case graph.BlockAccuse:
			if err := decorateAccuse(ctx, db, teamID, slotID, v.Node, b, players); err != nil {
				return err
			}
		case graph.BlockPhoto:
			p, err := latestPhoto(ctx, db, teamID, v.Node, b.ID)
			if err != nil {
				return err
			}
			b.Photo = p
		}
	}
	return nil
}

func voteTally(ctx context.Context, db *sql.DB, teamID int64, node, block string, slotID int64, options int) ([]int, string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT slot_id, choice FROM team_block_votes WHERE team_id = $1 AND node = $2 AND block_id = $3`,
		teamID, node, block)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	tally := make([]int, options)
	mine := ""
	for rows.Next() {
		var slot int64
		var choice string
		if err := rows.Scan(&slot, &choice); err != nil {
			return nil, "", err
		}
		if i, err := strconv.Atoi(choice); err == nil && i >= 0 && i < options {
			tally[i]++
			if slot == slotID {
				mine = choice
			}
		}
	}
	return tally, mine, rows.Err()
}

func latestPhoto(ctx context.Context, db *sql.DB, teamID int64, node, block string) (*session.PhotoView, error) {
	var p session.PhotoView
	err := db.QueryRowContext(ctx, `
		SELECT id, status FROM team_photos WHERE team_id = $1 AND node = $2 AND block_id = $3
		ORDER BY id DESC LIMIT 1`, teamID, node, block).Scan(&p.ID, &p.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// shownBlock finds a block of kind on the team's current step, as the
// team sees it now: not on another step, not hidden, not switched off.
// node is the step the player is looking at; one the team has left is
// stale, as for every other action.
func (h *Handlers) shownBlock(ctx context.Context, teamID int64, node, block string, kinds ...graph.BlockKind) (session.BlockView, error) {
	v, err := h.engine().Snapshot(ctx, teamID)
	if err != nil {
		return session.BlockView{}, err
	}
	if v.Node != node {
		return session.BlockView{}, session.ErrStale
	}
	for _, b := range v.Blocks {
		if b.ID == block && slices.Contains(kinds, b.Kind) && !b.Locked {
			return b, nil
		}
	}
	return session.BlockView{}, session.ErrWrongNode
}

// vote records the player's vote on a vote block — an option's index —
// or whom they name on an accusation — a teammate's slot, never their
// own. They may change it while the team is on the step.
func (h *Handlers) vote(ctx context.Context, tc teamContext, slotID int64, req PlayActionRequest) error {
	b, err := h.shownBlock(ctx, tc.TeamID, req.Node, req.Block, graph.BlockVote, graph.BlockAccuse)
	if err != nil {
		return err
	}
	choice := strings.TrimSpace(req.Choice)
	if b.Kind == graph.BlockAccuse {
		named, err := strconv.ParseInt(choice, 10, 64)
		if err != nil || named == slotID {
			return session.ErrWrongNode
		}
		players, err := teamPlayers(ctx, h.DB, tc.TeamID, slotID)
		if err != nil {
			return err
		}
		if !slices.Contains(players, named) {
			return session.ErrWrongNode
		}
	} else if i, err := strconv.Atoi(choice); err != nil || i < 0 || i >= len(b.Options) {
		return session.ErrWrongNode
	}
	_, err = h.DB.ExecContext(ctx, `
		INSERT INTO team_block_votes (team_id, node, block_id, slot_id, choice) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (team_id, node, block_id, slot_id) DO UPDATE SET choice = EXCLUDED.choice, voted_at = now()`,
		tc.TeamID, req.Node, req.Block, slotID, choice)
	return err
}

// decorateAccuse lists whom the player may name — their teammates who
// are playing — with how many have named each, and whom they named.
func decorateAccuse(ctx context.Context, db *sql.DB, teamID, slotID int64, node string, b *session.BlockView, players []int64) error {
	names, err := slotNames(ctx, db, teamID)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT slot_id, choice FROM team_block_votes WHERE team_id = $1 AND node = $2 AND block_id = $3`,
		teamID, node, b.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var voter int64
		var choice string
		if err := rows.Scan(&voter, &choice); err != nil {
			return err
		}
		if slices.Contains(players, voter) {
			counts[choice]++
		}
		if voter == slotID {
			b.Voted = choice
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range players {
		if p == slotID {
			continue
		}
		b.Suspects = append(b.Suspects, session.Suspect{Slot: p, Name: names[p]})
		b.Tally = append(b.Tally, counts[strconv.FormatInt(p, 10)])
	}
	b.Voters = len(players)
	return nil
}

// photoReply is the body of POST /api/player/play/photo.
type photoReply struct {
	Photo session.PhotoView `json:"photo"`
}

// PostPlayPhoto takes the team's picture for a photo block, to wait for
// an organiser's review. A pending picture is replaced by a new one; a
// denied one is kept, and another may be taken; after an approved one
// the challenge is done.
//
// POST /api/player/play/photo (multipart: node, block, file)
func (h *Handlers) PostPlayPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slot, _ := auth.SlotFromContext(ctx)
	if ok, retry := h.rateLimiter().AllowAnswer(slot.ID, slot.Dev); !ok {
		writeRateLimited(w, retry)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes+1<<20)
	if err := r.ParseMultipartForm(maxPhotoBytes); err != nil {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, playRefusal{Error: "the picture is too large", Code: "too_long"})
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "file field required")
		return
	}
	defer f.Close()
	if !strings.HasPrefix(hdr.Header.Get("Content-Type"), "image/") {
		httpx.Error(w, http.StatusBadRequest, "only pictures are accepted")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes+1))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read failed")
		return
	}
	if len(data) > maxPhotoBytes {
		httpx.JSON(w, http.StatusRequestEntityTooLarge, playRefusal{Error: "the picture is too large", Code: "too_long"})
		return
	}
	tc, err := loadTeam(ctx, h.DB, slot)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
		return
	}
	if tc.RunState != "running" {
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "the event is not running", Code: "not_running"})
		return
	}
	node, block := r.FormValue("node"), r.FormValue("block")
	b, err := h.shownBlock(ctx, tc.TeamID, node, block, graph.BlockPhoto)
	if err != nil {
		h.refusePlay(w, play.Result{}, err)
		return
	}
	shrunk, mime, err := imageutil.Shrink(data, photoMaxDim)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable picture: "+err.Error())
		return
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "begin failed", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	// Serialise a team's uploads for one block, so two phones cannot
	// both slip past the approved check.
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM teams WHERE id = $1 FOR UPDATE`, tc.TeamID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lock failed", err)
		return
	}
	var approved bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM team_photos WHERE team_id = $1 AND node = $2 AND block_id = $3 AND status = 'approved')`,
		tc.TeamID, node, block).Scan(&approved); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reading photos failed", err)
		return
	}
	if approved {
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "this picture has already been approved", Code: "not_allowed"})
		return
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM team_photos WHERE team_id = $1 AND node = $2 AND block_id = $3 AND status = 'pending'`,
		tc.TeamID, node, block); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "replacing photo failed", err)
		return
	}
	out := photoReply{Photo: session.PhotoView{Status: "pending"}}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO team_photos (team_id, slot_id, event_id, node, block_id, content, mime, worth)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		tc.TeamID, slot.ID, tc.EventID, node, block, shrunk, mime, b.Worth).Scan(&out.Photo.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "storing photo failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "commit failed", err)
		return
	}
	teamID := tc.TeamID
	activity.Record(ctx, h.DB, tc.EventID, &teamID, activity.SlotActor(slot.ID), activity.PhotoTaken,
		map[string]any{"node": node, "block": block, "photo": out.Photo.ID})
	h.Hub.Publish(ws.EventTopic(tc.EventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, out)
}

// GetPlayPhoto serves a picture the team took, to its own players.
//
// GET /api/player/play/photo/{photoID}
func (h *Handlers) GetPlayPhoto(w http.ResponseWriter, r *http.Request) {
	slot, _ := auth.SlotFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "photoID"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "no such photo")
		return
	}
	var content []byte
	var mime string
	err = h.DB.QueryRowContext(r.Context(), `
		SELECT content, mime FROM team_photos WHERE id = $1 AND team_id = $2`, id, slot.TeamID).Scan(&content, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such photo")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reading photo failed", err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}
