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
// every split each group came to is there, with the path it took.
func splitFits(g *graph.Graph, split string, strands []session.Strand) bool {
	if len(strands) == 0 {
		return false
	}
	for _, st := range strands {
		if _, ok := g.Node(st.Current); !ok {
			return false
		}
		for _, f := range st.Lineage(split) {
			n, ok := g.Node(f.Split)
			if !ok || n.Type != graph.Split {
				return false
			}
			if _, ok := n.Path(f.Port); !ok {
				return false
			}
		}
	}
	return true
}

// divide readies a split team for a view or an action: anyone playing
// who is in no group yet is dealt into one (session.Team.Deal), and,
// where the graph asks, each group's walk is read from its own phones.
// It reports whether anyone was dealt, or a choice of a group dropped,
// for an action to store.
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
	if s.Away == nil {
		// A group whose players are all away is anyone's until one is back.
		var err error
		if s.Away, err = away(ctx, q, s.teamID, s.Now); err != nil {
			return false, fmt.Errorf("reading who is away: %w", err)
		}
	}
	if me != 0 {
		// The phone asking is not.
		delete(s.Away, me)
	}
	// After who is away is known: dealing drops a choice of a group whose
	// players are back.
	dealt := s.Deal(players, holders)
	if usesWalked(s.G) {
		// Each group's walk is its walkers', whoever they are: its own
		// players, or those walking it for players gone. No one's is none.
		s.Walks = map[string]float64{}
		walkers := s.Walkers(players)
		for _, st := range s.Strands {
			var w float64
			if slots := walkers[st.Key()]; len(slots) > 0 {
				var err error
				if w, err = walkedBy(ctx, q, s.teamID, st.EnteredAt, slots); err != nil {
					return false, fmt.Errorf("reading walked distance: %w", err)
				}
			}
			s.Walks[st.Key()] = w
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
