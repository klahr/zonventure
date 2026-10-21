// Package sim plays a whole event with bot teams, in memory, on the same
// rules a real event runs (internal/graph/session), so an author can see
// how the adventure holds up under many teams before anyone walks it:
// how long it takes, where teams get stuck, how many hints they use and
// how far apart they finish.
//
// The bots are not clever. Each team gets a profile — walking pace, how
// good it is at puzzles, how patient with hints, how well it navigates —
// drawn from a seeded random source, and plays by reading its view the
// way a phone shows it. It knows the right answers (it reads the graph),
// but finds them only by chance, which grows with time spent, hints
// taken and nudges from the arranger. It walks at its own pace, not in
// straight lines, sometimes to the wrong place first.
//
// The arranger is simulated too: at a chosen rate it sends a message,
// mostly a nudge to the team that has been stuck longest, which that
// team acts on.
//
// Nothing runs in real time. The clock jumps from one team's next action
// to the next, so a three-hour event takes well under a second, and the
// result is a timeline the designer plays back at any speed. The same
// seed and graph give the same run.
package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// Limits on one run. A run is CPU only, but a request must not be able
// to ask for an hour of it.
const (
	MaxTeams   = 200
	MaxMembers = 12
	MaxSeconds = 24 * 60 * 60
	// maxActions bounds one team's actions, so a graph that loops for
	// ever cannot keep the run going.
	maxActions = 4000
	// stallAfter is how long a team may find nothing to do before it is
	// given up on: the draft has left it nowhere to go.
	stallAfter = 30 * 60
)

// Config is what the author sets.
type Config struct {
	Teams   int `json:"teams"`
	Members int `json:"members"`
	// Spread is how different the teams are, 0 (all alike) to 1 (from
	// families out for a stroll to orienteers).
	Spread float64 `json:"spread"`
	// MessageEvery is the seconds between the arranger's messages; 0
	// sends none.
	MessageEvery int `json:"message_every"`
	// MaxSeconds ends the run, finished or not.
	MaxSeconds int `json:"max_seconds"`
	// StartSpread is the seconds over which teams turn up and begin.
	StartSpread int `json:"start_spread"`
	// Start is the event's start; time-of-day conditions read it.
	Start time.Time `json:"start"`
	Seed  uint64    `json:"seed"`
}

// Check fills defaults and refuses what is out of range.
func (c *Config) Check() error {
	if c.Teams < 1 || c.Teams > MaxTeams {
		return fmt.Errorf("teams must be between 1 and %d", MaxTeams)
	}
	if c.Members < 1 || c.Members > MaxMembers {
		return fmt.Errorf("members must be between 1 and %d", MaxMembers)
	}
	if c.Spread < 0 || c.Spread > 1 {
		return errors.New("spread must be between 0 and 1")
	}
	if c.MessageEvery < 0 {
		return errors.New("message_every cannot be negative")
	}
	if c.MaxSeconds <= 0 {
		c.MaxSeconds = 6 * 60 * 60
	}
	if c.MaxSeconds > MaxSeconds {
		return fmt.Errorf("max_seconds cannot be more than %d", MaxSeconds)
	}
	if c.StartSpread < 0 || c.StartSpread > c.MaxSeconds {
		return errors.New("start_spread must be between 0 and max_seconds")
	}
	return nil
}

