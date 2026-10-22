package session

import (
	"slices"

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
//
// Splits do not nest (valid refuses a split on a path): a group is its
// path, and the team is split at one split at a time.

// Strand is one group's place on its path.
type Strand struct {
	// Port is the path the group took at the team's split, and Cursor
	// where the group is on it.
	Port string `json:"port"`
	Cursor
	// Members are the players dealt to the group (Deal), in the order
	// they were. None for a team whose players are not known: then every
	// group is anyone's.
	Members []int64 `json:"members,omitempty"`
	// Chosen are the players who chose to walk this group of theirs
	// (Walk) rather than their first still walking; 0 is the team as a
	// whole. Forgotten as the group arrives at its join.
	Chosen []int64 `json:"chosen,omitempty"`
}

// SetOff reports whether any group of a team split at split has left it:
// until one has, the team stands at its split together, its players
// dealt into groups but none of them out.
func SetOff(split string, strands []Strand) bool {
	return slices.ContainsFunc(strands, func(st Strand) bool { return st.Current != split })
}

// GroupView is one group of a split team, as every phone sees it.
type GroupView struct {
	// Port is the group's path, and Label its name: the path's, "Church
	// party".
	Port  string `json:"port"`
	Label string `json:"label"`
	// Node and At are where the group is: the step and its title.
	Node string `json:"node"`
	At   string `json:"at,omitempty"`
	// Arrived says the group waits at the join for the others.
	Arrived bool `json:"arrived,omitempty"`
	// Mine says the phone asking is with this group, and Current that
	// this view is the group's. Open says the group is anyone's to walk
	// (Walk): no one playing is with it, or every one of them is away.
	Mine    bool `json:"mine,omitempty"`
	Open    bool `json:"open,omitempty"`
	Current bool `json:"current,omitempty"`
	// Moved says the organisers moved it onto its step, and MovedByOther
	// another phone than the one asking did (Cursor) — whose is no one's
	// to know.
	Moved        bool `json:"moved,omitempty"`
	MovedByOther bool `json:"moved_by_other,omitempty"`
	// Players is how many players are with it, where they are known.
	Players int `json:"players,omitempty"`
}

// focused is what focusing on a strand narrowed, to put back: nothing
// for a team whose players are not known, and the phones only where
// each phone's place is.
type focused struct {
	narrowed  bool
	phonesToo bool
	players   []int64
	holders   map[string]int64
	phones    []Position
	position  *Position
	// viewer is the caller's, left as it was: the group's roles go on a
	// copy of it.
	viewer *Viewer
	// home is the team's own cursor, at the split.
	home Cursor
}

// Splits are t.G's splits, worked out as needed, once per graph: a team
// handed another graph works them out again.
func (t *Team) Splits() *graph.Splits {
	if t.splitsOf != t.G {
		t.splitsCache, t.splitsOf = t.G.Splits(), t.G
	}
	return t.splitsCache
}

// ShareSplits gives the team its graph's splits already worked out
// (graph.Splits.Warm), shared by every team on the graph rather than
// worked out again for each: for a graph no one changes. Splits of
// another graph are ignored.
func (t *Team) ShareSplits(sp *graph.Splits) {
	if sp != nil && sp.Graph() == t.G {
		t.splitsCache, t.splitsOf = sp, t.G
	}
}

// joinOf is the join split's paths meet at: "" for none.
func (t *Team) joinOf(split string) string {
	j, _ := t.Splits().JoinOf(split)
	return j
}

// meets reports whether join is where the focused group meets the
// others: its split's join.
func (t *Team) meets(join string) bool {
	return t.Split != "" && t.focus > 0 && t.joinOf(t.Split) == join
}

// JoinWaits implements walk.State: a join holds a group while another
// group of the split is still on its way.
func (t *Team) JoinWaits(join string) bool {
	return t.awaited(join) > 0
}

// awaited is how many of the focused group's split's other groups are
// still on their way to join.
func (t *Team) awaited(join string) int {
	if !t.meets(join) {
		return 0
	}
	n := 0
	for i, st := range t.Strands {
		if i != t.focus-1 && st.Current != join {
			n++
		}
	}
	return n
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
		// Each group come to the split by the move the team came by.
		t.Strands[i] = Strand{Port: p.Port, Cursor: Cursor{Current: slug, NodeEnteredAt: t.Now, NodePaused: t.Paused, Moved: t.Moved, MovedBy: t.MovedBy}}
	}
	if t.Players != nil {
		t.Deal(t.Players, t.RoleHolders)
	}
}

