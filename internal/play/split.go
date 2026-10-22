package play

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/logx"
)

// StoredGroups are a team's stored groups (team_graph_state.strands) as
// the engine plays them: none where they cannot be read, or no longer
// fit the graph (SplitFits), which the team's next view or action deals
// afresh.
func StoredGroups(splits *graph.Splits, split string, raw []byte) []session.Strand {
	var strands []session.Strand
	if split == "" || json.Unmarshal(raw, &strands) != nil || !session.SplitFits(splits, split, strands) {
		return nil
	}
	return strands
}

// Groups is a team as the organisers see it (Engine.Groups): while it
// is split up, its groups, each with whether they can move it on from its
// step, and the players dealt among them (Players); and whether its time
// limit is due (TimeUp), which a move of theirs runs out.
type Groups struct {
	Splits    *graph.Splits
	Split     string
	Strands   []session.Strand
	Rescuable []bool
	Players   []int64
	TimeUp    bool
}

// Groups reads a team as the organisers' rescue would find it (act, with
// no phone): loaded, located and dealt, with groups that no longer fit
// its graph split afresh (Team.Refit) as its next view or action will.
// It stores nothing. None for a team not started.
func (e *Engine) Groups(ctx context.Context, teamID int64) (Groups, error) {
	tx, err := e.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Groups{}, err
	}
	defer func() { _ = tx.Rollback() }()
	s, err := e.loadCore(ctx, tx, teamID, false)
	if errors.Is(err, ErrNoGraph) {
		return Groups{}, nil
	}
	if err != nil {
		return Groups{}, err
	}
	var players []int64
	if s.Split != "" && !s.Completed {
		// Who is in the branches of a capacity hub steers where a rescue
		// leads, and so whether its walk can be taken.
		if err := s.countOccupancy(ctx, tx, false); err != nil {
			return Groups{}, err
		}
		if err := e.locate(ctx, tx, s); err != nil {
			return Groups{}, err
		}
		if players, err = e.divide(ctx, tx, s); err != nil {
			return Groups{}, err
		}
	}
	return Groups{Splits: s.Splits(), Split: s.Split, Strands: s.Strands, Rescuable: s.Rescuable(), Players: players, TimeUp: s.TimeUpDue()}, nil
}

// divide readies a split team for a view or an action: its groups dealt
// (deal), and, where the graph asks, each group's walk read from the
// pings taken in it since it came to its step — whoever walked it, and
// however its phones have switched groups or gone quiet since. An
// action stores what changed (saveSplit). It returns the players dealt
// among.
func (e *Engine) divide(ctx context.Context, q Querier, s *teamState) ([]int64, error) {
	if s.Split == "" {
		return nil, nil
	}
	me, _ := phoneOf(ctx)
	players, err := e.deal(ctx, q, s, me)
	if err != nil {
		return nil, err
	}
	if s.walked {
		// A group still on the split has walked nothing of a step yet: its
		// pings are not read, nor the team's since the split from it.
		var (
			groups []walkers
			ports  []string
		)
		s.Walks = make(map[string]float64, len(s.Strands))
		for _, st := range s.Strands {
			s.Walks[st.Port] = 0
			if st.Current != s.Split {
				groups = append(groups, walkers{since: st.NodeEnteredAt, tag: GroupTag(s.Split, st.Port)})
				ports = append(ports, st.Port)
			}
		}
		walked, err := walkedEach(ctx, q, s.teamID, groups)
		if err != nil {
			return nil, fmt.Errorf("reading walked distance: %w", err)
		}
		for i, port := range ports {
			s.Walks[port] = walked[i]
		}
	}
	return players, nil
}

