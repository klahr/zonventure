package session

import (
	"slices"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Parallel paths: a split (graph.SplitConfig) divides a team into groups,
// each walking its own path to a join, where the last to arrive takes the
// team on together again.
//
// The team keeps one cursor per group — a Strand — and every rule in this
// package still reads the one cursor, Current. That is the trick: before
// a view or an action, the team is focused on one strand, which puts that
// strand's cursor in Current and narrows the players, their phones and
// the roles to the group's. Apply writes the cursor back. Nothing else
// had to learn that a team may be in several places at once, so a group
// answers, hints, times out and turns keys exactly as a team does.
//
// Everything else stays the team's: items, notes, points, answers. What
// one group finds, the other can use when they meet.

// Strand is one group's place on its path.
type Strand struct {
	// Port is the split's path the group took.
	Port      string    `json:"port"`
	Current   string    `json:"current"`
	EnteredAt time.Time `json:"entered_at"`
	Paused    int       `json:"paused,omitempty"`
	Walked    float64   `json:"walked,omitempty"`
	// Members are the players dealt to the group (Deal), in the order
	// they were. None for a team whose players are not known: then every
	// group is anyone's.
	Members []int64 `json:"members,omitempty"`
}

// GroupView is one group of a split team, as every phone sees it.
type GroupView struct {
	Port  string `json:"port"`
	Label string `json:"label"`
	// Node and At are where the group is: the step and its title.
	Node string `json:"node"`
	At   string `json:"at,omitempty"`
	// Arrived says the group waits at the join for the others.
	Arrived bool `json:"arrived,omitempty"`
	// Mine says the phone asking is with this group, and Current that
	// this view is the group's.
	Mine    bool `json:"mine,omitempty"`
	Current bool `json:"current,omitempty"`
	// Players is how many players are with it, where they are known.
	Players int `json:"players,omitempty"`
}

// focused is what focusing on a strand narrowed, to put back: nothing
// for a team whose players are not known, and the phones only where
// each phone's place is.
type focused struct {
	narrowed    bool
	phonesToo   bool
	players     []int64
	presses     map[int64]Press
	holders     map[string]int64
	phones      []Position
	position    *Position
	viewer      *Viewer
	viewerRoles []string
}

// Divided implements walk.State.
func (t *Team) Divided() bool { return t.Split != "" }

// JoinWaits implements walk.State: a join holds a group while any other
// group of the split is still on its way.
func (t *Team) JoinWaits(join string) bool {
	if t.Split == "" {
		return false
	}
	for i, st := range t.Strands {
		if i != t.focus-1 && st.Current != join {
			return true
		}
	}
	return false
}

// Whole is the node the team as a whole is at: where it split, while it
// is split, or else the one it is on. It is what the live engine stores
// as the team's node, and what the organisers see.
func (t *Team) Whole() string {
	if t.Split != "" {
		return t.Split
	}
	return t.Current
}

// Group is the path of the strand a view or action is for, or "".
func (t *Team) Group() string {
	if t.focus == 0 {
		return ""
	}
	return t.Strands[t.focus-1].Port
}

// beginSplit divides the team at a split, every group still on it, and
// deals its players among the groups where they are known.
func (t *Team) beginSplit(slug string) {
	n, ok := t.G.Node(slug)
	if !ok || n.SplitCfg == nil {
		return
	}
	t.Split = slug
	t.Strands = make([]Strand, len(n.SplitCfg.Paths))
	for i, p := range n.SplitCfg.Paths {
		t.Strands[i] = Strand{Port: p.Port, Current: slug, EnteredAt: t.Now, Paused: t.Paused}
	}
	if t.Players != nil {
		t.Deal(t.Players, t.RoleHolders)
	}
}

// endSplit makes the team one again, where its cursor stands.
func (t *Team) endSplit() {
	at := t.Current
	t.unfocus()
	t.Split, t.Strands, t.Current = "", nil, at
}

// Deal puts each of players not yet in a group into one: the group of
// each path whose role they hold, or, holding none, the group with
// fewest players, the first on a tie. holders is who holds each dealt
// role; a player holding none holds the default role, as does a player
// alone. Those already dealt stay where they are, so a player who joins
// mid-split changes no one else's group. It reports whether anyone was
// dealt, for the live engine to store.
func (t *Team) Deal(players []int64, holders map[string]int64) bool {
	if t.Split == "" || len(players) == 0 {
		return false
	}
	n, ok := t.G.Node(t.Split)
	if !ok || n.SplitCfg == nil || len(n.SplitCfg.Paths) != len(t.Strands) {
		return false
	}
	t.unfocus()
	dealt := map[int64]bool{}
	for _, st := range t.Strands {
		for _, m := range st.Members {
			dealt[m] = true
		}
	}
	holds := map[int64]bool{}
	for _, slot := range holders {
		holds[slot] = true
	}
	def := t.G.DefaultRole()
	changed := false
	var spare []int64
	for _, p := range players {
		if dealt[p] {
			continue
		}
		joined := false
		for i, path := range n.SplitCfg.Paths {
			mine := path.Role != "" && (path.Role == def && (!holds[p] || len(players) == 1) ||
				path.Role != def && holders[path.Role] == p)
			if mine {
				t.Strands[i].Members = append(t.Strands[i].Members, p)
				joined = true
			}
		}
		if joined {
			changed = true
		} else {
			spare = append(spare, p)
		}
	}
	for _, p := range spare {
		least := 0
		for i := range t.Strands {
			if t.liveCount(i, players) < t.liveCount(least, players) {
				least = i
			}
		}
		t.Strands[least].Members = append(t.Strands[least].Members, p)
		changed = true
	}
	return changed
}

// liveCount is how many of a group's members are among players.
func (t *Team) liveCount(i int, players []int64) int {
	n := 0
	for _, m := range t.Strands[i].Members {
		if slices.Contains(players, m) {
			n++
		}
	}
	return n
}

// everyone is the team's players whatever the focus: see Players.
func (t *Team) everyone() []int64 {
	if t.saved != nil {
		return t.saved.players
	}
	return t.Players
}

// live are a group's members still playing; all of them for a team
// whose players are not known.
func (t *Team) live(i int) []int64 {
	players := t.everyone()
	if players == nil {
		return t.Strands[i].Members
	}
	var out []int64
	for _, m := range t.Strands[i].Members {
		if slices.Contains(players, m) {
			out = append(out, m)
		}
	}
	return out
}

// strandsOf are the groups slot walks, in the order it walks them: its
// own, then any no one playing is with — anyone's to walk.
func (t *Team) strandsOf(slot int64) []int {
	var own, anyone []int
	for i := range t.Strands {
		live := t.live(i)
		switch {
		case slices.Contains(live, slot):
			own = append(own, i)
		case len(live) == 0:
			anyone = append(anyone, i)
		}
	}
	return append(own, anyone...)
}

// arrived reports whether a group waits at its join.
func (t *Team) arrived(i int) bool {
	n, ok := t.G.Node(t.Strands[i].Current)
	return ok && n.Type == graph.Join
}

// walking is the first of the groups not yet at the join, or else the
// first of them; -1 for none.
func (t *Team) walking(groups []int) int {
	for _, i := range groups {
		if !t.arrived(i) {
			return i
		}
	}
	if len(groups) > 0 {
		return groups[0]
	}
	return -1
}

// candidates are the groups a view or action may be for: the phone
// asking's, or every group for the team as a whole.
func (t *Team) candidates() []int {
	if t.Viewer != nil {
		if mine := t.strandsOf(t.Viewer.Slot); len(mine) > 0 {
			return mine
		}
	}
	all := make([]int, len(t.Strands))
	for i := range all {
		all[i] = i
	}
	return all
}

// Focus puts the team on the group a view is for, if it is split: the
// first of the phone's groups still walking — or, for the team as a
// whole, the first group still walking.
func (t *Team) Focus() {
	if t.Split == "" {
		return
	}
	if i := t.walking(t.candidates()); i >= 0 {
		t.focusOn(i)
	}
}

// focusFor puts the team on the group an action on node is for: one of
// the phone's at node. Another group's step is not the phone's to act
// on; a step no group is at leaves the default focus, and the action is
// stale.
func (t *Team) focusFor(node string) error {
	if t.focus > 0 && t.Strands[t.focus-1].Current == node && slices.Contains(t.candidates(), t.focus-1) {
		return nil
	}
	for _, i := range t.candidates() {
		if t.Strands[i].Current == node {
			t.focusOn(i)
			return nil
		}
	}
	for _, st := range t.Strands {
		if st.Current == node {
			return ErrNotYours
		}
	}
	t.Focus()
	return nil
}

// strandPlayers are the players working on group i now: those whose
// first group still walking it is. Nil when players are not known.
func (t *Team) strandPlayers(i int) []int64 {
	players := t.everyone()
	if players == nil {
		return nil
	}
	out := []int64{}
	for _, p := range players {
		if t.walking(t.strandsOf(p)) == i {
			out = append(out, p)
		}
	}
	return out
}

// focusOn puts group i's cursor in Current, and narrows the players,
// their phones and the roles to the group's: a step done together waits
// for the group, a place is reached by one of its players, and a role
// no one in the group holds is everyone's in it — as a player alone
// holds every role.
func (t *Team) focusOn(i int) {
	t.unfocus()
	st := t.Strands[i]
	group := t.strandPlayers(i)
	t.saved = &focused{players: t.Players, presses: t.Presses, holders: t.RoleHolders, phones: t.Phones, position: t.Position, viewer: t.Viewer}
	if t.Viewer != nil {
		t.saved.viewerRoles = t.Viewer.Roles
	}
	t.focus = i + 1
	t.Current, t.NodeEnteredAt, t.NodePaused, t.WalkedMetres = st.Current, st.EnteredAt, st.Paused, st.Walked
	if w, ok := t.Walks[st.Port]; ok {
		t.WalkedMetres = w
	}
	if t.PressesAt != nil {
		t.Presses = t.PressesAt[st.Current]
	}
	if group == nil {
		return
	}
	t.saved.narrowed = true
	t.Players = group
	if t.RoleHolders != nil {
		t.RoleHolders = map[string]int64{}
		for role, slot := range t.saved.holders {
			if slices.Contains(group, slot) {
				t.RoleHolders[role] = slot
			}
		}
	}
	if t.Viewer != nil && t.saved.holders != nil {
		roles := slices.Clone(t.Viewer.Roles)
		for _, role := range t.G.DealtRoles() {
			if _, held := t.RoleHolders[role]; !held && !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
		t.Viewer.Roles = roles
	}
	if t.PhoneAt != nil {
		t.saved.phonesToo = true
		t.Phones = []Position{}
		for _, p := range group {
			if at, ok := t.PhoneAt[p]; ok {
				t.Phones = append(t.Phones, at)
			}
		}
		if len(t.Phones) == 0 {
			t.Position = nil
		}
	}
}

// Unfocus puts back what focusing narrowed, and the team's own cursor
// while it is split: for a caller about to change the team's players,
// phones or roles between one phone's view and the next.
func (t *Team) Unfocus() { t.unfocus() }

func (t *Team) unfocus() {
	if t.saved != nil {
		if t.saved.narrowed {
			t.Players, t.RoleHolders = t.saved.players, t.saved.holders
			if t.saved.viewer != nil {
				t.saved.viewer.Roles = t.saved.viewerRoles
			}
		}
		if t.saved.phonesToo {
			t.Phones, t.Position = t.saved.phones, t.saved.position
		}
		if t.PressesAt != nil {
			t.Presses = t.saved.presses
		}
		t.saved = nil
	}
	if t.focus > 0 && t.Split != "" {
		// How far the group has walked on its step, as the simulator adds it.
		t.Strands[t.focus-1].Walked = t.WalkedMetres
		t.Current = t.Split
	}
	t.focus = 0
}

// applySplit follows a step into, along or out of a split: Apply's part.
func (t *Team) applySplit(step walk.Step) {
	switch {
	case t.Split != "" && (step.Finished || !step.Held && t.passedJoin(step)):
		// Together again — or the adventure is over, for every group.
		t.endSplit()
	case t.focus > 0:
		st := &t.Strands[t.focus-1]
		st.Current, st.EnteredAt, st.Paused, st.Walked = t.Current, t.NodeEnteredAt, t.NodePaused, 0
		if step.Held {
			// The group waits; its phones go on to any other group of theirs.
			t.unfocus()
		}
	case t.Split == "" && !step.Finished:
		if n, ok := t.G.Node(step.To); ok && n.Type == graph.Split {
			t.beginSplit(step.To)
		}
	}
}

func (t *Team) passedJoin(step walk.Step) bool {
	for _, slug := range step.Passed {
		if n, ok := t.G.Node(slug); ok && n.Type == graph.Join {
			return true
		}
	}
	return false
}

// leaveSplit sets the focused group off down its path.
func (t *Team) leaveSplit(n graph.Node) (Result, *walk.Step, error) {
	if t.focus == 0 || t.Split != n.Slug {
		return Result{}, nil, ErrWrongNode
	}
	port := t.Strands[t.focus-1].Port
	t.Row(n.Slug)
	step, err := walk.Advance(t.G, t, n.Slug, port)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Port: port}, &step, nil
}

// groups is every group of a split team, for its view.
func (t *Team) groups() []GroupView {
	n, _ := t.G.Node(t.Split)
	var mine []int
	if t.Viewer != nil {
		mine = t.strandsOf(t.Viewer.Slot)
	}
	out := make([]GroupView, len(t.Strands))
	for i, st := range t.Strands {
		g := GroupView{Port: st.Port, Node: st.Current, Arrived: t.arrived(i), Mine: slices.Contains(mine, i), Current: t.focus == i+1}
		if p, ok := n.Path(st.Port); ok {
			g.Label = t.pathLabel(p)
		}
		if at, ok := t.G.Node(st.Current); ok {
			g.At = t.G.Tr(t.lang(), graph.NodeKey(at.Slug, "title"), at.Title)
		}
		if t.everyone() != nil {
			g.Players = len(t.live(i))
		}
		out[i] = g
	}
	return out
}

// pathLabel is a path's name in the view's language.
func (t *Team) pathLabel(p graph.SplitPath) string {
	if p.Label == "" && p.Role != "" {
		return t.G.Tr(t.lang(), graph.RoleKey(p.Role, "name"), t.G.RoleName(p.Role))
	}
	return t.G.Tr(t.lang(), graph.NodeKey(t.Split, "path:"+p.Port+":label"), t.G.PathLabel(p))
}
