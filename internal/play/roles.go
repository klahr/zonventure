package play

import (
	"context"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
)

// playingSQL is whether player_slots s counts as playing (Players), $2
// the one asking.
const playingSQL = `(s.last_login_at IS NOT NULL OR s.id = $2) AND s.name <> '(forgotten)'`

// Players are the ids of a team's players who count as playing: those
// who have opened their link, and me — the one asking, if any — in slot
// order. A forgotten slot is out.
func Players(ctx context.Context, q Querier, teamID, me int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT s.id FROM player_slots s
		WHERE s.team_id = $1 AND `+playingSQL+`
		ORDER BY s.id`, teamID, me)
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

// Holding is who holds one role, whether they claimed it, and whether
// it is theirs for good: the team's roles are locked (LockRoles).
type Holding struct {
	Slot    int64
	Claimed bool
	Final   bool
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
	return DealRolesAmong(ctx, q, teamID, players, roles)
}

// DealRolesAmong is DealRoles among players already found (Players).
func DealRolesAmong(ctx context.Context, q Querier, teamID int64, players []int64, roles []string) (map[string]Holding, error) {
	out := map[string]Holding{}
	if len(roles) == 0 {
		return out, nil
	}
	var locked bool
	if err := q.QueryRowContext(ctx, `SELECT roles_locked FROM teams WHERE id = $1`, teamID).Scan(&locked); err != nil {
		return nil, err
	}
	if len(players) == 0 && !locked {
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
	if locked {
		// Final: who held each as the team locked them, playing or not.
		// A player who joined since holds none.
		for _, r := range roles {
			if slot, ok := claims[r]; ok {
				out[r] = Holding{Slot: slot, Claimed: true, Final: true}
			}
		}
		return out, nil
	}
	return DealAmong(players, claims, roles), nil
}

// DealAmong deals roles among players, who must be at least one: a role
// claimed by one of them is theirs, and the rest go in the author's
// order, each to the player holding fewest so far, the first on a tie.
func DealAmong(players []int64, claims map[string]int64, roles []string) map[string]Holding {
	out := map[string]Holding{}
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
	return out
}

// Holders is who holds each role of holdings, by slot.
func Holders(holdings map[string]Holding) map[string]int64 {
	out := make(map[string]int64, len(holdings))
	for role, h := range holdings {
		out[role] = h.Slot
	}
	return out
}

// PlayerRoles are the public roles slot holds, in the author's order,
// among players playing: the dealt roles it holds (holders: who holds
// each), and the default role — everyone's who holds no other, and a
// player alone's.
func PlayerRoles(g *graph.Graph, holders map[string]int64, slot int64, players int) []string {
	var out []string
	for _, r := range g.DealtRoles() {
		if h, ok := holders[r]; ok && h == slot {
			out = append(out, r)
		}
	}
	if d := g.DefaultRole(); d != "" && (len(out) == 0 || players <= 1) {
		out = append(out, d)
	}
	return out
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

// LockRoles makes the team's roles final: each held as dealt now is
// stored as its player's, and no one takes or gives back a role after. A
// team whose roles are locked already is left as it is.
func LockRoles(ctx context.Context, q Querier, teamID int64, players []int64, roles []string) error {
	var locked bool
	if err := q.QueryRowContext(ctx, `SELECT roles_locked FROM teams WHERE id = $1`, teamID).Scan(&locked); err != nil || locked {
		return err
	}
	holdings, err := DealRolesAmong(ctx, q, teamID, players, roles)
	if err != nil {
		return err
	}
	for _, r := range roles {
		h, ok := holdings[r]
		if !ok {
			continue
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO team_roles(team_id, role, slot_id) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, role) DO UPDATE SET slot_id = EXCLUDED.slot_id`, teamID, r, h.Slot); err != nil {
			return err
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE teams SET roles_locked = TRUE WHERE id = $1`, teamID)
	return err
}

// lockRolesLeaving locks the team's roles as it leaves node, when a roles
// block there asks for it (graph.BlockRoles, Lock), among the players
// playing then: the whole team's, though a group of it is leaving.
func lockRolesLeaving(ctx context.Context, q Querier, s *teamState, node string) error {
	n, ok := s.G.Node(node)
	if !ok || !slices.ContainsFunc(n.Blocks(), func(b graph.Block) bool { return b.Kind == graph.BlockRoles && b.Lock && !b.Disabled }) {
		return nil
	}
	players := s.Everyone()
	if players == nil {
		var err error
		if players, err = Players(ctx, q, s.teamID, 0); err != nil {
			return err
		}
	}
	return LockRoles(ctx, q, s.teamID, players, s.G.DealtRoles())
}