// SplitFits reports whether a team's stored groups still fit its graph:
// the split is there, and each group is on a step of it, one group to
// each of its paths, since a time it came there. A path added since has
// no group, and no one would be dealt into it; a group with no time —
// stored in another shape — would have its clocks run out at once: the
// team splits afresh.
func SplitFits(splits *graph.Splits, split string, strands []Strand) bool {
	n, ok := splits.Graph().Node(split)
	if !ok || n.Type != graph.Split || n.SplitCfg == nil || len(strands) != len(n.SplitCfg.Paths) {
		return false
	}
	paths := splits.Paths(split)
	join, _ := splits.JoinOf(split)
	ports := map[string]bool{}
	for _, st := range strands {
		if _, ok := n.Path(st.Port); !ok || ports[st.Port] || st.NodeEnteredAt.IsZero() {
			return false
		}
		ports[st.Port] = true
		// On its own path, or the split, or the join: not a step since
		// wired to another path, or past the join. Or on a split its path
		// leads to — a draft's, which valid refuses (split.nested) — where
		// it waits, rather than every group split afresh on each load.
		if st.Current != split && st.Current != join && !slices.Contains(paths.Steps[st.Port], st.Current) && !slices.Contains(paths.Nested[st.Port], st.Current) {
			return false
		}
	}
	return true
}

// Refit splits the team afresh where its groups no longer fit its graph
// — one edited under it: a dev team's, the designer preview's — or where
// it stands, together, on a step since made a split (Refits). It reports
// whether it did, for the host to store.
func (t *Team) Refit() bool {
	if Refits(t.Splits(), t.Current, t.Split, t.Strands) {
		t.SplitAfresh()
		return true
	}
	return false
}

// Refits reports whether a team stored on current, split at split into
// strands ("" and none for a team together), is split afresh as it is
// loaded (Refit).
func Refits(splits *graph.Splits, current, split string, strands []Strand) bool {
	if split != "" {
		return !SplitFits(splits, split, strands)
	}
	n, _ := splits.Node(current)
	return n.Type == graph.Split
}

// SplitAfresh divides the team again where it stands, as on arriving,
// its groups' places forgotten — for a team whose groups no longer fit
// its graph: on a split, every group on it; anywhere else, one team;
// on a split taken out of the graph, one team at its start screen.
func (t *Team) SplitAfresh() {
	t.unfocus()
	// Split afresh is no move: no one moved the groups onto the split.
	c := t.Cursor
	c.Moved, c.MovedBy = false, 0
	t.splitAfreshAt(c)
}

// splitAfreshAt is SplitAfresh with the team's cursor at c: set once the
// team is whole, or unfocusing would put it back on the split.
func (t *Team) splitAfreshAt(c Cursor) {
	t.unfocus()
	t.Split, t.Strands = "", nil
	t.Cursor = c
	n, ok := t.G.Node(t.Current)
	switch {
	case !ok:
		// The split itself is gone from the graph: the team is at its
		// start screen, to begin again.
		t.Current = ""
	case n.Type == graph.Split:
		t.beginSplit(t.Current)
		// On it since the team came there, not since now: the groups'
		// clocks are where they would have been, and a team read again
		// before it is stored splits afresh the same.
		if !t.NodeEnteredAt.IsZero() {
			for i := range t.Strands {
				t.Strands[i].NodeEnteredAt, t.Strands[i].NodePaused = t.NodeEnteredAt, t.NodePaused
			}
		}
	}
}

