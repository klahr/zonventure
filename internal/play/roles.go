package play

import (
	"context"
	"slices"
)

// Players are the ids of a team's players who count as playing: those
// who have opened their link, and me — the one asking, if any — in slot
// order. A forgotten slot is out.
func Players(ctx context.Context, q Querier, teamID, me int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id FROM player_slots
		WHERE team_id = $1 AND (last_login_at IS NOT NULL OR id = $2) AND name <> '(forgotten)'
		ORDER BY id`, teamID, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if me != 0 && !slices.Contains(out, me) {
		out = append(out, me)
	}
	return out, rows.Err()
}

// Holding is who holds one role, and whether they claimed it.
type Holding struct {
	Slot    int64
	Claimed bool
}

// DealRoles says who holds each of roles on a team, as seen by me (see
// Players): a role a playing player claimed is theirs; the rest are dealt
// in the author's order, each to the player holding fewest so far, the
// first in slot order on a tie. Every role is someone's while anyone
// plays, and a player alone holds them all. Nothing dealt is stored, so a
// player who opens their link later takes their share of the unclaimed.
func DealRoles(ctx context.Context, q Querier, teamID, me int64, roles []string) (map[string]Holding, error) {
	if len(roles) == 0 {
		return map[string]Holding{}, nil
	}
	players, err := Players(ctx, q, teamID, me)
	if err != nil {
		return nil, err
	}
	return dealRoles(ctx, q, teamID, players, roles)
}

// dealRoles is DealRoles among players already found.
func dealRoles(ctx context.Context, q Querier, teamID int64, players []int64, roles []string) (map[string]Holding, error) {
	out := map[string]Holding{}
	if len(roles) == 0 || len(players) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT role, slot_id FROM team_roles WHERE team_id = $1`, teamID)
	if err != nil {
		return nil, err
	}
	claims := map[string]int64{}
	for rows.Next() {
		var role string
		var slot int64
		if err := rows.Scan(&role, &slot); err != nil {
			rows.Close()
			return nil, err
		}
		claims[role] = slot
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	held := make([]int, len(players))
	for _, r := range roles {
		if i := slices.Index(players, claims[r]); i >= 0 {
			out[r] = Holding{Slot: players[i], Claimed: true}
			held[i]++
		}
	}
	for _, r := range roles {
		if _, ok := out[r]; ok {
			continue
		}
		least := 0
		for i := range players {
			if held[i] < held[least] {
				least = i
			}
		}
		out[r] = Holding{Slot: players[least]}
		held[least]++
	}
	return out, nil
}

// RolesOf are the roles slot holds, in the author's order.
func RolesOf(holdings map[string]Holding, roles []string, slot int64) []string {
	var out []string
	for _, r := range roles {
		if holdings[r].Slot == slot {
			out = append(out, r)
		}
	}
	return out
}