// Waypoint is where a team is at T seconds into the run; between two,
// it walks straight at even speed.
type Waypoint struct {
	T   float64 `json:"t"`
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Event is one thing that happened, for the feed.
type Event struct {
	T    float64 `json:"t"`
	Team int     `json:"team"`
	// Kind is start, arrive, astray, wrong, right, locked, hint, skip,
	// timeout, timeup, choose, back, leave, continue, pick, language,
	// split, message, nudge, forced, stalled, error or finish. Team is -1 for a
	// message to everyone.
	Kind string `json:"kind"`
	Node string `json:"node,omitempty"`
	Text string `json:"text,omitempty"`
}

// Team is one bot team's run.
type Team struct {
	Name    string     `json:"name"`
	Members int        `json:"members"`
	Path    []Waypoint `json:"path"`
	// Profile, for the author wondering why a team did what it did.
	Pace    float64 `json:"pace"`
	Skill   float64 `json:"skill"`
	Patient float64 `json:"patience"`
	// Finished is when it reached an end; nil if it did not.
	Finished *float64 `json:"finished,omitempty"`
	Stalled  bool     `json:"stalled,omitempty"`
	Outcome  string   `json:"outcome,omitempty"`
	Score    float64  `json:"score"`
	Walked   float64  `json:"walked"`
	Wrong    int      `json:"wrong"`
	Hints    int      `json:"hints"`
	Skips    int      `json:"skips"`
	Nudges   int      `json:"nudges"`
	Steps    int      `json:"steps"`
	// Progress is the score over time, for standings during playback.
	Progress []Mark `json:"progress"`
}

// Mark is a team's score and steps done at T.
type Mark struct {
	T     float64 `json:"t"`
	Score float64 `json:"score"`
	Steps int     `json:"steps"`
}

// Step is how one step went across every team.
type Step struct {
	Node     string  `json:"node"`
	Title    string  `json:"title,omitempty"`
	Type     string  `json:"type"`
	Reached  int     `json:"reached"`
	Left     int     `json:"left"`
	Median   float64 `json:"median"`
	Max      float64 `json:"max"`
	Wrong    int     `json:"wrong"`
	Hints    int     `json:"hints"`
	Skips    int     `json:"skips"`
	Timeouts int     `json:"timeouts"`
	Nudges   int     `json:"nudges"`
	Forced   int     `json:"forced"`
	// Waiting is teams still on the step when the run ended.
	Waiting int `json:"waiting"`
}

// Result is the whole run.
type Result struct {
	Start    time.Time `json:"start"`
	Duration float64   `json:"duration"`
	// Ended is why the run stopped: finished (every team is done),
	// time (MaxSeconds) or stalled (no team can go on).
	Ended  string  `json:"ended"`
	Teams  []Team  `json:"teams"`
	Events []Event `json:"events"`
	Steps  []Step  `json:"steps"`
}

// Run plays the event. An error is the draft refusing to start at all.
func Run(g *graph.Graph, c Config) (*Result, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if c.Start.IsZero() {
		return nil, errors.New("start is required")
	}
	r := &runner{g: g, c: c, rng: rand.New(rand.NewPCG(c.Seed, c.Seed^0x9e3779b97f4a7c15)), stats: map[string]*stepStat{}}
	r.origin = r.gathering()
	for i := range c.Teams {
		if err := r.addTeam(i); err != nil {
			return nil, err
		}
	}
	r.loop()
	return r.result(), nil
}

type stepStat struct {
	Step
	times []float64
}

type runner struct {
	g      *graph.Graph
	c      Config
	rng    *rand.Rand
	bots   []*bot
	events []Event
	stats  map[string]*stepStat
	order  []string
	now    float64
	// origin is where teams gather before the start; nil for an
	// adventure without places.
	origin   *session.Position
	nextNote float64
	notes    int
}

func (r *runner) at(t float64) time.Time {
	return r.c.Start.Add(time.Duration(t * float64(time.Second)))
}

func (r *runner) emit(team int, kind, node, text string) { r.emitAt(r.now, team, kind, node, text) }

// emitAt records an event at t, which may be ahead of the clock: an
// arrival is known when the walk starts.
func (r *runner) emitAt(t float64, team int, kind, node, text string) {
	r.events = append(r.events, Event{T: round(t, 1), Team: team, Kind: kind, Node: node, Text: text})
}

func (r *runner) stat(node string) *stepStat {
	s := r.stats[node]
	if s == nil {
		n, _ := r.g.Node(node)
		s = &stepStat{Step: Step{Node: node, Title: n.Title, Type: string(n.Type)}}
		r.stats[node] = s
		r.order = append(r.order, node)
	}
	return s
}

// gathering is where teams wait for the start: near the first start's
// place, or the first place the adventure has.
func (r *runner) gathering() *session.Position {
	for _, st := range r.g.Starts() {
		if st.StartCfg != nil && st.StartCfg.Zone != "" {
			if z, ok := r.zone(st.StartCfg.Zone); ok {
				p := centre(z)
				return &p
			}
		}
	}
	if len(r.g.Zones) > 0 {
		p := centre(r.g.Zones[0])
		return &p
	}
	return nil
}

func (r *runner) zone(slug string) (graph.Zone, bool) {
	for _, z := range r.g.Zones {
		if z.Slug == slug {
			return z, true
		}
	}
	return graph.Zone{}, false
}

func (r *runner) addTeam(i int) error {
	spread := r.c.Spread
	// A profile is a middling team pushed apart by Spread.
	vary := func(mid, width float64) float64 {
		return clamp(mid+(r.rng.Float64()*2-1)*width*spread, 0.02, 0.98)
	}
	b := &bot{
		r:     r,
		id:    i,
		skill: vary(0.5, 0.45),
		// Metres per second: a stroll is 1, a brisk walk 1.5, a jog 2.5.
		pace:    clamp(1.3*math.Exp((r.rng.Float64()*2-1)*0.55*spread), 0.7, 2.8),
		patient: vary(0.5, 0.45),
		lost:    vary(0.15, 0.13),
		greedy:  vary(0.6, 0.35),
		wake:    r.rng.Float64() * float64(r.c.StartSpread),
	}
	b.out = Team{Name: fmt.Sprintf("Team %d", i+1), Members: r.c.Members, Pace: round(b.pace, 2), Skill: round(b.skill, 2), Patient: round(b.patient, 2)}
	team, step, err := session.Start(r.g, r.at(b.wake))
	if err != nil {
		return err
	}
	team.Size = r.c.Members
	b.t = team
	if r.origin != nil {
		// Teams mill about the gathering point, a little apart.
		p := offset(*r.origin, 20+r.rng.Float64()*120, r.rng.Float64()*2*math.Pi)
		b.t.Position = &p
		b.out.Path = append(b.out.Path, Waypoint{T: 0, Lat: p.Lat, Lng: p.Lng})
	}
	r.bots = append(r.bots, b)
	b.applied(step)
	b.enterT = b.wake
	return nil
}

// loop runs the team due soonest until every team is done or out of time.
func (r *runner) loop() {
	if r.c.MessageEvery > 0 {
		r.nextNote = float64(r.c.MessageEvery)
	} else {
		r.nextNote = math.Inf(1)
	}
	limit := float64(r.c.MaxSeconds)
	for {
		var next *bot
		for _, b := range r.bots {
			if !b.done && (next == nil || b.wake < next.wake) {
				next = b
			}
		}
		if next == nil {
			return
		}
		if r.nextNote <= next.wake && r.nextNote <= limit {
			r.now = r.nextNote
			r.message()
			r.nextNote += float64(r.c.MessageEvery)
			continue
		}
		if next.wake > limit {
			r.now = limit
			return
		}
		r.now = next.wake
		next.act()
		// A walking team still runs out of time on the dot.
		if d, ok := next.t.Deadline(); ok && !next.t.TimedOut && !next.done {
			next.wake = math.Min(next.wake, math.Max(r.now+1, d.Sub(r.c.Start).Seconds()))
		}
	}
}

// message is the arranger writing: most often to the team stuck longest,
// sometimes to everyone — the bots read those, but nothing in them is
// for them to act on.
func (r *runner) message() {
	r.notes++
	if r.notes%4 == 0 {
		r.emit(-1, "message", "", "A note to every team")
		return
	}
	var worst *bot
	for _, b := range r.bots {
		if b.done || b.t.Completed || b.nudged || b.t.Current == "" {
			continue
		}
		if n, _ := r.g.Node(b.t.Current); n.Type != graph.Puzzle {
			continue
		}
		if worst == nil || b.t.NodeEnteredAt.Before(worst.t.NodeEnteredAt) {
			worst = b
		}
	}
	// Five minutes on a step is not stuck; leave them be.
	if worst == nil || r.at(r.now).Sub(worst.t.NodeEnteredAt) < 5*time.Minute {
		return
	}
	worst.nudged = true
	worst.out.Nudges++
	r.stat(worst.t.Current).Nudges++
	r.emit(worst.id, "message", worst.t.Current, "A nudge from the arranger")
	// The team reads it on its next look at the phone, soon.
	worst.wake = math.Min(worst.wake, r.now+30+r.rng.Float64()*90)
}

func (r *runner) result() *Result {
	sort.SliceStable(r.events, func(i, j int) bool { return r.events[i].T < r.events[j].T })
	res := &Result{Start: r.c.Start, Duration: r.now, Events: r.events}
	finished, stalled := 0, 0
	for _, b := range r.bots {
		b.out.Score = b.t.Score()
		if b.t.Completed {
			finished++
		} else if b.out.Stalled {
			stalled++
		} else if b.t.Current != "" {
			r.stat(b.t.Current).Waiting++
		}
		b.out.Walked = math.Round(b.out.Walked)
		res.Teams = append(res.Teams, b.out)
	}
	switch {
	case finished == len(r.bots):
		res.Ended = "finished"
	case finished+stalled == len(r.bots):
		res.Ended = "stalled"
	default:
		res.Ended = "time"
	}
	for _, slug := range r.order {
		s := r.stats[slug]
		if len(s.times) > 0 {
			sort.Float64s(s.times)
			s.Median = math.Round(s.times[len(s.times)/2])
			s.Max = math.Round(s.times[len(s.times)-1])
		}
		res.Steps = append(res.Steps, s.Step)
	}
	return res
}

// bot is one team playing.
type bot struct {
	r   *runner
	id  int
	t   *session.Team
	out Team

	skill, pace, patient, lost, greedy float64

	wake    float64
	done    bool
	actions int
	// idle is when the team last found nothing it could do; 0 while it
	// has.
	idle float64
	// nudged is a message from the arranger not yet acted on.
	nudged bool

	// On the current step: when the team began working on it (after
	// walking there), how far it has got, and where it has been sent
	// astray to.
	node    string
	enterT  float64
	working float64
	chance  float64
	astray  bool
	// branches the team is in, for other teams' hub capacity.
	open map[string]bool
	// given is the answer outputs the team has taken at each puzzle, so
	// at one with many right answers — squares on a scanner, say — it
	// tries another each time it comes back; searched is the places it
	// has searched at each.
	given    map[string]map[string]bool
	searched map[string]map[string]bool

	// regroup is when a team that split up to open a branch is together
	// again, having walked back to the player who stayed nearest.
	regroup float64

	// round is the hub the team last chose at and how far it had got
	// then, and roundSince when it first stood there that way.
	round      string
	roundSince float64

	// waits are the groups of a split team found able to do nothing, by
	// key, and when each is worth a look again: hopping to one before
	// then gets nowhere. Forgotten as the team gets anything done.
	waits map[string]float64
}

// mark notes key under node in m, making the maps as needed.
func mark(m *map[string]map[string]bool, node, key string) {
	if *m == nil {
		*m = map[string]map[string]bool{}
	}
	if (*m)[node] == nil {
		(*m)[node] = map[string]bool{}
	}
	(*m)[node][key] = true
}

func (b *bot) rng() *rand.Rand { return b.r.rng }

// act does the team's next thing and sets when it next wakes.
func (b *bot) act() {
	b.actions++
	if b.actions > maxActions {
		b.stall("too many actions — does the draft loop?")
		return
	}
	if b.regroup > b.r.now {
		b.wake = b.regroup
		return
	}
	b.t.Now = b.r.at(b.r.now)
	b.t.Occ = b.r.occupancy(b)

	if b.t.TimeUpDue() {
		b.do("timeup", func() (session.Result, *walk.Step, error) { return b.t.TimeUp() })
		return
	}
	v := b.t.View()
	switch {
	case v.Finished || b.t.Completed:
		b.finish()
	case len(v.Starts) > 0 && b.t.Current == "":
		b.chooseStart(v)
	case v.Type == graph.Story:
		b.story(v)
	case v.Type == graph.Puzzle:
		b.puzzle(v)
	case v.Type == graph.Hub:
		b.hub(v)
	case v.Type == graph.Split:
		// A simulated team is all of its players: it walks each group's
		// path in turn (session.Team.Focus), setting each off from here.
		if _, ok := b.do("continue", func() (session.Result, *walk.Step, error) { return b.t.Continue(v.Node) }); ok {
			b.later(5 + b.rng().Float64()*10)
		}
	default:
		b.waitFor(v)
	}
}

// later wakes the team again in s seconds.
func (b *bot) later(s float64) { b.wake = b.r.now + math.Max(1, s) }

// do runs one session action and applies its walk; it reports whether
// the action went through.
func (b *bot) do(kind string, action func() (session.Result, *walk.Step, error)) (session.Result, bool) {
	node := b.t.Current
	before := cloneTeam(b.t)
	res, step, err := b.t.Act(action)
	if err != nil {
		*b.t = *before
		code := session.Code(err)
		if code == "" {
			// The draft stopped the walk — an unconnected output. That is
			// for the author to fix; the team cannot go on.
			b.r.emit(b.id, "error", node, err.Error())
			b.stall(err.Error())
			return res, false
		}
		if errors.Is(err, session.ErrLocked) && res.BlockedUntil != nil {
			b.r.emit(b.id, "locked", node, "")
			b.wake = math.Max(b.r.now+1, res.BlockedUntil.Sub(b.r.c.Start).Seconds()+1)
			return res, false
		}
		b.later(30 + b.rng().Float64()*60)
		return res, false
	}
	if res.TimeUp {
		kind = "timeup"
	}
	switch kind {
	case "hint", "pick", "language", "wrong":
	case "right":
		if res.Accepted {
			b.r.emit(b.id, kind, node, res.Port)
		}
	default:
		b.r.emit(b.id, kind, node, res.Port)
	}
	if step != nil {
		b.t.Apply(*step)
		b.applied(*step)
	}
	if kind != "walk" {
		// Anything done may be what a waiting group waits for.
		b.waits = nil
	}
	b.idle = 0
	return res, true
}

// applied follows the team onto where a walk left it.
func (b *bot) applied(step walk.Step) {
	if b.open == nil {
		b.open = map[string]bool{}
	}
	for _, slug := range step.Passed {
		if n, ok := b.r.g.Node(slug); ok && n.Type == graph.Hub {
			for k := range b.open {
				if strings.HasPrefix(k, slug+"\x00") {
					delete(b.open, k)
				}
			}
		}
	}
	for _, e := range step.Entered {
		b.open[e.Hub+"\x00"+e.Port] = true
	}
	if b.t.Completed {
		b.open = nil
		b.finish()
		return
	}
	if b.t.Current != b.node {
		b.gather()
		b.leftStep()
		b.node = b.t.Current
		b.enterT, b.working, b.chance, b.astray = b.r.now, -1, 0, false
		if b.node != "" {
			b.r.stat(b.node).Reached++
		}
	}
}

func (b *bot) leftStep() {
	if b.node == "" {
		return
	}
	s := b.r.stat(b.node)
	s.Left++
	s.times = append(s.times, b.r.now-b.enterT)
	b.out.Steps++
	b.out.Progress = append(b.out.Progress, Mark{T: b.r.now, Score: b.t.Score(), Steps: b.out.Steps})
}

func (b *bot) finish() {
	if b.done {
		return
	}
	b.leftStep()
	b.node = ""
	b.done = true
	t := b.r.now
	b.out.Finished = &t
	b.out.Outcome = string(b.t.Ending)
	b.r.emit(b.id, "finish", "", string(b.t.Ending))
}

func (b *bot) stall(why string) {
	b.done = true
	b.out.Stalled = true
	b.r.emit(b.id, "stalled", b.t.Current, why)
}

// waitFor is a step the team can do nothing on: a gate waiting for a
// time, say. It looks again when the view says to, or in a minute — or,
// split up, when the first of its groups is worth a look again.
func (b *bot) waitFor(v session.View) {
	again := b.r.now + 60
	if v.RecheckAt != nil && v.RecheckAt.After(b.t.Now) {
		again = v.RecheckAt.Sub(b.r.c.Start).Seconds() + 1
	}
	if v.Group != "" {
		if at := slices.IndexFunc(v.Groups, func(g session.GroupView) bool { return g.Current }); at >= 0 {
			if b.waits == nil {
				b.waits = map[string]float64{}
			}
			b.waits[v.Groups[at].Port] = again
		}
	}
	if b.hop(v) {
		return
	}
	if b.idle == 0 {
		b.idle = b.r.now
	}
	if b.r.now-b.idle > stallAfter {
		b.stall("nothing to do on this step")
		return
	}
	for _, g := range v.Groups {
		if w, ok := b.waits[g.Port]; ok && !g.Current && !g.Arrived && w < again {
			again = w
		}
	}
	b.wake = math.Max(b.r.now+1, again)
}

// hop puts a split team on the next of its groups still on its way,
// when the one it walks can do nothing: another may find what this one
// needs (session.Team.Walk). A group found waiting is not hopped to
// until it is worth a look again (waits), so groups all waiting — on the
// clock, say — are waited for, not hopped between. Hopping is not
// doing, so a team hopping between groups all stuck still stalls.
func (b *bot) hop(v session.View) bool {
	at := slices.IndexFunc(v.Groups, func(g session.GroupView) bool { return g.Current })
	if at < 0 {
		return false
	}
	for k := 1; k < len(v.Groups); k++ {
		g := v.Groups[(at+k)%len(v.Groups)]
		if w, ok := b.waits[g.Port]; g.Arrived || ok && w > b.r.now {
			continue
		}
		idle := b.idle
		if idle == 0 {
			idle = b.r.now
		}
		_, ok := b.do("walk", func() (session.Result, *walk.Step, error) { return b.t.Walk(g.Port) })
		if b.idle = idle; ok && b.r.now-idle <= stallAfter {
			b.later(5)
			return true
		}
		return false
	}
	return false
}

// --- moving ---------------------------------------------------------------

// goTo walks the team towards zone z, reporting whether it is there
// already. Walking takes time: the team wakes again on arrival.
func (b *bot) goTo(z *graph.Zone) bool {
	if z == nil || b.t.InZone(z.Slug) {
		return true
	}
	if b.t.Position == nil {
		// No fix yet: the phone has not placed the team. Put it at the
		// gathering point and walk from there.
		p := centre(*z)
		if b.r.origin != nil {
			p = *b.r.origin
		}
		b.t.Position = &p
	}
	target := b.inside(*z)
	// Now and then a team heads somewhere wrong first — misreads the
	// map, takes the other path — and only then finds the place.
	if !b.astray && b.rng().Float64() < b.lost {
		b.astray = true
		from := *b.t.Position
		d := session.HaversineMeters(from.Lat, from.Lng, target.Lat, target.Lng)
		wrong := offset(target, 60+b.rng().Float64()*math.Max(80, d*0.4), b.rng().Float64()*2*math.Pi)
		b.r.emit(b.id, "astray", b.t.Current, z.Name)
		b.walk(wrong)
		return false
	}
	b.walk(target)
	b.r.emitAt(b.wake, b.id, "arrive", b.t.Current, z.Name)
	return false
}

// walk moves the team to p along a path that bends, at its own pace
// with some give, and wakes it there.
func (b *bot) walk(p session.Position) {
	from := *b.t.Position
	d := session.HaversineMeters(from.Lat, from.Lng, p.Lat, p.Lng)
	route := []session.Position{}
	if d > 40 {
		// One or two bends off the straight line: streets, paths,
		// someone wanting a look at the view.
		for i, n := 0, 1+b.rng().IntN(2); i < n; i++ {
			f := (float64(i) + 0.3 + b.rng().Float64()*0.4) / float64(n)
			mid := session.Position{Lat: from.Lat + (p.Lat-from.Lat)*f, Lng: from.Lng + (p.Lng-from.Lng)*f}
			route = append(route, offset(mid, d*(0.05+b.rng().Float64()*0.2), b.rng().Float64()*2*math.Pi))
		}
	}
	route = append(route, p)
	// From the end of any walk still under way, so the path runs forward.
	t := b.r.now
	if n := len(b.out.Path); n > 0 {
		t = math.Max(t, b.out.Path[n-1].T)
	}
	at := from
	for _, q := range route {
		leg := session.HaversineMeters(at.Lat, at.Lng, q.Lat, q.Lng)
		// Pace varies leg by leg: a hill, a chat, a run for it.
		t += leg / (b.pace * (0.75 + b.rng().Float64()*0.5))
		b.out.Path = append(b.out.Path, Waypoint{T: round(t, 1), Lat: q.Lat, Lng: q.Lng})
		b.out.Walked += leg
		b.t.WalkedMetres += leg
		at = q
	}
	b.t.Position = &p
	// Arriving, the team gets its bearings before it does anything.
	b.wake = t + 5 + b.rng().Float64()*20
}

// inside is a point in zone z, not always its centre.
func (b *bot) inside(z graph.Zone) session.Position {
	c := centre(z)
	if !z.IsPolygon() {
		return offset(c, z.RadiusM*0.6*math.Sqrt(b.rng().Float64()), b.rng().Float64()*2*math.Pi)
	}
	for range 10 {
		p := offset(c, 15*b.rng().Float64(), b.rng().Float64()*2*math.Pi)
		if session.ZoneContains(z, p.Lat, p.Lng) {
			return p
		}
	}
	return c
}

// --- steps ----------------------------------------------------------------

func (b *bot) chooseStart(v session.View) {
	if b.t.Lang == "" && len(v.Languages) > 1 && b.rng().Float64() < 0.2 {
		lang := v.Languages[1+b.rng().IntN(len(v.Languages)-1)]
		if _, ok := b.do("language", func() (session.Result, *walk.Step, error) { return session.Result{}, nil, b.t.SetLanguage(lang) }); ok {
			b.r.emit(b.id, "language", "", lang)
		}
	}
	// The nearest start, mostly; sometimes one the team likes the look of.
	opts := v.Starts
	pick := opts[b.rng().IntN(len(opts))]
	if b.rng().Float64() < b.greedy {
		pick = opts[b.nearest(len(opts), func(i int) *graph.Zone { return opts[i].Zone })]
	}
	if !b.goTo(pick.Zone) {
		return
	}
	if _, ok := b.do("start", func() (session.Result, *walk.Step, error) { return b.t.ChooseStart(pick.Node, "") }); ok {
		b.later(10 + b.rng().Float64()*30)
	}
}

// nearest is the index of the option whose zone is closest; an option
// with no zone is no walk at all.
func (b *bot) nearest(n int, zone func(int) *graph.Zone) int {
	best, bestD := 0, math.Inf(1)
	for i := range n {
		d := 0.0
		if z := zone(i); z != nil && b.t.Position != nil {
			c := centre(*z)
			d = session.HaversineMeters(b.t.Position.Lat, b.t.Position.Lng, c.Lat, c.Lng)
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// reading is how long a team spends on a step's words before acting.
func (b *bot) reading(v session.View) float64 {
	words := len(strings.Fields(v.Text)) + len(strings.Fields(v.Title))
	for _, bl := range v.Blocks {
		words += len(strings.Fields(bl.Text))
	}
	// Around three words a second, read aloud to the team.
	return 8 + float64(words)/3*(0.6+b.rng().Float64()*0.8)
}

// begin starts work on the step once the team is there, reporting
// whether it has already been reading long enough.
func (b *bot) begin(v session.View) bool {
	if b.working >= 0 {
		return true
	}
	b.working = b.r.now
	b.later(b.reading(v))
	return false
}

func (b *bot) story(v session.View) {
	if !b.goTo(v.Zone) {
		return
	}
	if !b.begin(v) {
		return
	}
	if b.pickBlocks(v) {
		return
	}
	b.nudged = false
	if _, ok := b.do("continue", func() (session.Result, *walk.Step, error) { return b.t.Continue(v.Node) }); ok {
		b.later(3 + b.rng().Float64()*10)
	}
}

// pickBlocks answers the step's quiz questions, tie-breakers and the
// like, one per look, reporting whether it picked one.
func (b *bot) pickBlocks(v session.View) bool {
	n, _ := b.r.g.Node(v.Node)
	for _, bv := range v.Blocks {
		if bv.Picked != "" || bv.Locked {
			continue
		}
		var choice string
		switch bv.Kind {
		case graph.BlockQuiz:
			choice = graph.QuizChoices[b.rng().IntN(len(graph.QuizChoices))]
			if src := blockByID(n, bv.ID); src != nil && src.Answer != "" && b.rng().Float64() < 0.35+0.5*b.skill {
				choice = src.Answer
			}
		case graph.BlockTiebreak:
			target := 100.0
			if src := blockByID(n, bv.ID); src != nil && src.Target != nil {
				target = *src.Target
			}
			// A guess is off by up to half, less for a better team.
			choice = fmt.Sprintf("%g", math.Round(target*(1+(b.rng().Float64()*2-1)*0.5*(1-b.skill))))
		case graph.BlockSuspects:
			if len(bv.Cards) < 2 {
				continue
			}
			choice = fmt.Sprintf("%d", b.rng().IntN(len(bv.Cards)))
		case graph.BlockDraw:
			choice = "0"
		default:
			continue
		}
		if _, ok := b.do("pick", func() (session.Result, *walk.Step, error) { return b.t.Pick(v.Node, bv.ID, choice) }); ok {
			b.r.emit(b.id, "pick", v.Node, choice)
			b.later(15 + b.rng().Float64()*45)
			return true
		}
	}
	return false
}

func (b *bot) hub(v session.View) {
	if !b.begin(v) {
		return
	}
	var open []session.OfferEntry
	for _, o := range v.Offer {
		if !o.Locked && !o.Visited {
			open = append(open, o)
		}
	}
	// Enough done and the clock against it, or nothing new: move on.
	if v.CanFinish && (len(open) == 0 || b.rng().Float64() < 0.25) {
		if _, ok := b.do("choose", func() (session.Result, *walk.Step, error) { return b.t.Choose(v.Node, graph.PortDone) }); ok {
			return
		}
	}
	// Nothing new here in a home hub's branch: back to the hub above, as
	// a team done with one suspect goes back to the case.
	if v.BackTo != "" && (len(open) == 0 || b.rng().Float64() < 0.1) {
		if _, ok := b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) }); ok {
			return
		}
	}
	// Nothing new open, but a branch waits on the players being in
	// different places: split up to open it, before going round again.
	if len(open) == 0 && b.spreadOut(v) {
		return
	}
	if len(open) == 0 {
		for _, o := range v.Offer {
			if !o.Locked {
				open = append(open, o)
			}
		}
	}
	if len(open) == 0 {
		b.waitFor(v)
		return
	}
	if b.goingRound(v.Node) {
		return
	}
	// The nearest branch, mostly — but teams also pick what sounds fun.
	pick := open[b.rng().IntN(len(open))]
	if b.rng().Float64() < b.greedy {
		pick = open[b.nearest(len(open), func(i int) *graph.Zone { return b.branchZone(v.Node, open[i].Port) })]
	}
	if _, ok := b.do("choose", func() (session.Result, *walk.Step, error) { return b.t.Choose(v.Node, pick.Port) }); ok {
		b.later(5 + b.rng().Float64()*15)
		return
	}
	// Full, most likely: try again soon, perhaps another.
	b.later(60 + b.rng().Float64()*120)
}

// goingRound reports whether the team has kept choosing at the hub with
// nothing new to show since it first did — no step reached or answered,
// nothing gained — for as long as a team may find nothing to do. That is
// a team going round between a hub and a gate it can never pass, a key
// out of reach, and it is given up on.
func (b *bot) goingRound(hub string) bool {
	key := hub + "\x00" + progress(b.t)
	if key != b.round {
		b.round, b.roundSince = key, b.r.now
		return false
	}
	if b.r.now-b.roundSince <= stallAfter {
		return false
	}
	b.stall("keeps coming back here with nothing new to do; a key may be out of reach")
	return true
}

// progress is how far a team has got, as one string.
func progress(t *session.Team) string {
	outcomes := 0
	for _, s := range t.Nodes {
		if s.HasOutcome || s.Solved {
			outcomes++
		}
	}
	return fmt.Sprint(len(t.Nodes), outcomes, t.Points, t.Items, t.Counters, len(t.JournalOrder), len(t.Picks), t.Entered)
}

// --- splitting up ---------------------------------------------------------

// spreadOut sends the team's players to the zones a locked branch of the
// hub waits on, one zone per player: the way a real team opens a branch
// that wants someone at the gate and someone at the bridge. A branch
// waiting on one zone the team walks to together. It reports
// whether the team did; a team too small for what every branch asks, or
// already spread as asked, waits like any other.
func (b *bot) spreadOut(v session.View) bool {
	n, _ := b.r.g.Node(v.Node)
	if n.HubCfg == nil || b.t.Position == nil {
		return false
	}
	for _, o := range v.Offer {
		if !o.Locked {
			continue
		}
		var want map[string]int
		for _, br := range n.HubCfg.Branches {
			if br.Port == o.Port {
				want = cond.Spread(br.Condition)
			}
		}
		total := 0
		for _, k := range want {
			total += k
		}
		if total == 0 || total > max(b.t.Size, 1) {
			continue
		}
		zones := slices.Sorted(maps.Keys(want))
		var posts []graph.Zone
		for _, slug := range zones {
			z, ok := b.r.zone(slug)
			if !ok {
				posts = nil
				break
			}
			for range want[slug] {
				posts = append(posts, z)
			}
		}
		if posts == nil || b.spreadAs(posts) {
			continue
		}
		// One place is somewhere to go, not a reason to split: the team
		// walks there together.
		if len(zones) == 1 && b.t.Phones == nil {
			if b.goTo(&posts[0]) {
				continue
			}
			return true
		}
		b.split(v.Node, posts)
		return true
	}
	return false
}

// split sends one player to each of posts, the rest staying where the
// team is, and wakes the team once the last has arrived.
func (b *bot) split(node string, posts []graph.Zone) {
	from := *b.t.Position
	phones := make([]session.Position, max(b.t.Size, 1))
	arrive := b.r.now
	for i := range phones {
		phones[i] = from
		if i < len(posts) {
			phones[i] = b.inside(posts[i])
			d := session.HaversineMeters(from.Lat, from.Lng, phones[i].Lat, phones[i].Lng)
			arrive = math.Max(arrive, b.r.now+d/(b.pace*(0.75+b.rng().Float64()*0.5)))
		}
	}
	b.t.Phones = phones
	// The map follows the first player, who keeps the team's phone.
	b.t.Position = &phones[0]
	b.out.Path = append(b.out.Path, Waypoint{T: round(arrive, 1), Lat: phones[0].Lat, Lng: phones[0].Lng})
	var names []string
	for _, z := range posts {
		if !slices.Contains(names, z.Name) {
			names = append(names, z.Name)
		}
	}
	b.r.emit(b.id, "split", node, strings.Join(names, ", "))
	b.wake = arrive + 5 + b.rng().Float64()*20
}

// launch turns a two-key launch's keys: at once, the team being all its
// players — after sending one to each post first, where it has posts.
func (b *bot) launch(v session.View, n graph.Node) {
	a := n.PuzzleCfg.Answer
	if len(a.Posts) > 0 && b.t.Position != nil {
		var posts []graph.Zone
		for _, slug := range a.Posts[:min(len(a.Posts), max(b.t.Size, 1))] {
			if z, ok := b.r.zone(slug); ok {
				posts = append(posts, z)
			}
		}
		if len(posts) == 1 && b.t.Phones == nil {
			if !b.goTo(&posts[0]) {
				return
			}
		} else if !b.spreadAs(posts) {
			b.split(v.Node, posts)
			return
		}
	}
	// Logged as solving it, which is what turning every key is.
	if res, ok := b.do("right", func() (session.Result, *walk.Step, error) { return b.t.Turn(v.Node) }); ok && res.Accepted {
		b.later(5 + b.rng().Float64()*15)
		return
	}
	b.waitFor(v)
}

// spreadAs reports whether the team is split up already with a player in
// each of posts, so it does not set out again while it waits.
func (b *bot) spreadAs(posts []graph.Zone) bool {
	if b.t.Phones == nil || len(posts) > len(b.t.Phones) {
		return false
	}
	for i, z := range posts {
		if !session.ZoneContains(z, b.t.Phones[i].Lat, b.t.Phones[i].Lng) {
			return false
		}
	}
	return true
}

// gather brings a split team back together where its first player is,
// once it moves on: the others walk there before it does anything else.
func (b *bot) gather() {
	if b.t.Phones == nil || b.t.Position == nil {
		return
	}
	at := *b.t.Position
	far := 0.0
	for _, p := range b.t.Phones {
		far = math.Max(far, session.HaversineMeters(p.Lat, p.Lng, at.Lat, at.Lng))
	}
	b.t.Phones = nil
	b.regroup = b.r.now + far/b.pace
}

// branchZone is where a hub branch would first send the team, found by
// trying it on a copy.
func (b *bot) branchZone(hub, port string) *graph.Zone {
	c := cloneTeam(b.t)
	_, step, err := c.Choose(hub, port)
	if err != nil || step == nil {
		return nil
	}
	c.Apply(*step)
	return c.View().Zone
}

func (b *bot) puzzle(v session.View) {
	n, _ := b.r.g.Node(v.Node)
	cfg := n.PuzzleCfg
	// A step done in parallel is met at its zone; once its tasks show,
	// the team goes where they are instead.
	tasksShown := cfg.Kind() == graph.AnswerTasks && slices.ContainsFunc(v.Blocks, func(bv session.BlockView) bool { return bv.Kind == graph.BlockTask })
	if !tasksShown && !b.goTo(v.Zone) {
		return
	}
	if !b.begin(v) {
		return
	}
	if v.Input != nil && v.Input.Missing && v.Input.Leave {
		b.do("leave", func() (session.Result, *walk.Step, error) { return b.t.Leave(v.Node) })
		b.later(5)
		return
	}
	if b.pickBlocks(v) {
		return
	}
	if v.BlockedUntil != nil && v.BlockedUntil.After(b.t.Now) {
		b.wake = v.BlockedUntil.Sub(b.r.c.Start).Seconds() + 1 + b.rng().Float64()*10
		return
	}
	spent := b.r.now - b.working
	// The step's own time limit: take its timeout route when it is due.
	if v.HasTimeout && v.Deadline != nil && !b.t.Now.Before(*v.Deadline) {
		b.r.stat(v.Node).Timeouts++
		b.do("timeout", func() (session.Result, *walk.Step, error) { return b.t.Timeout(v.Node) })
		return
	}
	// A hint, when one is open and the team has had enough — sooner for
	// an impatient team, at once if the arranger nudged.
	if h := v.NextHint; h != nil && !h.AvailableAt.After(b.t.Now) {
		waited := b.t.Now.Sub(h.AvailableAt).Seconds()
		if b.nudged || waited > b.patient*600 || b.rng().Float64() < 0.1*(1-b.patient) {
			if _, ok := b.do("hint", func() (session.Result, *walk.Step, error) { return b.t.RevealHint(v.Node, h.Index) }); ok {
				b.out.Hints++
				b.r.stat(v.Node).Hints++
				b.r.emit(b.id, "hint", v.Node, fmt.Sprintf("%d", h.Index+1))
				b.chance += 0.25
				if b.nudged {
					b.nudged = false
					b.r.emit(b.id, "nudge", v.Node, "took a hint")
				}
				b.later(20 + b.rng().Float64()*40)
				return
			}
		}
	}
	// Walk away from a step in a home hub's branch and come back to it
	// later, as teams do when another branch looks easier.
	if v.BackTo != "" && spent > 900+b.patient*900 && b.rng().Float64() < 0.2 {
		b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) })
		return
	}
	// Given up: skip when the step allows it and the team is beaten.
	if v.CanSkip && spent > 600+b.patient*1200 && b.rng().Float64() < 0.4 {
		if _, ok := b.do("skip", func() (session.Result, *walk.Step, error) { return b.t.Skip(v.Node) }); ok {
			b.out.Skips++
			b.r.stat(v.Node).Skips++
		}
		return
	}
	if b.nudged {
		// No hint to take: the nudge itself helps.
		b.nudged = false
		b.chance += 0.3
		b.r.emit(b.id, "nudge", v.Node, "read the nudge")
	}
	// Explored a puzzle with many answers enough: back to the hub.
	if v.BackTo != "" && len(b.given[n.Slug]) >= 2 && b.rng().Float64() < 0.3 {
		b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) })
		return
	}
	if cfg.Kind() == graph.AnswerSearch {
		b.search(v, n)
		return
	}
	if cfg.Kind() == graph.AnswerKeys {
		b.launch(v, n)
		return
	}
	b.attempt(v, n, spent)
}