// endSplit makes the team one again, where its cursor stands and since
// its cursor came there.
func (t *Team) endSplit() {
	at := t.Cursor
	t.unfocus()
	t.Split, t.Strands, t.Cursor = "", nil, at
}

// groupSteps are where the groups are, in their order.
func (t *Team) groupSteps() []string {
	out := make([]string, len(t.Strands))
	for i, st := range t.Strands {
		out[i] = st.Current
	}
	return out
}

// Deal puts each of players not yet in a group into one: the path whose
// role they hold, or, holding none, the group with fewest players, the
// first on a tie. holders is who holds each dealt role; a player holding none holds
// the default role, as does a player alone. Those already dealt stay
// where they are, so a player who joins mid-split changes no one else's
// group. A choice of a group the chooser may no longer walk — its own
// players back from away — is dropped (dropStale), for Away as it is
// now.
func (t *Team) Deal(players []int64, holders map[string]int64) {
	if t.Split == "" || len(players) == 0 || len(t.Strands) == 0 {
		return
	}
	t.unfocus()
	t.dropStale(players)
	if fresh := Undealt(t.Strands, players); len(fresh) > 0 {
		t.dealAt(fresh, holders, players)
	}
}

// Undealt are the players in no group yet, in their order.
func Undealt(strands []Strand, players []int64) []int64 {
	dealt := map[int64]bool{}
	for _, st := range strands {
		for _, m := range st.Members {
			dealt[m] = true
		}
	}
	var out []int64
	for _, p := range players {
		if !dealt[p] {
			out = append(out, p)
		}
	}
	return out
}

// dropStale forgets each player's choice of a group that is no longer
// one they may walk, among players those playing: chosen while it was
// anyone's, its own players since back. The choice is spent — the
// player went back to their own group — and the group going quiet again
// later does not take the phone back to it unasked. The team as a
// whole's choice stands.
func (t *Team) dropStale(players []int64) {
	r := t.rosterOf(players)
	for i := range t.Strands {
		chosen := t.Strands[i].Chosen
		if len(chosen) == 0 {
			continue
		}
		t.Strands[i].Chosen = slices.DeleteFunc(chosen, func(c int64) bool {
			return c != 0 && !slices.Contains(r.strands(c), i)
		})
		if len(t.Strands[i].Chosen) == 0 {
			t.Strands[i].Chosen = nil
		}
	}
}

// actsOn notes that the phone asking acts on group i: a choice it made
// of another of its groups is spent, as it is back with this one; and
// where it would still walk another first, it is taken to choose this
// one, so that it is among the players acting. It reports whether the
// choices changed, for the focus to be put on the group again, the phone
// now among its players.
func (t *Team) actsOn(i int) bool {
	if t.Viewer == nil {
		return false
	}
	slot := t.Viewer.Slot
	if slices.Contains(t.Strands[i].Chosen, slot) {
		return false
	}
	changed := false
	for k := range t.Strands {
		if k == i || !slices.Contains(t.Strands[k].Chosen, slot) {
			continue
		}
		t.Strands[k].Chosen = slices.DeleteFunc(t.Strands[k].Chosen, func(s int64) bool { return s == slot })
		if len(t.Strands[k].Chosen) == 0 {
			t.Strands[k].Chosen = nil
		}
		changed = true
	}
	if t.walking(slot, t.rosterOf(t.everyone()).strands(slot)) != i {
		t.Strands[i].Chosen = append(t.Strands[i].Chosen, slot)
		changed = true
	}
	return changed
}

