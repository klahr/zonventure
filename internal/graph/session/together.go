package session

// Steps the players do together, each on their own phone: a two-key
// launch (graph.AnswerKeys), turned by enough of them at once, and a
// unanimous answer (graph.PuzzleConfig.Unanimous), given by all of them.
//
// What each player did is a Press, one per player on the current step.
// The engine loads them with the team and the players who are playing,
// and persists the one change an action makes (PressChanged); the rules
// stay here, with the rest.

import (
	"slices"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Press is one player's part on the current step. Value is the posts a
// key was turned at, joined by "|", or the route a unanimous answer
// leads by.
type Press struct {
	Value string
	At    time.Time
}

// PressChange is the press an action records for Slot on Node, or with
// a nil Press withdraws: a split team's on its group's step.
type PressChange struct {
	Node  string
	Slot  int64
	Press *Press
}

// Together is how far the players are with a step they do together:
// Done of Of keys turned, or players agreed, and whether the phone asking
// is among them. Until is when the first counted key's window closes,
// and it stops counting.
type Together struct {
	Kind  string     `json:"kind"` // "keys" or "agree"
	Done  int        `json:"done"`
	Of    int        `json:"of"`
	Mine  bool       `json:"mine,omitempty"`
	Until *time.Time `json:"until,omitempty"`
}

// playing is how many players the team has for a step done together:
// those the engine found playing, or, for the team as a whole, its size.
func (t *Team) playing() int {
	if t.Players != nil {
		return max(len(t.Players), 1)
	}
	return max(t.Size, 1)
}

// keysNeeded is how many keys a launch takes from this team: the author's
// number, or one per post, or every player — and never more players than
// it has, so a player alone opens it alone.
func (t *Team) keysNeeded(a *graph.Answer) int {
	k := a.Keys
	if k <= 0 {
		k = t.playing()
		if len(a.Posts) > 0 {
			k = t.postKeys(a)
		}
	}
	return max(1, min(k, t.playing()))
}

// postKeys is one key per post, but one per player for the posts of
// roles a single player holds: they cannot stand at two at once.
func (t *Team) postKeys(a *graph.Answer) int {
	k := 0
	holders := map[int64]bool{}
	for i := range a.Posts {
		slot, ok := t.postHolder(a, i)
		if !ok {
			k++
		} else if !holders[slot] {
			holders[slot] = true
			k++
		}
	}
	return k
}

// mayTurnAt reports whether slot's player may turn the key at post i:
// any player, unless the post is a role's and another player holds it.
func (t *Team) mayTurnAt(a *graph.Answer, i int, slot int64) bool {
	holder, ok := t.postHolder(a, i)
	return !ok || holder == slot
}

// postHolder is the player who alone turns the key at post i: the
// holder of its role, while they play and their phone says where it is.
// A role held by someone who has stopped playing, or whose phone sends
// no location (NoFix), would shut the team out, so the post is anyone's.
func (t *Team) postHolder(a *graph.Answer, i int) (int64, bool) {
	holder, ok := t.RoleHolders[a.PostRole(i)]
	if !ok || t.NoFix[holder] || (t.Players != nil && !slices.Contains(t.Players, holder)) {
		return 0, false
	}
	return holder, true
}

// postHeld reports whether post i has its player standing at it: the
// holder of its role, or anyone, where the post is anyone's.
func (t *Team) postHeld(a *graph.Answer, i int) bool {
	z := t.zoneBySlug(a.Posts[i])
	if z == nil {
		return false
	}
	if holder, ok := t.postHolder(a, i); ok && t.PhoneAt != nil {
		p, ok := t.PhoneAt[holder]
		return ok && ZoneContains(*z, p.Lat, p.Lng)
	}
	return t.InZone(z.Slug)
}

// postsAt are the launch's posts p stands at.
func (t *Team) postsAt(a *graph.Answer, p *Position) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, slug := range a.Posts {
		if z := t.zoneBySlug(slug); z != nil && ZoneContains(*z, p.Lat, p.Lng) {
			out = append(out, slug)
		}
	}
	return out
}

// keysTurned counts the playing players' keys turned within the window —
// each at a post of its own, where the launch has posts — and says when
// the first of them stops counting.
func (t *Team) keysTurned(a *graph.Answer) (int, *time.Time) {
	window := time.Duration(a.KeyWindow()) * time.Second
	since := t.Now.Add(-window)
	var at [][]bool // at[i][j]: key i was turned at post j
	var until *time.Time
	for _, slot := range t.Players {
		p, ok := t.pressesHere()[slot]
		if !ok || p.At.Before(since) {
			continue
		}
		posts := strings.Split(p.Value, "|")
		row := make([]bool, len(a.Posts))
		for j, post := range a.Posts {
			row[j] = slices.Contains(posts, post) && t.mayTurnAt(a, j, slot)
		}
		at = append(at, row)
		if u := p.At.Add(window); until == nil || u.Before(*until) {
			until = &u
		}
	}
	if len(a.Posts) == 0 {
		return len(at), until
	}
	return matching(at, len(a.Posts)), until
}