// attempt has a go at the answer: right by a chance that grows with
// skill, time spent, hints and nudges; otherwise wrong.
func (b *bot) attempt(v session.View, n graph.Node, spent float64) {
	cfg := n.PuzzleCfg
	// Harder puzzles have more hints; each makes the first go less sure.
	hard := 1 + 0.35*float64(len(cfg.Hints))
	p := clamp((0.15+0.6*b.skill)/hard+spent/1800+b.chance, 0.03, 0.97)
	if cfg.Kind() == graph.AnswerQuestions {
		p = 1 // the questions were the puzzle; this is the button
	}
	if cfg.Kind() == graph.AnswerTasks {
		// The team does every role's task, each at its place; the last
		// moves it on. One not yet got right waits for the next go.
		if b.rng().Float64() >= p {
			return
		}
		for _, blk := range n.Blocks() {
			if blk.Kind != graph.BlockTask || b.t.Picks[session.PickKey(n.Slug, blk.ID)] != "" {
				continue
			}
			if z, ok := b.r.zone(blk.Zone); ok && !b.goTo(&z) {
				return
			}
			if _, ok := b.do("task", func() (session.Result, *walk.Step, error) { return b.t.Task(v.Node, blk.ID, blk.Answer) }); !ok {
				return
			}
		}
		return
	}
	if b.rng().Float64() < p {
		o, guess, ok := b.rightAnswer(n)
		if !ok && len(b.given[n.Slug]) > 0 && v.BackTo != "" {
			// Every answer it knows is given.
			b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) })
			return
		}
		if ok && !b.goTo(b.outputZone(o)) {
			return
		}
		if ok && o.Inverted && o.Zone != "" && b.t.InZone(o.Zone) {
			// "Only if you are not here": step out first.
			b.walk(offset(*b.t.Position, 150, b.rng().Float64()*2*math.Pi))
			return
		}
		if ok {
			res, done := b.do("right", func() (session.Result, *walk.Step, error) { return b.t.Answer(v.Node, guess) })
			if done && res.Accepted {
				mark(&b.given, n.Slug, res.Port)
				b.later(5 + b.rng().Float64()*15)
				return
			}
			if done {
				// The bot's right answer was not right after all.
				b.countWrong(v.Node)
			}
		}
		switch {
		case !ok && cfg.UsesMatchers():
			// No answer a bot can type (a pattern, say): pass it as the
			// designer's simulator does, and say so.
			for _, out := range cfg.Outputs {
				if out.Trigger == graph.OnAnswer {
					if _, ok := b.do("forced", func() (session.Result, *walk.Step, error) { return b.t.ForceSolve(v.Node, out.Port) }); ok {
						b.r.stat(n.Slug).Forced++
					}
					return
				}
			}
		case !ok && cfg.Kind() == graph.AnswerItems:
			// It knows the answer but lacks the things: go find them, if
			// there is a way back out.
			if v.BackTo != "" {
				b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) })
				return
			}
			// Or another group may find them.
			if b.hop(v) {
				return
			}
		}
	}
	guess := b.wrongAnswer(n)
	res, ok := b.do("wrong", func() (session.Result, *walk.Step, error) { return b.t.Answer(v.Node, guess) })
	if ok && !res.Accepted {
		b.countWrong(v.Node)
		b.r.emit(b.id, "wrong", v.Node, guess)
	}
	if ok && b.wake <= b.r.now {
		b.later(b.think(cfg))
	}
}