// deal deals a split team's players, with me the phone asking (0 for
// none): anyone playing who is in no group yet goes into one
// (session.Team.Deal), by the roles as dealt; and a group whose players
// are all away is anyone's until one is back, me not away. After who is
// away is known, so dealing drops a choice of a group whose players are
// back. It returns the players.
func (e *Engine) deal(ctx context.Context, q Querier, s *teamState, me int64) ([]int64, error) {
	players := s.Players
	if players == nil {
		var err error
		if players, err = Players(ctx, q, s.teamID, me); err != nil {
			return nil, fmt.Errorf("reading players: %w", err)
		}
	}
	if s.Away == nil {
		var err error
		if s.Away, err = away(ctx, q, s.teamID, s.Now); err != nil {
			return nil, fmt.Errorf("reading who is away: %w", err)
		}
	}
	if me != 0 {
		delete(s.Away, me)
	}
	// Who holds which role, where it decides a newcomer's group: a path
	// of the split is a role's.
	holders := s.RoleHolders
	if holders == nil && len(s.G.DealtRoles()) > 0 && pathsHaveRoles(s.G, s.Split) && len(session.Undealt(s.Strands, players)) > 0 {
		dealt, err := DealRolesAmong(ctx, q, s.teamID, players, s.G.DealtRoles())
		if err != nil {
			return nil, fmt.Errorf("dealing roles: %w", err)
		}
		holders = Holders(dealt)
	}
	s.Deal(players, holders)
	return players, nil
}

// pathsHaveRoles reports whether any path of split is a role's.
func pathsHaveRoles(g *graph.Graph, split string) bool {
	n, _ := g.Node(split)
	return n.SplitCfg != nil && slices.ContainsFunc(n.SplitCfg.Paths, func(p graph.SplitPath) bool { return p.Role != "" })
}

// dealArrived deals a team just come to a split by a move that knew none
// of its players — an organiser's, the AI's, the event's start, the dev
// phone's — so its groups are stored as first shown, rather than dealt
// again on every view until someone acts, each phone's group changing
// as players open their links.
func (e *Engine) dealArrived(ctx context.Context, q Querier, s *teamState) error {
	if s.Split == "" || slices.ContainsFunc(s.Strands, func(st session.Strand) bool { return len(st.Members) > 0 }) {
		return nil
	}
	me, _ := phoneOf(ctx)
	_, err := e.deal(ctx, q, s, me)
	return err
}

