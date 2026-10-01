package session

// Secret roles: a saboteur among the team (graph.Role.Secret). The engine
// deals them, once, and loads who holds each (Secrets) and whom the team
// accused (Accused); the rules — what the saboteur may do, what an
// accusation scores, what the end reveals — are here.

import (
	"strconv"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// RevealView is, once the team has finished, one secret role laid bare:
// who held it, which of its goals were met, and whether the team named
// them. Holder is filled by the player endpoints, which know names.
// Dealt is false when no one held it: a team too small, or an event with
// secret roles off.
type RevealView struct {
	Role   string     `json:"role"`
	Name   string     `json:"name,omitempty"`
	Dealt  bool       `json:"dealt"`
	Slot   int64      `json:"slot,omitempty"`
	Holder string     `json:"holder,omitempty"`
	Goals  []GoalView `json:"goals,omitempty"`
	// Caught is set when the team was asked: whether most named them.
	Caught *bool `json:"caught,omitempty"`
}

// GoalView is one of a secret role's goals, and whether it was met.
type GoalView struct {
	Text string `json:"text"`
	Met  bool   `json:"met"`
}

// secretHeld reports whether the role is a secret one with a player.
func (t *Team) secretHeld(role string) bool { return t.Secrets[role] != 0 }

// roleShows reports whether a block's own role lets the player asking
// see it: a sabotage is its saboteur's alone, and an accusation is only
// asked of a team that has one. The team as a whole — the organisers, a
// simulated team — sees sabotage, and is asked only about a dealt role.
func (t *Team) roleShows(b graph.Block) bool {
	switch b.Kind {
	case graph.BlockSabotage:
		return t.HasRole(b.Role)
	case graph.BlockAccuse:
		return t.secretHeld(b.Role)
	}
	return true
}

// Sabotage presses a sabotage block on the current step, once: the team
// loses its points, the step's answer locks for its seconds, and its note
// joins the journal. Only the saboteur's phone is shown it, and only a
// block shown can be pressed.
func (t *Team) Sabotage(node, block string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	var b *graph.Block
	for _, x := range n.Blocks() {
		if x.ID == block && x.Kind == graph.BlockSabotage {
			b = &x
		}
	}
	if b == nil || !t.blockShown(n.Slug, *b) {
		return Result{}, nil, ErrWrongNode
	}
	key := PickKey(n.Slug, b.ID)
	if t.Picks[key] != "" {
		return Result{}, nil, ErrNotAllowed
	}
	if t.Picks == nil {
		t.Picks = map[string]string{}
	}
	t.Picks[key] = "1"
	t.PenaltyScore += float64(b.Worth)
	if b.Seconds > 0 && n.Type == graph.Puzzle {
		r := t.Row(n.Slug)
		until := t.Now.Add(time.Duration(b.Seconds) * time.Second)
		if r.BlockedUntil == nil || until.After(*r.BlockedUntil) {
			r.BlockedUntil = &until
		}
	}
	if b.Journal != "" && !t.Journal[b.Journal] {
		t.Journal[b.Journal] = true
		t.JournalOrder = append(t.JournalOrder, b.Journal)
		t.NewJournal = append(t.NewJournal, b.Journal)
	}
	return Result{}, nil, nil
}

// accusePoints are what the team's accusations score: each accusation
// block on a step it reached, whose role is held, pays its worth if most
// of the team named the holder.
func (t *Team) accusePoints() float64 {
	points := 0.0
	for _, n := range t.G.Nodes {
		if !t.Visited(n.Slug) {
			continue
		}
		for _, b := range n.Blocks() {
			if b.Kind == graph.BlockAccuse && t.secretHeld(b.Role) && t.Accused[PickKey(n.Slug, b.ID)] == t.Secrets[b.Role] {
				points += float64(b.Worth)
			}
		}
	}
	return points
}

// reveal is the end screen's unmasking: every secret role, once the team
// has finished.
func (t *Team) reveal() []RevealView {
	if !t.Completed {
		return nil
	}
	var out []RevealView
	for _, ro := range t.G.Roles {
		if !ro.Secret {
			continue
		}
		rv := RevealView{Role: ro.Slug, Name: t.G.Tr(t.Lang, graph.RoleKey(ro.Slug, "name"), t.G.RoleName(ro.Slug)), Slot: t.Secrets[ro.Slug]}
		rv.Dealt = rv.Slot != 0
		if rv.Dealt {
			for i, g := range ro.Goals {
				rv.Goals = append(rv.Goals, GoalView{
					Text: t.G.Tr(t.Lang, graph.RoleKey(ro.Slug, goalField(i)), g.Text),
					Met:  cond.Eval(g.When, t),
				})
			}
			for _, n := range t.G.Nodes {
				for _, b := range n.Blocks() {
					if b.Kind == graph.BlockAccuse && b.Role == ro.Slug && t.Visited(n.Slug) {
						caught := t.Accused[PickKey(n.Slug, b.ID)] == rv.Slot
						rv.Caught = &caught
					}
				}
			}
		}
		out = append(out, rv)
	}
	return out
}

func goalField(i int) string { return "goal:" + strconv.Itoa(i) }
