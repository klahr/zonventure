package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// splitFits reports whether a team's stored groups still fit its graph:
// the split is there, with a path for each group.
func splitFits(g *graph.Graph, split string, strands []session.Strand) bool {
	n, ok := g.Node(split)
	if !ok || n.Type != graph.Split || n.SplitCfg == nil || len(n.SplitCfg.Paths) != len(strands) {
		return false
	}
	for i, st := range strands {
		if _, ok := g.Node(st.Current); !ok || n.SplitCfg.Paths[i].Port != st.Port {
			return false
		}
	}
	return true
}

// divide readies a split team for a view or an action: anyone playing
// who is in no group yet is dealt into one (session.Team.Deal), and,
// where the graph asks, each group's walk is read from its own phones.
// It reports whether anyone was dealt, for an action to store.
func (e *Engine) divide(ctx context.Context, q Querier, s *teamState) (bool, error) {
	if s.Split == "" {
		return false, nil
	}
	me, _ := phoneOf(ctx)
	players := s.Players
	if players == nil {
		var err error
		if players, err = Players(ctx, q, s.teamID, me); err != nil {
			return false, fmt.Errorf("reading players: %w", err)
		}
	}
	holders := s.RoleHolders
	if holders == nil && len(s.G.DealtRoles()) > 0 && len(players) > 0 {
		dealt, err := dealRoles(ctx, q, s.teamID, players, s.G.DealtRoles())
		if err != nil {
			return false, fmt.Errorf("dealing roles: %w", err)
		}
		holders = make(map[string]int64, len(dealt))
		for role, h := range dealt {
			holders[role] = h.Slot
		}
	}
	dealt := s.Deal(players, holders)
	if usesWalked(s.G) {
		s.Walks = map[string]float64{}
		for _, st := range s.Strands {
			w, err := walkedBy(ctx, q, s.teamID, st.EnteredAt, st.Members)
			if err != nil {
				return false, fmt.Errorf("reading walked distance: %w", err)
			}
			s.Walks[st.Port] = w
		}
	}
	return dealt, nil
}

// saveSplit stores the team's groups, or that it has none.
func saveSplit(ctx context.Context, tx *sql.Tx, s *teamState) error {
	var split, strands any
	if s.Split != "" {
		doc, err := json.Marshal(s.Strands)
		if err != nil {
			return err
		}
		split, strands = s.Split, doc
	}
	_, err := tx.ExecContext(ctx, `UPDATE team_graph_state SET split = $1, strands = $2 WHERE team_id = $3`, split, strands, s.teamID)
	return err
}
