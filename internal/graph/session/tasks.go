package session

import (
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Tasks: a step done in parallel (graph.AnswerTasks), each role's part a
// task block answered on its own phone. The team stays on the step; the
// last task done moves it on.

// Task answers a task on the current step for the player asking: theirs,
// from its place where it has one. A right answer marks it done, and the
// last one takes the step's answer output; a wrong one changes nothing.
func (t *Team) Task(node, block, guess string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	if n.Type != graph.Puzzle || n.PuzzleCfg == nil || n.PuzzleCfg.Kind() != graph.AnswerTasks {
		return Result{}, nil, ErrWrongNode
	}
	var b *graph.Block
	for _, x := range n.Blocks() {
		if x.ID == block && x.Kind == graph.BlockTask {
			b = &x
		}
	}
	if b == nil || !t.blockShown(n.Slug, *b) {
		return Result{}, nil, ErrWrongNode
	}
	if !t.mayTask(*b) {
		return Result{}, nil, ErrNotYours
	}
	if res, err := t.mayAnswerAt(n, false); err != nil {
		return res, nil, err
	}
	if b.Zone != "" && !t.Here(b.Zone) {
		return Result{}, nil, ErrNotHere
	}
	key := PickKey(n.Slug, b.ID)
	if t.Picks[key] != "" {
		return Result{}, nil, ErrNotAllowed
	}
	if !(match.Matcher{Kind: match.Exact, Value: b.Answer}).Match(guess).OK {
		return Result{}, nil, nil
	}
	if t.Picks == nil {
		t.Picks = map[string]string{}
	}
	t.Picks[key] = "1"
	if t.tasksLeft(n) {
		return Result{Accepted: true}, nil, nil
	}
	// The last task: the step's answer output, wherever the team stands.
	o, ok := outputFor(n, graph.OnAnswer)
	if !ok {
		return Result{}, nil, ErrWrongNode
	}
	t.Row(n.Slug).Solved = true
	points := float64(o.Points)
	step, err := t.leave(n, o.Port, points)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Accepted: true, Port: o.Port, Points: points}, step, nil
}

// tasksLeft reports whether a step shows a task not yet done.
func (t *Team) tasksLeft(n graph.Node) bool {
	for _, b := range n.Blocks() {
		if b.Kind == graph.BlockTask && t.blockShown(n.Slug, b) && t.Picks[PickKey(n.Slug, b.ID)] == "" {
			return true
		}
	}
	return false
}

// mayTask reports whether the player asking may do a task: it is their
// role's, or a role no one on the team holds — so a team too small for
// every part still finishes.
func (t *Team) mayTask(b graph.Block) bool {
	return t.HasRole(b.Role) || !t.roleHeld(b.Role)
}

// roleHeld reports whether a player on the team holds role. The team as
// a whole, and a team whose holders are not known, holds every role.
func (t *Team) roleHeld(role string) bool {
	if t.Viewer == nil || t.RoleHolders == nil {
		return true
	}
	if role != t.G.DefaultRole() {
		_, ok := t.RoleHolders[role]
		return ok
	}
	// The default is everyone's who holds no other role.
	dealt := map[int64]bool{}
	for _, slot := range t.RoleHolders {
		dealt[slot] = true
	}
	return len(t.Players) <= 1 || len(dealt) < len(t.Players)
}
