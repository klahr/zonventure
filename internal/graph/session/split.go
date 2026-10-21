package session

import (
	"encoding/json"
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
//
// Splits do not nest (valid refuses a split on a path): a group is its
// path, and the team is split at one split at a time.

// Strand is one group's place on its path.
type Strand struct {
	// Port is the path the group took at the team's split.
	Port      string    `json:"port"`
	Current   string    `json:"current"`
	EnteredAt time.Time `json:"entered_at"`
	Paused    int       `json:"paused,omitempty"`
	Walked    float64   `json:"walked,omitempty"`
	// Members are the players dealt to the group (Deal), in the order
	// they were. None for a team whose players are not known: then every
	// group is anyone's.
	Members []int64 `json:"members,omitempty"`
	// Chosen are the players who chose to walk this group of theirs
	// (Walk) rather than their first still walking; 0 is the team as a
	// whole. Forgotten as the group arrives at its join.
	Chosen []int64 `json:"chosen,omitempty"`
}

// Key names a group: its path's port, "church".
func (st Strand) Key() string { return st.Port }

// GroupView is one group of a split team, as every phone sees it.
type GroupView struct {
	// Port is the group's Key, and Label its name: its path's, "Church
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
	entered     time.Time
	paused      int
}

// shapes are the graph's splits as the team has needed them, worked
// out once: a split's paths and its join. Kept for the graph they were
// worked out on, so a team handed another graph works them out again.
type shapes struct {
	g     *graph.Graph
	paths map[string]graph.Paths
	joins map[string]string
}

func (t *Team) shapes() *shapes {
	if t.shape == nil || t.shape.g != t.G {
		t.shape = &shapes{g: t.G, paths: map[string]graph.Paths{}, joins: map[string]string{}}
	}
	return t.shape
}

// splitPaths is t.G.SplitPaths, once per split.
func (t *Team) splitPaths(split string) graph.Paths {
	sh := t.shapes()
	p, ok := sh.paths[split]
	if !ok {
		p = t.G.SplitPaths(split)
		sh.paths[split] = p
	}
	return p
}

// joinOf is t.G.JoinOf, once per split: "" for none.
func (t *Team) joinOf(split string) string {
	sh := t.shapes()
	j, ok := sh.joins[split]
	if !ok {
		j, _ = t.G.JoinOf(split)
		sh.joins[split] = j
	}
	return j
}

// meets reports whether join is where the focused group meets its
// siblings, on the walk under way: its split's join, not yet passed.
func (t *Team) meets(join string) bool {
	return t.Split != "" && t.focus > 0 && !t.joined && t.joinOf(t.Split) == join
}

// JoinWaits implements walk.State: a join holds a group while another
// group of the split — its sibling — is still on its way.
func (t *Team) JoinWaits(join string) bool {
	return len(t.awaited(join)) > 0
}

// awaited are the paths of the focused group's siblings still on their
// way to join, in the order of the groups.
func (t *Team) awaited(join string) []string {
	if !t.meets(join) {
		return nil
	}
	var out []string
	for i, st := range t.Strands {
		if i != t.focus-1 && st.Current != join {
			out = append(out, st.Port)
		}
	}
	return out
}

// Joined implements walk.State: the walk has taken the focused group on
// through its join, so from there it is the team's.
func (t *Team) Joined(join string) bool {
	if !t.meets(join) {
		return false
	}
	t.joined = true
	return true
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

// Group is the key of the group a view or action is for, or "".
func (t *Team) Group() string {
	if t.focus == 0 {
		return ""
	}
	return t.Strands[t.focus-1].Key()
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

// SplitAfresh divides the team again where it stands, as on arriving,
// its groups' places forgotten — for a team whose groups no longer fit
// its graph: on a split, every group on it; anywhere else, one team;
// on a split taken out of the graph, one team at its start screen.
func (t *Team) SplitAfresh() {
	t.unfocus()
	t.Split, t.Strands = "", nil
	n, ok := t.G.Node(t.Current)
	switch {
	case !ok:
		// The split itself is gone from the graph: the team is at its
		// start screen, to begin again.
		t.Current = ""
	case n.Type == graph.Split:
		t.beginSplit(t.Current)
	}
}

// endSplit makes the team one again, where its cursor stands and since
// its cursor came there.
func (t *Team) endSplit() {
	at, entered, paused := t.Current, t.NodeEnteredAt, t.NodePaused
	t.unfocus()
	t.Split, t.Strands, t.Current = "", nil, at
	t.NodeEnteredAt, t.NodePaused = entered, paused
}

// Deal puts each of players not yet in a group into one: the path whose
// role they hold, or, holding none, the group with fewest players, the
// first on a tie. holders is who holds each dealt role; a player holding none holds
// the default role, as does a player alone. Those already dealt stay
// where they are, so a player who joins mid-split changes no one else's
// group. A choice of a group the chooser may no longer walk — its own
// players back from away — is dropped (dropStale), for Away as it is
// now. It reports whether anyone was dealt or a choice dropped, for the
// live engine to store.
func (t *Team) Deal(players []int64, holders map[string]int64) bool {
	if t.Split == "" || len(players) == 0 || len(t.Strands) == 0 {
		return false
	}
	t.unfocus()
	dropped := t.dropStale(players)
	dealt := map[int64]bool{}
	for _, st := range t.Strands {
		for _, m := range st.Members {
			dealt[m] = true
		}
	}
	var fresh []int64
	for _, p := range players {
		if !dealt[p] {
			fresh = append(fresh, p)
		}
	}
	if len(fresh) == 0 {
		return dropped
	}
	t.dealAt(fresh, holders, players)
	return true
}

// dropStale forgets each player's choice of a group that is no longer
// one they may walk, among players those playing: chosen while it was
// anyone's, its own players since back. The choice is spent — the
// player went back to their own group — and the group going quiet again
// later does not take the phone back to it unasked. The team as a
// whole's choice stands. It reports whether any was dropped.
func (t *Team) dropStale(players []int64) bool {
	dropped := false
	for i := range t.Strands {
		chosen := t.Strands[i].Chosen
		if len(chosen) == 0 {
			continue
		}
		t.Strands[i].Chosen = slices.DeleteFunc(chosen, func(c int64) bool {
			stale := c != 0 && !slices.Contains(t.strandsAmong(c, players), i)
			dropped = dropped || stale
			return stale
		})
		if len(t.Strands[i].Chosen) == 0 {
			t.Strands[i].Chosen = nil
		}
	}
	return dropped
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
		t.GroupsChanged, changed = true, true
	}
	if t.walking(slot, t.strandsOf(slot)) != i {
		t.Strands[i].Chosen = append(t.Strands[i].Chosen, slot)
		t.GroupsChanged, changed = true, true
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
	def := t.G.DefaultRole()
	assigned := make([][]int64, len(paths))
	var spare []int64
	for _, p := range players {
		joined := false
		for x, path := range paths {
			mine := path.Role != "" && (path.Role == def && (!holds[p] || len(live) == 1) ||
				path.Role != def && holders[path.Role] == p)
			if mine {
				assigned[x] = append(assigned[x], p)
				joined = true
			}
		}
		if !joined {
			spare = append(spare, p)
		}
	}
	count := func(x int) int {
		return len(assigned[x]) + t.liveCount(group[x], live)
	}
	for _, p := range spare {
		least := 0
		for x := range paths {
			if group[x] >= 0 && (group[least] < 0 || count(x) < count(least)) {
				least = x
			}
		}
		assigned[least] = append(assigned[least], p)
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

// live are a group's members still playing; all of them for a team
// whose players are not known.
func (t *Team) live(i int) []int64 { return t.liveAmong(i, t.everyone()) }

// liveAmong are a group's members among players; all of them for nil.
func (t *Team) liveAmong(i int, players []int64) []int64 {
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
func (t *Team) strandsOf(slot int64) []int { return t.strandsAmong(slot, t.everyone()) }

// strandsAmong is strandsOf with players the ones playing.
func (t *Team) strandsAmong(slot int64, players []int64) []int {
	var own, anyone []int
	for i := range t.Strands {
		switch {
		case slices.Contains(t.liveAmong(i, players), slot):
			own = append(own, i)
		case t.anyones(i, players):
			anyone = append(anyone, i)
		}
	}
	return append(own, anyone...)
}

// anyones reports whether group i is anyone's to walk, among players
// those playing: no one is left in it, or every one of them is away — a
// phone flat, the app shut — until one is back.
func (t *Team) anyones(i int, players []int64) bool {
	return !slices.ContainsFunc(t.liveAmong(i, players), func(m int64) bool { return !t.Away[m] })
}

// walks reports whether player p, walking group i, is counted as one of
// its walkers, among players those playing: not when p is away and the
// group is anyone's for it — its own group, every one of whose players
// is away. Those walking it are its walkers, and a role its away
// holder has is everyone's in it, until one of them is back.
func (t *Team) walks(p int64, i int, players []int64) bool {
	return !t.Away[p] || !t.anyones(i, players)
}

// arrived reports whether a group waits at its join.
func (t *Team) arrived(i int) bool {
	n, ok := t.G.Node(t.Strands[i].Current)
	return ok && n.Type == graph.Join
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
	if i := t.walking(t.asking(), t.candidates()); i >= 0 {
		t.focusOn(i)
	}
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
	i := slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Key() == key })
	if i < 0 || t.arrived(i) {
		return Result{}, nil, ErrStale
	}
	if !slices.Contains(t.candidates(), i) {
		return Result{}, nil, ErrNotYours
	}
	slot := t.asking()
	for k := range t.Strands {
		t.Strands[k].Chosen = slices.DeleteFunc(t.Strands[k].Chosen, func(s int64) bool { return s == slot })
	}
	t.Strands[i].Chosen = append(t.Strands[i].Chosen, slot)
	t.GroupsChanged = true
	return Result{Accepted: true}, nil, nil
}

// focusFor puts the team on the group an action on node is for: one of
// the phone's at node. Another group's step is not the phone's to act
// on; any other step leaves the default focus, and the action is stale.
func (t *Team) focusFor(node string) error {
	t.joined = false
	if n, ok := t.G.Node(node); ok && n.Type == graph.Split {
		if t.Viewer != nil {
			// A phone's press on a split sets off the group it is walking,
			// and only that: pressed again, after the group set off, it is
			// a repeat — stale — not a press for its next group, or an
			// empty one. That group's turn comes when its phones see it.
			t.Focus()
			return nil
		}
		// The team as a whole sets off the group it chose to walk, while
		// that still stands on the split; else the first that does. A
		// group it is already on that stands there — the one an
		// organiser's rescue names (RescueGroup) — is the one it sets off.
		if t.focus == 0 || t.Strands[t.focus-1].Current != node {
			for _, i := range t.candidates() {
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
	if n, ok := t.G.Node(node); ok && n.Type == graph.Join {
		own = false
	}
	if i := t.focus - 1; i >= 0 && t.Strands[i].Current == node && slices.Contains(t.candidates(), i) {
		if own && t.actsOn(i) {
			t.focusOn(i)
		}
		return nil
	}
	for _, i := range t.candidates() {
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
	if n, ok := t.G.Node(node); ok && n.Type != graph.Split && n.Type != graph.Join {
		for _, st := range t.Strands {
			if st.Current == node {
				return ErrNotYours
			}
		}
	}
	t.Focus()
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
	out := []int64{}
	for _, p := range players {
		if t.walking(p, t.strandsOf(p)) == i && t.walks(p, i, players) {
			out = append(out, p)
		}
	}
	return out
}

// Walkers are the players walking each group now, by its Key: each of
// players on the first of their groups still walking — their own, or
// one no one among players is with — but for a group's own players away
// while it is anyone's. A group no one walks has none.
func (t *Team) Walkers(players []int64) map[string][]int64 {
	out := make(map[string][]int64, len(t.Strands))
	for _, st := range t.Strands {
		out[st.Key()] = []int64{}
	}
	for _, p := range players {
		if i := t.walking(p, t.strandsAmong(p, players)); i >= 0 && t.walks(p, i, players) {
			k := t.Strands[i].Key()
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
	// What the team walked unfocused, the simulator moving it, the group
	// it is put on walked.
	unowned := t.WalkedMetres
	st := t.Strands[i]
	group := t.strandPlayers(i)
	t.saved = &focused{players: t.Players, presses: t.Presses, holders: t.RoleHolders, phones: t.Phones, position: t.Position, viewer: t.Viewer,
		entered: t.NodeEnteredAt, paused: t.NodePaused}
	if t.Viewer != nil {
		t.saved.viewerRoles = t.Viewer.Roles
	}
	t.focus, t.joined = i+1, false
	t.Current, t.NodeEnteredAt, t.NodePaused, t.WalkedMetres = st.Current, st.EnteredAt, st.Paused, st.Walked+unowned
	if w, ok := t.Walks[st.Key()]; ok {
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

// MarshalJSON writes the team as it is unfocused, for the focus does not
// travel: the group's cursor and how far it walked go with its strand,
// and the team's own cursor is the split.
func (t Team) MarshalJSON() ([]byte, error) {
	if t.Split != "" {
		if t.focus > 0 {
			t.Strands = slices.Clone(t.Strands)
			t.Strands[t.focus-1].Walked, t.WalkedMetres = t.WalkedMetres, 0
		}
		t.Current = t.Split
	}
	type plain Team
	return json.Marshal(plain(t))
}

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
		// The team's own clock is when it came to the split, not when the
		// group last moved.
		t.NodeEnteredAt, t.NodePaused = t.saved.entered, t.saved.paused
		t.saved = nil
	}
	if t.focus > 0 && t.Split != "" {
		// How far the group has walked on its step, as the simulator adds
		// it; the team unfocused has walked none of its own since.
		t.Strands[t.focus-1].Walked, t.WalkedMetres = t.WalkedMetres, 0
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
	defer func() { t.joined = false }()
	if t.Split != "" && step.Finished {
		// The adventure is over, for every group.
		t.endSplit()
		return
	}
	if t.focus > 0 {
		i := t.focus - 1
		st := &t.Strands[i]
		st.Current, st.EnteredAt, st.Paused, st.Walked = t.Current, t.NodeEnteredAt, t.NodePaused, 0
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
	return slices.Contains(t.splitPaths(t.Split).Steps[t.Strands[t.focus-1].Port], node)
}

// onAnyPath reports whether node is a step of any split's path.
func (t *Team) onAnyPath(node string) bool {
	for _, n := range t.G.Nodes {
		if n.Type != graph.Split {
			continue
		}
		for _, steps := range t.splitPaths(n.Slug).Steps {
			if slices.Contains(steps, node) {
				return true
			}
		}
	}
	return false
}

// RescueGroup is Rescue for one group of a split team — the group with
// key, which must stand on node: several groups may wait on one split,
// and the organisers move the one they mean. Without a key it is Rescue;
// a key for a team no longer split, or for no group of it, is stale. A
// split team's split names no group, so a rescue there must say which.
func (t *Team) RescueGroup(node, key string) (Result, *walk.Step, error) {
	if key == "" && t.Split != "" && !t.Completed {
		// A group's own step says whose it is; a split does not.
		if n, ok := t.G.Node(node); ok && n.Type == graph.Split {
			return Result{}, nil, ErrWhichGroup
		}
	}
	if key != "" && t.Split == "" && !t.Completed {
		// The groups met, or the team was put elsewhere, since the
		// organisers looked: the group is gone, not the team to move.
		return Result{}, nil, ErrStale
	}
	if key != "" && t.Split != "" && !t.Completed {
		i := slices.IndexFunc(t.Strands, func(st Strand) bool { return st.Key() == key })
		if i < 0 || t.Strands[i].Current != node {
			// Met its siblings, or moved on, since the organisers looked.
			return Result{}, nil, ErrStale
		}
		t.focusOn(i)
	}
	return t.Rescue(node)
}

// leaveSplit sets the focused group off down its path.
func (t *Team) leaveSplit(n graph.Node) (Result, *walk.Step, error) {
	if t.focus == 0 {
		return Result{}, nil, ErrWrongNode
	}
	if t.Split != n.Slug {
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
	out := make([]GroupView, len(t.Strands))
	for i, st := range t.Strands {
		g := GroupView{Port: st.Key(), Label: t.groupLabel(i), Node: st.Current, Arrived: t.arrived(i), Open: t.anyones(i, t.everyone()), Current: t.focus == i+1}
		if t.Viewer != nil {
			g.Mine = slices.Contains(t.live(i), t.Viewer.Slot)
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
