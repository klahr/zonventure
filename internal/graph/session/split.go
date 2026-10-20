package session

import (
	"slices"
	"strings"
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
// Splits nest: a group that reaches a split on its path divides again,
// among its own players, and its groups meet at that split's join to go
// on as the group they were, to the outer join. Strands are the groups
// as they are now — the leaves — each knowing the forks it came down.

// Strand is one group's place on its path.
type Strand struct {
	// Port is the path the group took at the team's split, and Sub the
	// path it took at each split its group split at since, innermost
	// last: a group of a group.
	Port      string    `json:"port"`
	Sub       []Fork    `json:"sub,omitempty"`
	Current   string    `json:"current"`
	EnteredAt time.Time `json:"entered_at"`
	Paused    int       `json:"paused,omitempty"`
	Walked    float64   `json:"walked,omitempty"`
	// Members are the players dealt to the group (Deal), in the order
	// they were. None for a team whose players are not known: then every
	// group is anyone's.
	Members []int64 `json:"members,omitempty"`
}

// Fork is a split a group came to and the path it took there.
type Fork struct {
	Split string `json:"split"`
	Port  string `json:"port"`
}

// Key names a group by the paths it came down: "church", or
// "church>tower" for the tower party of the church party.
func (st Strand) Key() string {
	k := st.Port
	for _, f := range st.Sub {
		k += ">" + f.Port
	}
	return k
}

// GroupView is one group of a split team, as every phone sees it.
type GroupView struct {
	// Port is the group's Key, and Label its name: its path's, and those
	// of the groups it is part of, "Church party › Tower".
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

// lineage is the forks group i came down, the team's split first.
func (t *Team) lineage(i int) []Fork {
	return append([]Fork{{Split: t.Split, Port: t.Strands[i].Port}}, t.Strands[i].Sub...)
}

// underFork reports whether lineage l continues the forks prefix.
func underFork(l, prefix []Fork) bool {
	return len(l) >= len(prefix) && slices.Equal(l[:len(prefix)], prefix)
}

// meeting is, for the walk the focused group is on, how many forks deep
// it is now — less the joins it has been taken through on the way — and
// whether join is where the groups of its innermost split meet.
func (t *Team) meeting(join string) (depth int, ok bool) {
	if t.Split == "" || t.focus == 0 {
		return 0, false
	}
	lin := t.lineage(t.focus - 1)
	d := len(lin) - t.popped
	if d <= 0 {
		return 0, false
	}
	return d, t.joinOf(lin[d-1].Split) == join
}

// JoinWaits implements walk.State: a join holds a group while another
// group of the same split — its sibling — is still on its way.
func (t *Team) JoinWaits(join string) bool {
	return len(t.awaited(join)) > 0
}

// awaited are the paths of the focused group's siblings — the other
// groups of the same split — still on their way to join, by port, in the
// order of the groups: a sibling that has split again is one path,
// however many groups it is now. The focused group's own groups, met at
// joins the walk has passed, are with it.
func (t *Team) awaited(join string) []string {
	d, ok := t.meeting(join)
	if !ok {
		return nil
	}
	own := t.lineage(t.focus - 1)[:d]
	var out []string
	for i, st := range t.Strands {
		l := t.lineage(i)
		if underFork(l, own[:d-1]) && !underFork(l, own) && st.Current != join && !slices.Contains(out, l[d-1].Port) {
			out = append(out, l[d-1].Port)
		}
	}
	return out
}

// rejoin is what the focused group, waiting at its join, goes on as once
// its siblings arrive: the name of the group they split from, or "" for
// the whole team.
func (t *Team) rejoin() string {
	if t.focus == 0 {
		return ""
	}
	lin := t.lineage(t.focus - 1)
	if len(lin) < 2 {
		return ""
	}
	return GroupLabel(t.G, t.lang(), lin[:len(lin)-1])
}

// Joined implements walk.State: the walk has taken the focused group on
// through its join, so from there it is the group the siblings make.
func (t *Team) Joined(join string) bool {
	if _, ok := t.meeting(join); !ok {
		return false
	}
	t.popped++
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

// endSplit makes the team one again, where its cursor stands.
func (t *Team) endSplit() {
	at := t.Current
	t.unfocus()
	t.Split, t.Strands, t.Current = "", nil, at
}

// subSplit divides group i at a split on its path: its groups take its
// place, each on the split, and its players are dealt among them.
func (t *Team) subSplit(i int, slug string) {
	n, ok := t.G.Node(slug)
	if !ok || n.SplitCfg == nil {
		return
	}
	parent := t.Strands[i]
	depth := len(t.lineage(i))
	kids := make([]Strand, len(n.SplitCfg.Paths))
	for k, p := range n.SplitCfg.Paths {
		kids[k] = Strand{Port: parent.Port, Sub: append(slices.Clone(parent.Sub), Fork{Split: slug, Port: p.Port}),
			Current: slug, EnteredAt: t.Now, Paused: t.Paused}
	}
	t.Strands = slices.Replace(t.Strands, i, i+1, kids...)
	ins := make([]int, len(kids))
	for k := range ins {
		ins[k] = i + k
	}
	t.dealAt(parent.Members, ins, depth, t.RoleHolders, parent.Members)
}

// merge brings group i together with its siblings, the groups of its
// innermost split, all at their join: they become the group they split
// from, with all their players, at group i's cursor. It returns where the
// merged group is in Strands.
func (t *Team) merge(i int) int {
	lin := t.lineage(i)
	prefix := lin[:len(lin)-1]
	merged := t.Strands[i]
	merged.Sub = slices.Clone(merged.Sub[:len(merged.Sub)-1])
	merged.Members = nil
	at := -1
	var keep []Strand
	for j, st := range t.Strands {
		l := t.lineage(j)
		if len(l) == len(lin) && underFork(l, prefix) && l[len(l)-1].Split == lin[len(lin)-1].Split {
			for _, m := range st.Members {
				if !slices.Contains(merged.Members, m) {
					merged.Members = append(merged.Members, m)
				}
			}
			if at < 0 {
				at = len(keep)
				keep = append(keep, Strand{})
			}
			continue
		}
		keep = append(keep, st)
	}
	keep[at] = merged
	t.Strands = keep
	return at
}

// Deal puts each of players not yet in a group into one: at each split,
// the path whose role they hold, or, holding none, the path whose groups
// have fewest players, the first on a tie — down to a group of groups'
// own. holders is who holds each dealt role; a player holding none holds
// the default role, as does a player alone. Those already dealt stay
// where they are, so a player who joins mid-split changes no one else's
// group. It reports whether anyone was dealt, for the live engine to
// store.
func (t *Team) Deal(players []int64, holders map[string]int64) bool {
	if t.Split == "" || len(players) == 0 || len(t.Strands) == 0 {
		return false
	}
	t.unfocus()
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
		return false
	}
	all := make([]int, len(t.Strands))
	for i := range all {
		all[i] = i
	}
	t.dealAt(fresh, all, 0, holders, players)
	return true
}

// dealAt deals players among the groups at ins, which share their first
// k forks: by the paths of the split they divide at next, then on down
// each path's own groups. live are the players counted as in a group,
// for which has fewest.
func (t *Team) dealAt(players []int64, ins []int, k int, holders map[string]int64, live []int64) {
	if len(players) == 0 || len(ins) == 0 {
		return
	}
	if len(ins) == 1 && len(t.lineage(ins[0])) == k {
		st := &t.Strands[ins[0]]
		for _, p := range players {
			if !slices.Contains(st.Members, p) {
				st.Members = append(st.Members, p)
			}
		}
		return
	}
	n, ok := t.G.Node(t.lineage(ins[0])[k].Split)
	if !ok || n.SplitCfg == nil {
		return
	}
	paths := n.SplitCfg.Paths
	buckets := make([][]int, len(paths))
	for _, i := range ins {
		port := t.lineage(i)[k].Port
		for x, p := range paths {
			if p.Port == port {
				buckets[x] = append(buckets[x], i)
			}
		}
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
		c := len(assigned[x])
		for _, i := range buckets[x] {
			c += t.liveCount(i, live)
		}
		return c
	}
	for _, p := range spare {
		least := 0
		for x := range paths {
			if len(buckets[x]) > 0 && (len(buckets[least]) == 0 || count(x) < count(least)) {
				least = x
			}
		}
		assigned[least] = append(assigned[least], p)
	}
	for x := range paths {
		t.dealAt(assigned[x], buckets[x], k+1, holders, live)
	}
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
// on; any other step leaves the default focus, and the action is stale.
func (t *Team) focusFor(node string) error {
	t.popped = 0
	if t.focus > 0 && t.Strands[t.focus-1].Current == node && slices.Contains(t.candidates(), t.focus-1) {
		return nil
	}
	for _, i := range t.candidates() {
		if t.Strands[i].Current == node {
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
	t.focus, t.popped = i+1, 0
	t.Current, t.NodeEnteredAt, t.NodePaused, t.WalkedMetres = st.Current, st.EnteredAt, st.Paused, st.Walked
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
	}
	if t.Split != "" {
		// Also for a team decoded as it was sent, focused on a group: the
		// focus does not travel, the team's own cursor does.
		t.Current = t.Split
	}
	t.focus = 0
}

// applySplit follows a step into, along or out of a split: Apply's part.
// One step may do several: the last group through a join takes its
// siblings on as the group they split from, which may be the last
// through the outer join too, and walk straight on into another split.
func (t *Team) applySplit(step walk.Step) {
	defer func() { t.popped = 0 }()
	if t.Split != "" && step.Finished {
		// The adventure is over, for every group.
		t.endSplit()
		return
	}
	if t.focus > 0 {
		i := t.focus - 1
		st := &t.Strands[i]
		st.Current, st.EnteredAt, st.Paused, st.Walked = t.Current, t.NodeEnteredAt, t.NodePaused, 0
		// Through the joins it met its siblings at, innermost first.
		for k, slug := range step.Passed {
			if step.Held && k == len(step.Passed)-1 {
				break
			}
			if n, ok := t.G.Node(slug); !ok || n.Type != graph.Join {
				continue
			}
			lin := t.lineage(i)
			if t.joinOf(lin[len(lin)-1].Split) != slug {
				continue
			}
			if len(lin) == 1 {
				t.endSplit()
				break
			}
			i = t.merge(i)
			t.focus = i + 1
		}
		if t.Split != "" {
			if step.Held {
				// The group waits; its phones go on to any other group of theirs.
				t.unfocus()
				return
			}
			if n, ok := t.G.Node(step.To); ok && n.Type == graph.Split {
				t.unfocus()
				t.subSplit(i, step.To)
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
	lin := t.lineage(t.focus - 1)
	f := lin[len(lin)-1]
	return slices.Contains(t.splitPaths(f.Split).Steps[f.Port], node)
}

// leaveSplit sets the focused group off down its path.
func (t *Team) leaveSplit(n graph.Node) (Result, *walk.Step, error) {
	if t.focus == 0 {
		return Result{}, nil, ErrWrongNode
	}
	lin := t.lineage(t.focus - 1)
	if lin[len(lin)-1].Split != n.Slug {
		return Result{}, nil, ErrWrongNode
	}
	port := lin[len(lin)-1].Port
	t.Row(n.Slug)
	step, err := walk.Advance(t.G, t, n.Slug, port)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Port: port}, &step, nil
}

// groups is every group of a split team, for its view.
func (t *Team) groups() []GroupView {
	var mine []int
	if t.Viewer != nil {
		mine = t.strandsOf(t.Viewer.Slot)
	}
	out := make([]GroupView, len(t.Strands))
	for i, st := range t.Strands {
		g := GroupView{Port: st.Key(), Label: t.groupLabel(i), Node: st.Current, Arrived: t.arrived(i), Mine: slices.Contains(mine, i), Current: t.focus == i+1}
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

// groupLabel is group i's name in the view's language: its paths' names,
// outermost first.
func (t *Team) groupLabel(i int) string {
	return GroupLabel(t.G, t.lang(), t.lineage(i))
}

// GroupLabel names the group that came down forks, in lang: each path's
// name, outermost first — "Church party › Tower".
func GroupLabel(g *graph.Graph, lang string, forks []Fork) string {
	var parts []string
	for _, f := range forks {
		n, ok := g.Node(f.Split)
		p, ok2 := n.Path(f.Port)
		switch {
		case !ok || !ok2:
			parts = append(parts, f.Port)
		case p.Label == "" && p.Role != "":
			parts = append(parts, g.Tr(lang, graph.RoleKey(p.Role, "name"), g.RoleName(p.Role)))
		default:
			parts = append(parts, g.Tr(lang, graph.NodeKey(f.Split, "path:"+p.Port+":label"), g.PathLabel(p)))
		}
	}
	return strings.Join(parts, " › ")
}

// Lineage is the forks a group of a team split at split came down.
func (st Strand) Lineage(split string) []Fork {
	return append([]Fork{{Split: split, Port: st.Port}}, st.Sub...)
}
