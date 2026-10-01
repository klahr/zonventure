package player

// The roles a team's players take (graph.Role). The engine deals them
// to the phone asking; here the view learns who holds each, and a player
// claims or gives up a role.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/play"
	"github.com/klahr/zonventure/internal/ws"
)

// decorateRoles says, for the player asking, who holds each of the
// view's roles and which are theirs.
func decorateRoles(ctx context.Context, db *sql.DB, teamID, slotID int64, v *play.View) error {
	if len(v.Roles) == 0 {
		return nil
	}
	slugs := make([]string, len(v.Roles))
	for i, r := range v.Roles {
		slugs[i] = r.Slug
	}
	holdings, err := play.DealRoles(ctx, db, teamID, slotID, slugs)
	if err != nil {
		return err
	}
	names, err := slotNames(ctx, db, teamID)
	if err != nil {
		return err
	}
	for i := range v.Roles {
		r := &v.Roles[i]
		h := holdings[r.Slug]
		r.Mine, r.Holder, r.Claimed = h.Slot == slotID, names[h.Slot], h.Claimed
	}
	return nil
}

func slotNames(ctx context.Context, db *sql.DB, teamID int64) (map[int64]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM player_slots WHERE team_id = $1`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// RoleRequest is the body of POST /api/player/role: take the role, or
// with Claim false give it back, to be dealt again.
type RoleRequest struct {
	Role  string `json:"role"`
	Claim bool   `json:"claim"`
}

// PostRole claims or gives up one of the adventure's roles for the
// player asking. A role another playing teammate claimed is theirs until
// they give it up — the team settles it out loud, then on the phone;
// one claimed by a player no longer playing is free. A role only dealt
// is anyone's to claim. Roles may change hands during the game: passing
// the map to a teammate is the team's call.
func (h *Handlers) PostRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req RoleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	slot, _ := auth.SlotFromContext(ctx)
	view, err := h.engine().Snapshot(ctx, slot.TeamID)
	if errors.Is(err, play.ErrNoGraph) {
		httpx.Error(w, http.StatusConflict, "the game has not started")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading game failed", err)
		return
	}
	known := false
	for _, ro := range view.Roles {
		known = known || ro.Slug == req.Role
	}
	if !known {
		httpx.Error(w, http.StatusNotFound, "no such role")
		return
	}

	if !req.Claim {
		if _, err := h.DB.ExecContext(ctx, `DELETE FROM team_roles WHERE team_id = $1 AND role = $2 AND slot_id = $3`,
			slot.TeamID, req.Role, slot.ID); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "giving up role failed", err)
			return
		}
	} else {
		players, err := play.Players(ctx, h.DB, slot.TeamID, slot.ID)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "loading team failed", err)
			return
		}
		// Taken over only from a slot that is not playing: the WHERE turns
		// a live teammate's claim into no row, which is a refusal.
		res, err := h.DB.ExecContext(ctx, `
			INSERT INTO team_roles(team_id, role, slot_id) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, role) DO UPDATE SET slot_id = EXCLUDED.slot_id, claimed_at = now_text()
			WHERE team_roles.slot_id = EXCLUDED.slot_id OR NOT (team_roles.slot_id = ANY($4))`,
			slot.TeamID, req.Role, slot.ID, players)
		if err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "claiming role failed", err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.JSON(w, http.StatusConflict, playRefusal{Error: "a teammate has taken that role", Code: "role_taken"})
			return
		}
	}
	// Every teammate's phone shows something else now.
	h.Hub.Publish(ws.TeamTopic(slot.TeamID), "changed", nil)
	h.Hub.Publish(ws.EventTopic(slot.EventID), "changed", nil)
	w.WriteHeader(http.StatusNoContent)
}