func (b *bot) countWrong(node string) {
	b.out.Wrong++
	b.r.stat(node).Wrong++
}

// think is how long until the next go.
func (b *bot) think(cfg *graph.PuzzleConfig) float64 {
	base := 40 + 160*(1-b.skill)
	if cfg.Kind() == graph.AnswerTiles || cfg.Kind() == graph.AnswerLock || cfg.Kind() == graph.AnswerOrder {
		base *= 1.5
	}
	return base * (0.5 + b.rng().Float64())
}

// search walks to the places a search might be, trying each: the right
// one eventually, by chance or by elimination. A team remembers where it
// has searched, so at a search it comes back to it looks somewhere new.
func (b *bot) search(v session.View, n graph.Node) {
	cfg := n.PuzzleCfg
	var right, wrong []graph.Zone
	mine := map[string]bool{}
	for _, o := range cfg.Outputs {
		if o.Trigger == graph.OnAnswer && o.Zone != "" {
			mine[o.Zone] = true
		}
	}
	for _, z := range b.r.g.Zones {
		switch {
		case b.searched[n.Slug][z.Slug]:
		case mine[z.Slug]:
			right = append(right, z)
		case cfg.Answer.Group != "" && z.Group == cfg.Answer.Group:
			wrong = append(wrong, z)
		}
	}
	// Search where the team stands, if it is in one of the places.
	for _, z := range append(append([]graph.Zone{}, right...), wrong...) {
		if b.t.InZone(z.Slug) {
			mark(&b.searched, n.Slug, z.Slug)
			res, ok := b.do("right", func() (session.Result, *walk.Step, error) { return b.t.Answer(v.Node, "") })
			if ok && res.Accepted {
				mark(&b.given, n.Slug, res.Port)
			} else if ok {
				b.countWrong(v.Node)
				b.r.emit(b.id, "wrong", v.Node, z.Name)
				b.later(10 + b.rng().Float64()*20)
			}
			return
		}
	}
	next := wrong
	if len(right) > 0 && (len(wrong) == 0 || b.rng().Float64() < clamp(0.3+0.4*b.skill+b.chance, 0, 0.95)) {
		next = right
	}
	if len(next) == 0 {
		if len(mine) == 0 {
			// A search with no places: press the button where it stands.
			b.do("right", func() (session.Result, *walk.Step, error) { return b.t.Answer(v.Node, "") })
			b.later(b.think(cfg))
			return
		}
		// Searched everywhere there is: done here.
		if v.BackTo != "" {
			b.do("back", func() (session.Result, *walk.Step, error) { return b.t.Back(v.Node) })
			return
		}
		b.waitFor(v)
		return
	}
	z := next[b.rng().IntN(len(next))]
	b.walk(b.inside(z))
}