// dealAt deals players among the groups, by the paths of the split.
// live are the players counted as in a group, for which has fewest.
func (t *Team) dealAt(players []int64, holders map[string]int64, live []int64) {
	n, ok := t.G.Node(t.Split)
	if len(players) == 0 || !ok || n.SplitCfg == nil {
		return
	}
	paths := n.SplitCfg.Paths
	// The group of each path; -1 for a path with none.
	group := make([]int, len(paths))
	for x, p := range paths {
		group[x] = slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Port == p.Port })
	}
	holds := map[int64]bool{}
	for _, slot := range holders {
		holds[slot] = true
	}
	// A group waiting at the join takes no one more while another is on
	// its way — its role's holder no more than anyone: a newcomer there
	// would only wait with it.
	walking := false
	for x := range paths {
		walking = walking || group[x] >= 0 && !t.arrived(group[x])
	}
	open := func(x int) bool { return group[x] >= 0 && (!walking || !t.arrived(group[x])) }
	def := t.G.DefaultRole()
	assigned := make([][]int64, len(paths))
	var spare []int64
	for _, p := range players {
		joined := false
		for x, path := range paths {
			mine := path.Role != "" && (path.Role == def && (!holds[p] || len(live) == 1) ||
				path.Role != def && holders[path.Role] == p)
			if mine && open(x) {
				assigned[x] = append(assigned[x], p)
				joined = true
			}
		}
		if !joined {
			spare = append(spare, p)
		}
	}
	// How many each path's group has, counting those assigned so far.
	count := make([]int, len(paths))
	for x := range paths {
		count[x] = len(assigned[x]) + t.liveCount(group[x], live)
	}
	for _, p := range spare {
		least := -1
		for x := range paths {
			if open(x) && (least < 0 || count[x] < count[least]) {
				least = x
			}
		}
		if least < 0 {
			break
		}
		assigned[least] = append(assigned[least], p)
		count[least]++
	}
	for x := range paths {
		if group[x] < 0 {
			continue
		}
		st := &t.Strands[group[x]]
		for _, p := range assigned[x] {
			if !slices.Contains(st.Members, p) {
				st.Members = append(st.Members, p)
			}
		}
	}
}

// liveCount is how many of a group's members are among players: none
// for no group.
func (t *Team) liveCount(i int, players []int64) int {
	if i < 0 {
		return 0
	}
	n := 0
	for _, m := range t.Strands[i].Members {
		if slices.Contains(players, m) {
			n++
		}
	}
	return n
}

// Everyone is the team's players whatever the focus — not only the
// group's it is focused on: nil for the team as a whole.
func (t *Team) Everyone() []int64 { return t.everyone() }

// everyone is the team's players whatever the focus: see Players.
func (t *Team) everyone() []int64 {
	if t.saved != nil {
		return t.saved.players
	}
	return t.Players
}

// roster is, for players those playing, who of them is in each group
// (live: every member, for players not known) and which groups are
// anyone's to walk (open): no one is left in it, or every one of them is
// away — a phone flat, the app shut — until one is back. Worked out once
// for a pass over the players.
type roster struct {
	live [][]int64
	open []bool
}

func (t *Team) rosterOf(players []int64) roster {
	r := roster{live: make([][]int64, len(t.Strands)), open: make([]bool, len(t.Strands))}
	var playing map[int64]bool
	if players != nil {
		playing = make(map[int64]bool, len(players))
		for _, p := range players {
			playing[p] = true
		}
	}
	for i, st := range t.Strands {
		live := st.Members
		if playing != nil {
			live = nil
			for _, m := range st.Members {
				if playing[m] {
					live = append(live, m)
				}
			}
		}
		r.live[i] = live
		r.open[i] = !slices.ContainsFunc(live, func(m int64) bool { return !t.Away[m] })
	}
	return r
}

// strands are the groups slot walks, in the order it walks them: its
// own, then any that are anyone's.
func (r roster) strands(slot int64) []int {
	var own, anyone []int
	for i, live := range r.live {
		switch {
		case slices.Contains(live, slot):
			own = append(own, i)
		case r.open[i]:
			anyone = append(anyone, i)
		}
	}
	return append(own, anyone...)
}