// WalkingGroup is the group the phone slot of team teamID walks while
// the team is split up, as a view or action of its would deal it
// (deal): the split and the group's path. None for a team together, or
// at its split with no group set off yet (session.SetOff). It runs on every
// location ping, so the groups, who plays and when each phone was heard
// from are one query, and the graph is the cache's; for a team together
// that query is one lookup by team_graph_state's primary key, finding
// no split, and the rest is skipped.
func (e *Engine) WalkingGroup(ctx context.Context, teamID, slot int64) (split, port string, err error) {
	var (
		graphID      int64
		raw, rawSeen []byte
	)
	s := newTeamState(teamID, e.now())
	err = e.DB.QueryRowContext(ctx, `
		SELECT gs.graph_id, gs.split, gs.strands, COALESCE((
		  SELECT jsonb_agg(jsonb_build_object('slot', s.id, 'seen', `+seenSQL+`) ORDER BY s.id)
		  FROM player_slots s WHERE s.team_id = gs.team_id AND `+playingSQL+`), '[]')
		FROM team_graph_state gs
		WHERE gs.team_id = $1 AND gs.split IS NOT NULL AND jsonb_typeof(gs.strands) = 'array'`, teamID, slot).
		Scan(&graphID, &s.Split, &raw, &rawSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if err := json.Unmarshal(raw, &s.Strands); err != nil {
		return "", "", err
	}
	if !session.SetOff(s.Split, s.Strands) {
		// At the split still, no group out: the team's pings.
		return "", "", nil
	}
	loaded, err := e.loadGraph(ctx, e.DB, graphID)
	if err != nil {
		return "", "", err
	}
	s.G = loaded.g
	s.ShareSplits(loaded.splits)
	if !session.SplitFits(loaded.splits, s.Split, s.Strands) {
		// Groups no longer fitting the graph are split afresh by the
		// team's next view or action, not walked meanwhile: its pings
		// are the team's until then.
		return "", "", nil
	}
	var seen []struct {
		Slot int64  `json:"slot"`
		Seen string `json:"seen"`
	}
	if err := json.Unmarshal(rawSeen, &seen); err != nil {
		return "", "", err
	}
	// Who plays and who is away, as Players and away read them.
	s.Players, s.Away = []int64{}, map[int64]bool{}
	for _, p := range seen {
		s.Players = append(s.Players, p.Slot)
		if isAway(p.Seen, s.Now) {
			s.Away[p.Slot] = true
		}
	}
	if !slices.Contains(s.Players, slot) {
		s.Players = append(s.Players, slot)
	}
	// Dealt as a view deals: roles are read only for players in no group
	// yet, between a player's joining and the first view since — theirs,
	// as they open the game — which stores them in one.
	players, err := e.deal(ctx, e.DB, s, slot)
	if err != nil {
		return "", "", err
	}
	for p, slots := range s.Walkers(players) {
		if slices.Contains(slots, slot) {
			return s.Split, p, nil
		}
	}
	return "", "", nil
}

// GroupTag is how a ping names the group its phone was walking
// (location_pings.groups): its split and path as JSON —
// [{"split":…,"port":…}] — which no slug or port can make read as
// another group's, so a group's walk and trail are its own pings.
func GroupTag(split, port string) string {
	b, err := json.Marshal([]groupTag{{split, port}})
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseGroupTag reads a ping's group back: false for a tag not read.
func ParseGroupTag(tag string) (split, port string, ok bool) {
	var out []groupTag
	if json.Unmarshal([]byte(tag), &out) != nil || len(out) != 1 {
		return "", "", false
	}
	return out[0].Split, out[0].Port, true
}

type groupTag struct {
	Split string `json:"split"`
	Port  string `json:"port"`
}

// splitDoc is the team's groups as saveSplit stores them: the split, and
// its strands as JSON; nothing for a team together.
func splitDoc(s *teamState) (string, []byte, error) {
	if s.Split == "" {
		return "", nil, nil
	}
	doc, err := json.Marshal(s.Strands)
	return s.Split, doc, err
}

// keepDeal stores a split team's groups as a view dealt them — a player
// new to the split put in a group, a spent choice dropped, groups split
// afresh or made whole — so the next view, and the phone's pings, find
// them as shown, rather than dealt again each time until someone acts,
// and a phone's group changing as others open their links. Only over the
// groups as the view read them, on the step it read: an action that
// moved the team or stored other groups since wins, and the next view
// deals from those. It writes after the view's own read is done, never
// beside it; failing, it leaves the view as it is.
func (e *Engine) keepDeal(ctx context.Context, s *teamState) {
	split, doc, err := splitDoc(s)
	if err != nil || split == s.storedSplit && bytes.Equal(doc, s.storedStrands) {
		return
	}
	// Whole again — groups that no longer fit, made one (Team.Refit) —
	// is stored too: no split and no groups.
	var now, was any
	if doc != nil {
		now = doc
	}
	if s.storedRaw != nil {
		was = s.storedRaw
	}
	// The team's own mover too: groups split afresh, or made whole, were
	// moved by no one (SplitAfresh).
	if _, err := e.DB.ExecContext(ctx, `
		UPDATE team_graph_state SET split = $1, strands = $2, moved = $7, moved_by = $8
		WHERE team_id = $3 AND current_node IS NOT DISTINCT FROM $6
		  AND split IS NOT DISTINCT FROM $4 AND strands IS NOT DISTINCT FROM $5::jsonb
		  AND moved = $9 AND moved_by = $10`,
		nullIfEmpty(split), now, s.teamID, nullIfEmpty(s.storedSplit), was, nullIfEmpty(s.whole), s.Moved, s.MovedBy,
		s.storedMoved, s.storedMovedBy); err != nil {
		logx.Warn("play.keep_deal_failed", "team", s.teamID, "err", err.Error())
	}
}

// saveSplit stores the team's groups, or that it has none, where they
// differ from what is stored: once, at the end of an action.
func saveSplit(ctx context.Context, tx *sql.Tx, s *teamState) error {
	split, doc, err := splitDoc(s)
	if err != nil || split == s.storedSplit && bytes.Equal(doc, s.storedStrands) {
		return err
	}
	var splitArg, docArg any
	if split != "" {
		splitArg, docArg = split, doc
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_graph_state SET split = $1, strands = $2 WHERE team_id = $3`, splitArg, docArg, s.teamID); err != nil {
		return err
	}
	s.storedSplit, s.storedStrands, s.storedRaw = split, doc, doc
	return nil
}