// outputZone is where an answer output wants the team, if anywhere.
func (b *bot) outputZone(o graph.Output) *graph.Zone {
	if o.Zone == "" || o.Inverted {
		return nil
	}
	if z, ok := b.r.zone(o.Zone); ok {
		return &z
	}
	return nil
}

// rightAnswer is a right answer the bot can give, and the output it
// takes: one it has not taken here before, at random.
func (b *bot) rightAnswer(n graph.Node) (graph.Output, string, bool) {
	type answer struct {
		o     graph.Output
		guess string
	}
	var fresh, catchAll []answer
	for _, o := range n.PuzzleCfg.Outputs {
		if o.Trigger != graph.OnAnswer || b.given[n.Slug][o.Port] {
			continue
		}
		g, ok := b.answerFor(n, o)
		switch {
		case !ok:
		case len(o.Matchers) == 1 && o.Matchers[0].Kind == match.Anything:
			// "Anything else" is where a wrong answer goes, not a right one.
			catchAll = append(catchAll, answer{o, g})
		default:
			fresh = append(fresh, answer{o, g})
		}
	}
	if len(fresh) == 0 {
		fresh = catchAll
	}
	if len(fresh) == 0 {
		return graph.Output{}, "", false
	}
	a := fresh[b.rng().IntN(len(fresh))]
	return a.o, a.guess, true
}