// walks reports whether player p, walking group i, is counted as one of
// its walkers: not when p is away and the group is anyone's for it — its
// own group, every one of whose players is away. Those walking it are
// its walkers, and a role its away holder has is everyone's in it, until
// one of them is back.
func (t *Team) walks(r roster, p int64, i int) bool {
	return !t.Away[p] || !r.open[i]
}

// arrived reports whether a group waits at its split's join.
func (t *Team) arrived(i int) bool {
	join := t.joinOf(t.Split)
	return join != "" && t.Strands[i].Current == join
}

// walking is the one of groups slot walks: the one it chose, while
// that is still on its way, or else the first not yet at the join, or
// else the first of them; -1 for none.
func (t *Team) walking(slot int64, groups []int) int {
	for _, i := range groups {
		if slices.Contains(t.Strands[i].Chosen, slot) && !t.arrived(i) {
			return i
		}
	}
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
		if mine := t.rosterOf(t.everyone()).strands(t.Viewer.Slot); len(mine) > 0 {
			return mine
		}
	}
	all := make([]int, len(t.Strands))
	for i := range all {
		all[i] = i
	}
	return all
}

// focusView puts the team on the group a view is for, if it is split: the
// first of the phone's groups still walking — or, for the team as a
// whole, the first group still walking.
func (t *Team) focusView() {
	if t.Split == "" {
		return
	}
	if i := t.walking(t.asking(), t.candidates()); i >= 0 {
		t.focusOn(i)
	}
}

// Step is the cursor of the group a view is for (focusView) — the team's
// own, when it is not split — for a caller following the team step by
// step. Not between an action and its Apply, which the focus is for.
func (t *Team) Step() Cursor {
	if t.Split == "" {
		return t.Cursor
	}
	t.focusView()
	defer t.unfocus()
	return t.Cursor
}

// asking is the slot of the phone asking: 0 for the team as a whole.
func (t *Team) asking() int64 {
	if t.Viewer != nil {
		return t.Viewer.Slot
	}
	return 0
}

// Walk is a phone's choice of which of its groups it walks — any of
// them still on its way, its own or one no one is left in — from now
// until the group is at its join: a player in two groups need not finish
// the first before the second, which may be finding what the first
// needs. For the team as a whole, any group. A group met at its join,
// or gone, is stale; another's is not the phone's.
func (t *Team) Walk(key string) (Result, *walk.Step, error) {
	if t.Completed {
		return Result{}, nil, ErrFinished
	}
	if t.Split == "" {
		return Result{}, nil, ErrStale
	}
	t.unfocus()
	i := slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Port == key })
	if i < 0 || t.arrived(i) {
		return Result{}, nil, ErrStale
	}
	if !slices.Contains(t.candidates(), i) {
		return Result{}, nil, ErrOtherGroup
	}
	slot := t.asking()
	for k := range t.Strands {
		t.Strands[k].Chosen = slices.DeleteFunc(t.Strands[k].Chosen, func(s int64) bool { return s == slot })
	}
	t.Strands[i].Chosen = append(t.Strands[i].Chosen, slot)
	return Result{Accepted: true}, nil, nil
}

