package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
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
// of the team, with their roles and groups. Roles are dealt as the
// players' phones deal them (play.DealRoles), so a player who has not
// opened their link yet holds none.
func (h *Handlers) TeamPlayers(w http.ResponseWriter, r *http.Request) {
	teamID, _, ok := h.teamFromRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var (
		slug    string
		graphID int64
		split   sql.NullString
		strands []byte
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT ev.adventure_slug, COALESCE(gs.graph_id, ev.graph_id, 0), gs.split, gs.strands
		FROM teams t JOIN events ev ON ev.id = t.event_id
		LEFT JOIN team_graph_state gs ON gs.team_id = t.id
		WHERE t.id = $1`, teamID).Scan(&slug, &graphID, &split, &strands)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}
	g, err := h.eventGraph(ctx, slug, graphID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, graphstore.ErrNotFound) {
		httpx.ServerErr(w, http.StatusInternalServerError, "loading the adventure failed", err)
		return
	}
	if g == nil {
		g = &graph.Graph{}
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
	if err := rows.Close(); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "lookup failed", err)
		return
	}

	playing, err := play.Players(ctx, h.DB, teamID, 0)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "reading players failed", err)
		return
	}
	dealt := g.DealtRoles()
	holdings, err := play.DealRoles(ctx, h.DB, teamID, 0, dealt)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "dealing roles failed", err)
		return
	}
	var groups []session.Strand
	if split.Valid && len(strands) > 0 {
		_ = json.Unmarshal(strands, &groups)
	}
	for i := range out {
		p := &out[i]
		p.Playing = slices.Contains(playing, p.ID)
		for _, role := range play.RolesOf(holdings, dealt, p.ID) {
			p.Roles = append(p.Roles, g.RoleName(role))
		}
		// The default role is everyone else's who plays — and a player alone's.
		if d := g.DefaultRole(); d != "" && p.Playing && (len(p.Roles) == 0 || len(playing) == 1) {
			p.Roles = append(p.Roles, g.RoleName(d))
		}
		for _, st := range groups {
			if slices.Contains(st.Members, p.ID) {
				p.Groups = append(p.Groups, session.GroupLabel(g, "", st.Lineage(split.String)))
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"players": out})
}

// rescuedGroup is the name of the group of a split team a rescue moves
// on: the one with key, or else the one on node; "" for a team not split.
func (h *Handlers) rescuedGroup(ctx context.Context, teamID int64, node, key string) string {
	var (
		graphID int64
		split   sql.NullString
		raw     []byte
	)
	if err := h.DB.QueryRowContext(ctx, `SELECT graph_id, split, strands FROM team_graph_state WHERE team_id = $1`, teamID).
		Scan(&graphID, &split, &raw); err != nil || !split.Valid || len(raw) == 0 {
		return ""
	}
	var strands []session.Strand
	if json.Unmarshal(raw, &strands) != nil {
		return ""
	}
	st, err := graphstore.ByID(ctx, h.DB, graphID)
	if err != nil {
		return ""
	}
	for _, s := range strands {
		if key != "" && s.Key() == key || key == "" && s.Current == node {
			return session.GroupLabel(st.Graph, "", s.Lineage(split.String))
		}
	}
	return ""
}