// answerFor is a guess output o of puzzle n accepts, if the bot can
// make one.
func (b *bot) answerFor(n graph.Node, o graph.Output) (string, bool) {
	cfg := n.PuzzleCfg
	a := cfg.Answer
	switch cfg.Kind() {
	case graph.AnswerTiles:
		return a.Solution(), true
	case graph.AnswerOrder:
		return orderSolution(a), true
	case graph.AnswerQuestions:
		return "", true
	case graph.AnswerItems:
		if !b.carries(a.Needs()) {
			return "", false
		}
		parts := make([]string, len(a.Slots))
		for i, s := range a.Slots {
			parts[i] = s.Item
		}
		return strings.Join(parts, "|"), true
	case graph.AnswerChoice:
		if a.Graded() {
			parts := make([]string, len(a.Fields))
			for i, f := range a.Fields {
				parts[i] = f.Answer
			}
			return strings.Join(parts, "|"), true
		}
	}
	for _, m := range o.Matchers {
		if g, ok := satisfy(m); ok && match.Any(o.Matchers, g).OK {
			return g, true
		}
	}
	return "", false
}

func (b *bot) carries(need map[string]int) bool {
	for slug, q := range need {
		if b.t.Items[slug] < q {
			return false
		}
	}
	return true
}

// satisfy is a guess matcher m accepts, if one can be made from it.
func satisfy(m match.Matcher) (string, bool) {
	switch m.Kind {
	case match.Exact, match.Contains, match.Regex:
		return m.Value, m.Value != ""
	case match.Interval:
		return fmt.Sprintf("%g", (m.Low+m.High)/2), true
	case match.Near:
		return fmt.Sprintf("%g", m.Target), true
	case match.Set:
		return strings.Join(m.Members, ", "), len(m.Members) > 0
	case match.Anything:
		return "anything", true
	}
	return "", false
}