// focusFor puts the team on the group an action on node is for: one of
// the phone's at node. Another group's step is not the phone's to act
// on; any other step leaves the default focus, and the action is stale.
func (t *Team) focusFor(node string) error {
	n, known := t.Splits().Node(node)
	cands := t.candidates()
	if known && n.Type == graph.Split {
		if t.Viewer != nil {
			// A phone's press on a split sets off the group it is walking,
			// and only that: pressed again, after the group set off, it is
			// a repeat — stale — not a press for its next group, or an
			// empty one. That group's turn comes when its phones see it.
			t.focusView()
			return nil
		}
		// The team as a whole sets off the group it chose to walk, while
		// that still stands on the split; else the first that does. A
		// group it is already on that stands there — the one an
		// organiser's rescue names (RescueGroup) — is the one it sets off.
		if t.focus == 0 || t.Strands[t.focus-1].Current != node {
			for _, i := range cands {
				if slices.Contains(t.Strands[i].Chosen, 0) && t.Strands[i].Current == node && !t.arrived(i) {
					t.focusOn(i)
					return nil
				}
			}
		}
	}
	// An action on a step of one of the phone's groups is on that group:
	// a choice of another is spent (actsOn). The join is every group's,
	// and spends none.
	own := true
	if known && n.Type == graph.Join {
		own = false
	}
	if i := t.focus - 1; i >= 0 && t.Strands[i].Current == node && slices.Contains(cands, i) {
		if own && t.actsOn(i) {
			t.focusOn(i)
		}
		return nil
	}
	for _, i := range cands {
		if t.Strands[i].Current == node {
			if own {
				t.actsOn(i)
			}
			t.focusOn(i)
			return nil
		}
	}
	// A step on another group's path is not the phone's. The split and the
	// join are every group's: a phone pressing to set off after its group
	// already has is only late, and the action is stale.
	if known && n.Type != graph.Split && n.Type != graph.Join {
		for _, st := range t.Strands {
			if st.Current == node {
				return ErrOtherGroup
			}
		}
	}
	t.focusView()
	return nil
}

// strandPlayers are the players working on group i now: those whose
// first group still walking it is, but for its own players away while it
// is anyone's. Nil when players are not known.
func (t *Team) strandPlayers(i int) []int64 {
	players := t.everyone()
	if players == nil {
		return nil
	}
	return t.Walkers(players)[t.Strands[i].Port]
}

// Walkers are the players walking each group now, by its port: each of
// players on the first of their groups still walking — their own, or
// one no one among players is with — but for a group's own players away
// while it is anyone's. A group no one walks has none.
func (t *Team) Walkers(players []int64) map[string][]int64 {
	out := make(map[string][]int64, len(t.Strands))
	for _, st := range t.Strands {
		out[st.Port] = []int64{}
	}
	r := t.rosterOf(players)
	for _, p := range players {
		if i := t.walking(p, r.strands(p)); i >= 0 && t.walks(r, p, i) {
			k := t.Strands[i].Port
			out[k] = append(out[k], p)
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
	t.saved = &focused{players: t.Players, holders: t.RoleHolders, phones: t.Phones, position: t.Position, viewer: t.Viewer,
		home: t.Cursor}
	t.focus = i + 1
	t.Cursor = st.Cursor
	if w, ok := t.Walks[st.Port]; ok {
		t.WalkedMetres = w
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
		// The default is everyone else's — and a player alone's, as
		// play's locate deals it — so in a group whose players all hold
		// another role, or of one, it is everyone's in it.
		if d := t.G.DefaultRole(); d != "" && !slices.Contains(roles, d) {
			dealt := map[int64]bool{}
			for _, slot := range t.RoleHolders {
				dealt[slot] = true
			}
			if len(group) <= 1 || len(dealt) >= len(group) {
				roles = append(roles, d)
			}
		}
		v := *t.Viewer
		v.Roles = roles
		t.Viewer = &v
	}
	if t.PhoneAt != nil {
		t.saved.phonesToo = true
		t.Phones = []Position{}
		for _, p := range group {
			if at, ok := t.PhoneAt[p]; ok {
				t.Phones = append(t.Phones, at)
			}
		}
		// Where the group is: the phone asking's, if it is the group's,
		// else one of its phones — never another group's latest fix.
		switch {
		case len(t.Phones) == 0:
			t.Position = nil
		case t.Viewer != nil && t.Viewer.Position != nil && slices.Contains(group, t.Viewer.Slot):
			at := *t.Viewer.Position
			t.Position = &at
		default:
			at := t.Phones[0]
			t.Position = &at
		}
	}
}

// Joined implements walk.State: the walk has taken the focused group
// through its join, so from there it is the team's — its players, their
// phones and roles — though Apply still moves the group that walked.
func (t *Team) Joined(join string) {
	if t.meets(join) {
		t.widen()
	}
}

// widen puts back what focusing narrowed — the players, their phones and
// roles — but not the cursor.
func (t *Team) widen() {
	if t.saved == nil {
		return
	}
	if t.saved.narrowed {
		t.Players, t.RoleHolders, t.Viewer = t.saved.players, t.saved.holders, t.saved.viewer
		t.saved.narrowed = false
	}
	if t.saved.phonesToo {
		t.Phones, t.Position = t.saved.phones, t.saved.position
		t.saved.phonesToo = false
	}
}

// AddWalk adds metres walked by the phone slot to what is walked on the
// step — while the team is split up, by the group the slot walks
// (Walkers), or, for no slot or one walking none, the group the team's
// own view is for (focusView): for the simulators, which move the team
// themselves.
func (t *Team) AddWalk(slot int64, metres float64) {
	t.unfocus()
	if t.Split == "" {
		t.WalkedMetres += metres
		return
	}
	i := -1
	if slot != 0 {
		for port, slots := range t.Walkers(t.everyone()) {
			if slices.Contains(slots, slot) {
				i = slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Port == port })
			}
		}
	}
	if i < 0 {
		i = t.walking(0, t.candidates())
	}
	if i >= 0 {
		t.Strands[i].WalkedMetres += metres
	}
}