// Turn turns the asking player's key in a two-key launch, at the post
// they stand at if it has posts. The launch opens when enough keys are
// turned within the window; until then the result says how many are.
// For the team as a whole — a simulated team — every player turns at
// once, wherever each stands.
func (t *Team) Turn(node string) (Result, *walk.Step, error) {
	n, err := t.current(node)
	if err != nil {
		return Result{}, nil, err
	}
	cfg := n.PuzzleCfg
	if n.Type != graph.Puzzle || cfg == nil || cfg.Kind() != graph.AnswerKeys {
		return Result{}, nil, ErrWrongNode
	}
	if res, err := t.mayAnswer(n); err != nil {
		return res, nil, err
	}
	a := cfg.Answer
	need := t.keysNeeded(a)
	var done int
	if t.Viewer == nil {
		players := t.players()
		done = len(players)
		if len(a.Posts) > 0 {
			at := make([][]bool, len(players))
			for i, p := range players {
				at[i] = make([]bool, len(a.Posts))
				for _, post := range t.postsAt(a, &p) {
					at[i][slices.Index(a.Posts, post)] = true
				}
			}
			done = matching(at, len(a.Posts))
		}
	} else {
		if len(a.Posts) > 0 && t.Viewer.Position == nil {
			// Not "go to your post" to a player standing at it while the
			// phone finds itself.
			return Result{}, nil, ErrNoFix
		}
		posts := slices.DeleteFunc(t.postsAt(a, t.Viewer.Position), func(post string) bool {
			return !t.mayTurnAt(a, slices.Index(a.Posts, post), t.Viewer.Slot)
		})
		if len(a.Posts) > 0 && len(posts) == 0 {
			// A key is turned at a post, and a role's at its own: the
			// phone says to go to one.
			return Result{}, nil, ErrNotHere
		}
		t.setPress(t.Viewer.Slot, &Press{Value: strings.Join(posts, "|"), At: t.Now})
		done, _ = t.keysTurned(a)
	}
	if done < need {
		return Result{Waiting: &Together{Kind: "keys", Done: done, Of: need, Mine: t.Viewer != nil}}, nil, nil
	}
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

// agree records that the asking player gave a right answer to a unanimous
// puzzle, leading by port, and reports how many players have, if not yet
// all of them. Answering for the team as a whole, or alone, is all of it.
func (t *Team) agree(port string) (*Together, bool) {
	if t.Viewer == nil || len(t.Players) < 2 {
		return nil, true
	}
	t.setPress(t.Viewer.Slot, &Press{Value: port, At: t.Now})
	done := t.agreeing(port)
	if done < len(t.Players) {
		return &Together{Kind: "agree", Done: done, Of: len(t.Players), Mine: true}, false
	}
	return nil, true
}

// disagree withdraws the asking player's answer to a unanimous puzzle,
// after a wrong one.
func (t *Team) disagree() {
	if t.Viewer == nil {
		return
	}
	if _, ok := t.pressesHere()[t.Viewer.Slot]; ok {
		t.setPress(t.Viewer.Slot, nil)
	}
}

// agreeing counts the playing players whose answer leads by port.
func (t *Team) agreeing(port string) int {
	n := 0
	for _, slot := range t.Players {
		if p, ok := t.pressesHere()[slot]; ok && p.Value == port {
			n++
		}
	}
	return n
}

// pressesHere are what each player did on the current step: a split
// team's group's, focused.
func (t *Team) pressesHere() map[int64]Press { return t.Presses[t.Current] }

func (t *Team) setPress(slot int64, p *Press) {
	switch {
	case p == nil:
		delete(t.Presses[t.Current], slot)
	case t.Presses == nil:
		t.Presses = map[string]map[int64]Press{t.Current: {slot: *p}}
	case t.Presses[t.Current] == nil:
		t.Presses[t.Current] = map[int64]Press{slot: *p}
	default:
		t.Presses[t.Current][slot] = *p
	}
	t.PressChanged = &PressChange{Node: t.Current, Slot: slot, Press: p}
}

// together is how far the players are on puzzle n, for its view: nil
// when it is not done together, or the players are not known.
func (t *Team) together(n graph.Node) *Together {
	cfg := n.PuzzleCfg
	if t.Players == nil {
		return nil
	}
	mine := func(ok func(Press) bool) bool {
		if t.Viewer == nil {
			return false
		}
		p, has := t.pressesHere()[t.Viewer.Slot]
		return has && ok(p)
	}
	switch {
	case cfg.Kind() == graph.AnswerKeys:
		a := cfg.Answer
		done, until := t.keysTurned(a)
		since := t.Now.Add(-time.Duration(a.KeyWindow()) * time.Second)
		return &Together{Kind: "keys", Done: done, Of: t.keysNeeded(a), Until: until,
			Mine: mine(func(p Press) bool { return !p.At.Before(since) })}
	case cfg.Unanimous && len(t.Players) > 1:
		// The most players behind one way out: how close the team is,
		// without saying which.
		best := 0
		for _, o := range cfg.Outputs {
			best = max(best, t.agreeing(o.Port))
		}
		return &Together{Kind: "agree", Done: best, Of: len(t.Players), Mine: mine(func(Press) bool { return true })}
	}
	return nil
}

// matching is the most rows that can each be given a column of their
// own, where in[i][j] says row i may have column j: the plain
// augmenting-path search, which small teams and short lists make plenty.
func matching(in [][]bool, cols int) int {
	owner := make([]int, cols) // column → row holding it, -1 for none
	for j := range owner {
		owner[j] = -1
	}
	var claim func(i int, tried []bool) bool
	claim = func(i int, tried []bool) bool {
		for j := 0; j < cols; j++ {
			if !in[i][j] || tried[j] {
				continue
			}
			tried[j] = true
			if owner[j] < 0 || claim(owner[j], tried) {
				owner[j] = i
				return true
			}
		}
		return false
	}
	n := 0
	for i := range in {
		if claim(i, make([]bool, cols)) {
			n++
		}
	}
	return n
}