// wrongGuesses are what teams type when they have not got it, for an
// adventure with no words of its own to guess from.
var wrongGuesses = []string{"north", "seven", "the old oak", "1848", "lighthouse", "red", "under the bridge", "42", "clock", "anna"}

// contentGuesses are wrong guesses in the adventure's own words: near
// misses of the step's answers (a letter dropped, two swapped), then the
// answers of its other steps and its options' labels. In whatever
// language the adventure is written, so the commonest wrong guesses say
// something about it.
func contentGuesses(g *graph.Graph, n graph.Node) []string {
	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	exacts := func(cfg *graph.PuzzleConfig) []string {
		var vs []string
		for _, o := range cfg.Outputs {
			for _, m := range o.Matchers {
				if m.Kind == match.Exact {
					vs = append(vs, m.Value)
				}
			}
		}
		return vs
	}
	for _, v := range exacts(n.PuzzleCfg) {
		r := []rune(strings.TrimSpace(v))
		if len(r) > 2 {
			add(string(r[:len(r)-1]))
			add(string(append([]rune{r[1], r[0]}, r[2:]...)))
		}
	}
	for _, o := range g.Nodes {
		if o.Slug == n.Slug || o.PuzzleCfg == nil {
			continue
		}
		for _, v := range exacts(o.PuzzleCfg) {
			add(v)
		}
		if a := o.PuzzleCfg.Answer; a != nil {
			for _, f := range a.Fields {
				for _, opt := range f.Options {
					add(opt.Label)
				}
			}
		}
	}
	return out
}