// unfocus puts back what focusing narrowed, and the team's own cursor
// while it is split.
func (t *Team) unfocus() {
	if t.saved != nil {
		t.widen()
		if t.focus > 0 && t.Split != "" {
			// The group's cursor, as the focus moved it — but for a walk
			// read from its phones (Walks), read again each time.
			st := &t.Strands[t.focus-1]
			c := t.Cursor
			if _, read := t.Walks[st.Port]; read {
				c.WalkedMetres = st.WalkedMetres
			}
			st.Cursor = c
		}
		// The team's own: at the split since it came there, having
		// walked none of its own since.
		t.Cursor = t.saved.home
		t.saved = nil
	}
	if t.Split != "" {
		// Also for a team decoded as it was sent, focused on a group: the
		// focus does not travel, the team's own cursor does.
		t.Current = t.Split
	}
	t.focus = 0
}

// applySplit follows a step into, along or out of a split: Apply's part.
// One step may do several: the last group through the join takes the
// team on, which may walk straight on into another split.
func (t *Team) applySplit(step walk.Step) {
	if t.Split != "" && step.Finished {
		// The adventure is over, for every group.
		t.endSplit()
		return
	}
	if t.focus > 0 {
		i := t.focus - 1
		// Through the join, where it met its siblings.
		join := t.joinOf(t.Split)
		for k, slug := range step.Passed {
			if step.Held && k == len(step.Passed)-1 {
				break
			}
			if slug == join {
				t.endSplit()
				break
			}
		}
		if t.Split != "" {
			if step.Held {
				// The group waits; its phones go on to any other group of
				// theirs, the choice of it forgotten.
				t.Strands[i].Chosen = nil
				t.unfocus()
			}
			return
		}
	}
	if t.Split == "" && !step.Finished {
		if n, ok := t.G.Node(step.To); ok && n.Type == graph.Split {
			t.beginSplit(step.To)
		}
	}
}

// onPath reports whether node is a step of the path of the group the
// team is focused on.
func (t *Team) onPath(node string) bool {
	if t.focus == 0 {
		return false
	}
	return slices.Contains(t.Splits().Paths(t.Split).Steps[t.Strands[t.focus-1].Port], node)
}

