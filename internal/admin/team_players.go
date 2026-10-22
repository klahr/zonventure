package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/play"
)

// teamPlayerDTO is one of a team's players as the organisers see them:
// the public roles they hold and, while the team is split up, the groups
// they are in. A secret role is never listed: no one but its player
// knows who holds it until the team finishes.
type teamPlayerDTO struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Playing bool     `json:"playing"`
	Roles   []string `json:"roles"`
	Groups  []string `json:"groups"`
}

// TeamPlayers serves GET /api/admin/teams/{teamID}/players: each player
// of the team, with their roles and groups, and while the team is split
// up its groups, each with whether the organisers can move it on now.
// Roles are dealt as the players' phones deal them (play.DealRoles), so
// a player who has not opened their link yet holds none.
func (h *Handlers) TeamPlayers(w http.ResponseWriter, r *http.Request) {
	teamID, _, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var (
		slug, node, split string
		graphID           int64
		paused            int
		live              bool
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT ev.adventure_slug, COALESCE(gs.graph_id, ev.graph_id, 0), ev.paused_seconds,
		       gs.team_id IS NOT NULL AND gs.completed_at IS NULL,
		       COALESCE(gs.current_node, ''), COALESCE(gs.split, '')
		FROM teams t JOIN events ev ON ev.id = t.event_id
		LEFT JOIN team_graph_state gs ON gs.team_id = t.id
		WHERE t.id = $1`, teamID).Scan(&slug, &graphID, &paused, &live, &node, &split)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	// The graph the team plays, the engine's shared one — before it
	// starts, the event's.
	var (
		g      *graph.Graph
		splits *graph.Splits
	)
	if graphID != 0 {
		if splits, err = h.engine().Splits(ctx, graphID); err == nil {
			g = splits.Graph()
		}
	} else {
		g, err = h.eventGraph(ctx, slug, graphID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, graphstore.ErrNotFound) && !errors.Is(err, play.ErrNoGraph) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the adventure failed", err)
		return
	}
	if g == nil {
		g = &graph.Graph{}
	}
	// A team begun and not finished, as the organisers' rescue would find it
	// (play.Engine.Groups) — split up, or standing on a split, or with a
	// time limit to run out — its groups and whether its time limit is
	// due. They are a part of the list, not worth losing it over: not
	// read, the groups are null — the console shows the event's — rather
	// than none.
	var gs play.Groups
	groups := []teamGroupDTO{}
	if splits != nil && live && (split != "" || g.TimeLimit != nil || session.Refits(splits, node, "", nil)) {
		if gs, err = h.engine().Groups(ctx, teamID); err != nil {
			logx.Warn("admin.team_groups_failed", "team", teamID, "err", err.Error())
			groups = nil
		}
	}

	rows, err := h.DB.QueryContext(ctx, `SELECT id, name FROM player_slots WHERE team_id = $1 AND name <> '(forgotten)' ORDER BY id`, teamID)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	out := []teamPlayerDTO{}
	for rows.Next() {
		p := teamPlayerDTO{Roles: []string{}, Groups: []string{}}
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			rows.Close()
			httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
			return
		}
		out = append(out, p)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}

	// Who plays, as the groups were dealt among — read here for a team
	// not split.
	playing := gs.Players
	if playing == nil {
		if playing, err = play.Players(ctx, h.DB, teamID, 0); err != nil {
			httpx.ServerErr(w, http.StatusInternalServerError, "reading players failed", err)
			return
		}
	}
	dealt := g.DealtRoles()
	holdings, err := play.DealRolesAmong(ctx, h.DB, teamID, playing, dealt)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dealing roles failed", err)
		return
	}
	holders := play.Holders(holdings)
	for i := range out {
		p := &out[i]
		p.Playing = slices.Contains(playing, p.ID)
		// A player not playing holds what was dealt them, and no default.
		roles := play.RolesOf(holdings, dealt, p.ID)
		if p.Playing {
			roles = play.PlayerRoles(g, holders, p.ID, len(playing))
		}
		for _, role := range roles {
			p.Roles = append(p.Roles, g.RoleName(role))
		}
		for _, st := range gs.Strands {
			if slices.Contains(st.Members, p.ID) {
				p.Groups = append(p.Groups, session.GroupLabel(g, "", gs.Split, st.Port))
			}
		}
	}
	if gs.Splits != nil {
		groups = append(groups, groupsOf(gs.Splits, gs.Split, gs.Strands, gs.Rescuable, paused)...)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"players": out, "groups": groups, "time_up": gs.TimeUp})
}