// orderSolution is the guess that puts an order puzzle's dealt cards
// right: for each card in turn, where it was dealt.
func orderSolution(a *graph.Answer) string {
	deal := a.Deal()
	parts := make([]string, len(deal))
	for at, card := range deal {
		parts[card] = strconv.Itoa(at)
	}
	return strings.Join(parts, ",")
}

// wrongAnswer is a plausible wrong guess for the puzzle's kind.
func (b *bot) wrongAnswer(n graph.Node) string {
	cfg := n.PuzzleCfg
	a := cfg.Answer
	var g string
	switch cfg.Kind() {
	case graph.AnswerTiles:
		s := strings.Split(a.Solution(), ",")
		b.rng().Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
		g = strings.Join(s, ",")
	case graph.AnswerOrder:
		// The cards as dealt, untouched.
		parts := make([]string, len(a.Cards))
		for i := range parts {
			parts[i] = strconv.Itoa(i)
		}
		g = strings.Join(parts, ",")
	case graph.AnswerLock:
		parts := make([]string, a.Wheels)
		for i := range parts {
			if len(a.Symbols) > 0 {
				parts[i] = a.Symbols[b.rng().IntN(len(a.Symbols))].Value
			}
		}
		g = strings.Join(parts, "")
	case graph.AnswerChoice:
		parts := make([]string, len(a.Fields))
		for i, f := range a.Fields {
			if len(f.Options) > 0 {
				parts[i] = f.Options[b.rng().IntN(len(f.Options))].Value
			}
		}
		g = strings.Join(parts, "|")
	case graph.AnswerItems:
		parts := make([]string, len(a.Slots))
		for i := range a.Slots {
			parts[i] = a.Slots[len(a.Slots)-1-i].Item
		}
		g = strings.Join(parts, "|")
	default:
		pool := contentGuesses(b.r.g, n)
		if len(pool) == 0 {
			pool = wrongGuesses
		}
		g = pool[b.rng().IntN(len(pool))]
	}
	// Never by accident the right one.
	for _, o := range cfg.Outputs {
		if o.Trigger == graph.OnAnswer && len(o.Matchers) > 0 && match.Any(o.Matchers, g).OK {
			return g + " ?"
		}
	}
	return g
}

// occupancy is how many other teams are in each hub branch, which a
// branch's capacity reads.
func (r *runner) occupancy(self *bot) map[string]int {
	occ := map[string]int{}
	for _, b := range r.bots {
		if b == self {
			continue
		}
		for k := range b.open {
			occ[k]++
		}
	}
	return occ
}

// --- helpers --------------------------------------------------------------

func blockByID(n graph.Node, id string) *graph.Block {
	for _, bl := range n.Blocks() {
		if bl.ID == id {
			return &bl
		}
	}
	return nil
}

// centre is a zone's middle: its centre, or a polygon's corners' mean.
func centre(z graph.Zone) session.Position {
	if !z.IsPolygon() {
		return session.Position{Lat: z.Lat, Lng: z.Lng}
	}
	var lat, lng float64
	for _, c := range z.Polygon {
		lat += c[0]
		lng += c[1]
	}
	k := float64(len(z.Polygon))
	p := session.Position{Lat: lat / k, Lng: lng / k}
	if session.ZoneContains(z, p.Lat, p.Lng) {
		return p
	}
	return session.Position{Lat: z.Lat, Lng: z.Lng}
}

// offset is the point d metres from p in direction a (radians).
func offset(p session.Position, d, a float64) session.Position {
	const earth = 6371000.0
	dLat := d * math.Cos(a) / earth
	dLng := d * math.Sin(a) / (earth * math.Cos(p.Lat*math.Pi/180))
	return session.Position{Lat: p.Lat + dLat*180/math.Pi, Lng: p.Lng + dLng*180/math.Pi}
}

func clamp(v, lo, hi float64) float64 { return math.Min(hi, math.Max(lo, v)) }

func round(v float64, places int) float64 {
	k := math.Pow(10, float64(places))
	return math.Round(v*k) / k
}

// cloneTeam is a deep copy, so a refused action leaves the team as it was
// and a branch can be tried without taking it.
func cloneTeam(t *session.Team) *session.Team {
	c := *t
	c.Nodes = make(map[string]*session.NodeState, len(t.Nodes))
	for k, v := range t.Nodes {
		row := *v
		c.Nodes[k] = &row
	}
	c.Items = copyMap(t.Items)
	c.Journal = copyMap(t.Journal)
	c.Counters = copyMap(t.Counters)
	c.Picks = copyMap(t.Picks)
	c.Occ = copyMap(t.Occ)
	c.JournalOrder = append([]string(nil), t.JournalOrder...)
	c.Entered = make(map[string]map[string]bool, len(t.Entered))
	for k, v := range t.Entered {
		c.Entered[k] = copyMap(v)
	}
	if t.Position != nil {
		p := *t.Position
		c.Position = &p
	}
	if t.Phones != nil {
		c.Phones = slices.Clone(t.Phones)
	}
	return &c
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	maps.Copy(out, m)
	return out
}