// RescueGroup is Rescue for one group of a split team — the group with
// key, which must stand on node, or with node "" wherever it stands;
// with neither, the team on its own step. Several groups may wait on one
// split, and the organisers move the one they mean: the team's split
// names one only where one alone is left on it. A key for a team no
// longer split, or for no group of it, is stale.
func (t *Team) RescueGroup(node, key string) (Result, *walk.Step, error) {
	// Act marks where it takes the team, for its phones (Cursor.Moved).
	t.rescuing = true
	if t.Completed {
		return t.Rescue(node)
	}
	if t.Split == "" {
		switch {
		case key != "":
			// The groups met, or the team was put elsewhere, since the
			// organisers looked: the group is gone, not the team to move.
			return Result{}, nil, ErrStale
		case node == "":
			// The team's own step — a start screen is none to move past.
			if node = t.Current; node == "" {
				return Result{}, nil, ErrWrongNode
			}
		}
		return t.Rescue(node)
	}
	if key == "" {
		// A group's own step says whose it is; the team's own — its split,
		// or none named — says only where one group alone is on it.
		// Another split is stale (Rescue).
		if node != "" && node != t.Split {
			return t.Rescue(node)
		}
		var on []string
		for _, st := range t.Strands {
			if st.Current == t.Split {
				on = append(on, st.Port)
			}
		}
		if node == "" || len(on) != 1 {
			return Result{}, nil, ErrWhichGroup
		}
		key = on[0]
	}
	i := slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Port == key })
	if i >= 0 && node == "" {
		node = t.Strands[i].Current
	}
	if i < 0 || t.Strands[i].Current != node {
		// Met its siblings, or moved on, since the organisers looked.
		return Result{}, nil, ErrStale
	}
	t.focusOn(i)
	return t.Rescue(node)
}

// leaveSplit sets the focused group off down its path, port
// (splitPort).
func (t *Team) leaveSplit(n graph.Node, port string) (Result, *walk.Step, error) {
	t.Row(n.Slug)
	step, err := walk.Advance(t.G, t, n.Slug, port)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Port: port}, &step, nil
}

// splitPort is the path the focused group takes from the team's split
// n: ErrWrongNode for no group, or another split.
func (t *Team) splitPort(n graph.Node) (string, error) {
	if t.focus == 0 || t.Split != n.Slug {
		return "", ErrWrongNode
	}
	return t.Strands[t.focus-1].Port, nil
}

// movedByOther reports whether a move by the phone by (Cursor.MovedBy)
// was another phone's than the one the view is for: no phone's — the
// organisers', a timer's — is not.
func (t *Team) movedByOther(by int64) bool {
	return t.Viewer != nil && by != 0 && by != t.Viewer.Slot
}

// groups is every group of a split team, for its view.
func (t *Team) groups() []GroupView {
	out := make([]GroupView, len(t.Strands))
	r := t.rosterOf(t.everyone())
	for i, st := range t.Strands {
		g := GroupView{Port: st.Port, Label: t.groupLabel(i), Node: st.Current, Arrived: t.arrived(i), Open: r.open[i], Current: t.focus == i+1, Moved: st.Moved, MovedByOther: t.movedByOther(st.MovedBy)}
		if t.Viewer != nil {
			g.Mine = slices.Contains(r.live[i], t.Viewer.Slot)
		}
		if at, ok := t.G.Node(st.Current); ok {
			g.At = t.G.Tr(t.lang(), graph.NodeKey(at.Slug, "title"), at.Title)
		}
		if t.everyone() != nil {
			g.Players = len(r.live[i])
		}
		out[i] = g
	}
	return out
}

// groupLabel is group i's name in the view's language.
func (t *Team) groupLabel(i int) string {
	return GroupLabel(t.G, t.lang(), t.Split, t.Strands[i].Port)
}

// GroupLabel names the group that took split's path port, in lang: the
// path's name, or its role's.
func GroupLabel(g *graph.Graph, lang, split, port string) string {
	n, ok := g.Node(split)
	p, ok2 := n.Path(port)
	switch {
	case !ok || !ok2:
		return port
	case p.Label == "" && p.Role != "":
		return g.Tr(lang, graph.RoleKey(p.Role, "name"), g.RoleName(p.Role))
	}
	return g.Tr(lang, graph.NodeKey(split, "path:"+p.Port+":label"), g.PathLabel(p))
}
