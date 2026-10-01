package player

// Handing a role's kit item to a teammate (graph.Role.Kit): the giver's
// phone shows a code, the teammate's phone scans it, and the item is
// theirs to use (session.Team.CanUse).
//
//	POST /api/player/items/give     {item}  → a code and its QR, for a few minutes
//	POST /api/player/items/receive  {code}  → the item is the scanner's

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/tokenx"
	"github.com/klahr/zonventure/internal/ws"
)

// giftTTL is how long a give-away code can be scanned: long enough to
// find the teammate, short enough that a photo of it is no use later.
const giftTTL = 5 * time.Minute

// giftPrefix marks a scanned code as a give-away, not a puzzle's answer.
const giftPrefix = "zv-gift:"

type giveRequest struct {
	Item string `json:"item"`
}

type giveResponse struct {
	Code      string    `json:"code"`
	QR        string    `json:"qr"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PostGiveItem makes a code that hands one of the player's kit items to
// whichever teammate scans it. A new code for the item replaces any
// earlier one.
func (h *Handlers) PostGiveItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req giveRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	it, err := h.carried(ctx, slot.TeamID, req.Item)
	if err != nil {
		h.itemErr(w, err)
		return
	}
	if !it.Give {
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "that item is not yours to give", Code: "not_yours"})
		return
	}
	code, err := tokenx.New(12)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "making code failed", err)
		return
	}
	png, err := qrcode.Encode(giftPrefix+code, qrcode.Medium, 320)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "drawing code failed", err)
		return
	}
	expires := time.Now().Add(giftTTL).UTC()
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO item_gifts (code, team_id, item, from_slot, expires_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (team_id, item) DO UPDATE SET code = EXCLUDED.code, from_slot = EXCLUDED.from_slot, expires_at = EXCLUDED.expires_at`,
		code, slot.TeamID, req.Item, slot.ID, expires); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "saving code failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, giveResponse{Code: code, QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), ExpiresAt: expires})
}

type receiveRequest struct {
	Code string `json:"code"`
}

type receiveResponse struct {
	Item string `json:"item"`
	Name string `json:"name,omitempty"`
	Icon string `json:"icon,omitempty"`
}

// PostReceiveItem takes the item a teammate's code gives: once, from a
// player on the same team who may still give it, before the code expires.
func (h *Handlers) PostReceiveItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req receiveRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	code := strings.TrimPrefix(strings.TrimSpace(req.Code), giftPrefix)

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "receiving failed", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var item string
	var from int64
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT item, from_slot, expires_at FROM item_gifts WHERE code = $1 AND team_id = $2 FOR UPDATE`,
		code, slot.TeamID).Scan(&item, &from, &expires)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Another team's code, a used one, or no code at all: the same to the phone.
		httpx.JSON(w, http.StatusNotFound, playRefusal{Error: "that is not a code to receive an item", Code: "gift_unknown"})
		return
	case err != nil:
		httpx.ServerErr(w, http.StatusInternalServerError, "receiving failed", err)
		return
	case time.Now().After(expires):
		httpx.JSON(w, http.StatusGone, playRefusal{Error: "the code has run out; ask for a new one", Code: "gift_expired"})
		return
	case from == slot.ID:
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "that code is your own; a teammate scans it", Code: "gift_own"})
		return
	}
	// The giver may have used it up, or given it on, since.
	it, err := h.carried(play.ForPhone(ctx, from), slot.TeamID, item)
	if err != nil {
		h.itemErr(w, err)
		return
	}
	if !it.Give {
		httpx.JSON(w, http.StatusConflict, playRefusal{Error: "the item is no longer theirs to give", Code: "gift_gone"})
		return
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO team_item_holders (team_id, item, slot_id) VALUES ($1, $2, $3)
		ON CONFLICT (team_id, item) DO UPDATE SET slot_id = EXCLUDED.slot_id, given_at = now()`,
		slot.TeamID, item, slot.ID); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "receiving failed", err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_gifts WHERE code = $1`, code); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "receiving failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "receiving failed", err)
		return
	}
	// Both backpacks show something else now.
	h.Hub.Publish(ws.TeamTopic(slot.TeamID), "changed", nil)
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	httpx.JSON(w, http.StatusOK, receiveResponse{Item: item, Name: it.Name, Icon: it.Icon})
}

var (
	errNotCarried = errors.New("the team does not carry that item")
	errNoGame     = errors.New("the game has not started")
)

// carried is the item as the phone ctx is for sees it in the team's
// backpack.
func (h *Handlers) carried(ctx context.Context, teamID int64, item string) (session.ItemView, error) {
	view, err := h.engine().Snapshot(ctx, teamID)
	if errors.Is(err, play.ErrNoGraph) {
		return session.ItemView{}, errNoGame
	}
	if err != nil {
		return session.ItemView{}, err
	}
	for _, it := range view.Items {
		if it.Slug == item {
			return it, nil
		}
	}
	return session.ItemView{}, errNotCarried
}

func (h *Handlers) itemErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNoGame):
		httpx.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, errNotCarried):
		httpx.Error(w, http.StatusNotFound, err.Error())
	default:
		httpx.ServerErr(w, http.StatusInternalServerError, "loading game failed", err)
	}
}

// decorateItems names the player each handed-on item is with.
func decorateItems(ctx context.Context, db *sql.DB, teamID int64, v *play.View) error {
	given := false
	for _, it := range v.Items {
		given = given || it.HolderSlot != 0
	}
	if !given {
		return nil
	}
	names, err := slotNames(ctx, db, teamID)
	if err != nil {
		return err
	}
	for i := range v.Items {
		if s := v.Items[i].HolderSlot; s != 0 {
			v.Items[i].Holder = names[s]
		}
	}
	return nil
}
