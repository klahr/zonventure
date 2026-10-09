package play

import (
	"context"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// decorate adds to a view what the session cannot know on its own:
// standings, which need the other teams. In a party game that holds its
// results (ResultsHeld) it takes out what would give them away.
func (e *Engine) decorate(ctx context.Context, q Querier, g *graph.Graph, eventID, teamID int64, v *View) error {
	held, err := ResultsHeld(ctx, q, teamID)
	if err != nil {
		return err
	}
	if held {
		v.ResultsHeld, v.HideScore = true, true
		v.Score, v.PenaltyScore, v.Quiz = 0, 0, nil
		return nil
	}
	var standings bool
	for _, b := range v.Blocks {
		standings = standings || b.Kind == graph.BlockStandings && !b.Locked
	}
	if standings {
		st, err := standingsOf(ctx, q, g, eventID, teamID)
		if err != nil {
			return err
		}
		for i := range v.Blocks {
			if v.Blocks[i].Kind == graph.BlockStandings && !v.Blocks[i].Locked {
				v.Blocks[i].Standings = st
			}
		}
	}
	return nil
}

// standingsOf is the team's place by the event's ranking (Ahead): its
// rank, and the nearest teams above and below it.
func standingsOf(ctx context.Context, q Querier, g *graph.Graph, eventID, teamID int64) (*session.Standings, error) {
	all, err := ranking(ctx, q, g, eventID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(all, func(r Ranked) bool { return r.TeamID == teamID })
	if i < 0 {
		return nil, nil
	}
	me := all[i]
	st := &session.Standings{Rank: 1, Of: len(all)}
	for _, r := range all {
		switch {
		case Ahead(r, me):
			st.Rank++
			if st.Ahead == nil || r.Score-me.Score < st.Ahead.Gap {
				st.Ahead = &session.Rival{Name: r.Name, Gap: r.Score - me.Score}
			}
		case Ahead(me, r):
			if st.Behind == nil || me.Score-r.Score < st.Behind.Gap {
				st.Behind = &session.Rival{Name: r.Name, Gap: me.Score - r.Score}
			}
		}
	}
	return st, nil
}
