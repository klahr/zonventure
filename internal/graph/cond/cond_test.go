package cond

import (
	"encoding/json"
	"testing"
	"time"
)

// fakeState is a hand-built State. The whole point of the interface is
// that a condition test needs no database and no clock.
type fakeState struct {
	solved      map[string]bool
	visited     map[string]bool
	outcomes    map[string]string
	attempts    map[string]int
	items       map[string]int
	counters    map[string]float64
	score       float64
	elapsed     float64
	nodeElapsed float64
	teamSize    int
	zones       map[string]bool
	walked      float64
	picks       map[string]string
	now         time.Time
}

func newState() *fakeState {
	return &fakeState{
		solved: map[string]bool{}, visited: map[string]bool{},
		outcomes: map[string]string{}, attempts: map[string]int{},
		items: map[string]int{}, counters: map[string]float64{},
		zones:    map[string]bool{},
		now:      time.Date(2026, 9, 22, 14, 30, 0, 0, time.UTC), // a Tuesday
		teamSize: 4,
	}
}

func (f *fakeState) Solved(n string) bool  { return f.solved[n] }
func (f *fakeState) Visited(n string) bool { return f.visited[n] }
func (f *fakeState) Outcome(n string) (string, bool) {
	v, ok := f.outcomes[n]
	return v, ok
}
func (f *fakeState) Attempts(n string) int         { return f.attempts[n] }
func (f *fakeState) ItemQty(i string) int          { return f.items[i] }
func (f *fakeState) CounterValue(c string) float64 { return f.counters[c] }
func (f *fakeState) Score() float64                { return f.score }
func (f *fakeState) ElapsedSeconds() float64       { return f.elapsed }
func (f *fakeState) NodeElapsedSeconds() float64   { return f.nodeElapsed }
func (f *fakeState) TeamSize() int                 { return f.teamSize }
func (f *fakeState) InZone(z string) bool          { return f.zones[z] }
func (f *fakeState) Walked() float64               { return f.walked }
func (f *fakeState) Picked(n, b string) string     { return f.picks[n+"/"+b] }
func (f *fakeState) LocalTime() time.Time          { return f.now }

// An empty predicate is the shape of a freshly-dragged Gate. It must be
// open, or an author has to satisfy a condition they never wrote.
func TestEmptyPredicateIsOpen(t *testing.T) {
	if !Eval(Predicate{}, newState()) {
		t.Error("an empty predicate must evaluate true")
	}
}

func TestHistoryOperators(t *testing.T) {
	s := newState()
	s.solved["bakery"] = true
	s.visited["bakery"] = true
	s.visited["church"] = true
	s.outcomes["door"] = "left"
	s.attempts["vault"] = 3

	cases := []struct {
		name string
		p    Predicate
		want bool
	}{
		{"solved hit", Predicate{Op: OpSolved, Node: "bakery"}, true},
		{"solved miss", Predicate{Op: OpSolved, Node: "vault"}, false},
		{"visited but unsolved", Predicate{Op: OpVisited, Node: "church"}, true},
		{"visited miss", Predicate{Op: OpVisited, Node: "market"}, false},
		{"outcome hit", Predicate{Op: OpOutcome, Node: "door", Is: "left"}, true},
		{"outcome wrong port", Predicate{Op: OpOutcome, Node: "door", Is: "right"}, false},
		{"outcome node never left", Predicate{Op: OpOutcome, Node: "vault", Is: "left"}, false},
		{"attempts gte default", Predicate{Op: OpAttempts, Node: "vault", N: 3}, true},
		{"attempts below", Predicate{Op: OpAttempts, Node: "vault", N: 4}, false},
		{"attempts lt", Predicate{Op: OpAttempts, Node: "vault", Cmp: Lt, N: 5}, true},
	}
	for _, c := range cases {
		if got := Eval(c.p, s); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// This is the case that makes keys unnecessary: branch A grants nothing,
// yet a gate far away in branch C can still tell you were there.
func TestHistoryIsGlobalNotBranchScoped(t *testing.T) {
	s := newState()
	s.solved["church"] = true // solved in a completely different branch

	p := Predicate{Op: OpAll, Of: []Predicate{
		{Op: OpSolved, Node: "church"},
		{Op: OpNot, Of: []Predicate{{Op: OpSolved, Node: "vault"}}},
	}}
	if !Eval(p, s) {
		t.Error("history from another branch must be visible without any item being granted")
	}
}

func TestComposites(t *testing.T) {
	s := newState()
	s.solved["a"] = true

	yes := Predicate{Op: OpSolved, Node: "a"}
	no := Predicate{Op: OpSolved, Node: "b"}

	cases := []struct {
		name string
		p    Predicate
		want bool
	}{
		{"all true", Predicate{Op: OpAll, Of: []Predicate{yes, yes}}, true},
		{"all with a false", Predicate{Op: OpAll, Of: []Predicate{yes, no}}, false},
		{"any with a true", Predicate{Op: OpAny, Of: []Predicate{no, yes}}, true},
		{"any all false", Predicate{Op: OpAny, Of: []Predicate{no, no}}, false},
		{"not true", Predicate{Op: OpNot, Of: []Predicate{yes}}, false},
		{"not false", Predicate{Op: OpNot, Of: []Predicate{no}}, true},

		// Vacuous cases. "all of nothing" is true by the usual
		// convention; "any of nothing" is false.
		{"empty all", Predicate{Op: OpAll}, true},
		{"empty any", Predicate{Op: OpAny}, false},

		// An empty Not is an author slip. Failing closed is safer than
		// silently opening the gate they meant to restrict.
		{"empty not fails closed", Predicate{Op: OpNot}, false},

		{"nested", Predicate{Op: OpAll, Of: []Predicate{
			yes,
			{Op: OpAny, Of: []Predicate{no, {Op: OpNot, Of: []Predicate{no}}}},
		}}, true},
	}
	for _, c := range cases {
		if got := Eval(c.p, s); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A bare item test must mean "has at least one". With the default Gte
// and a zero N it would otherwise be vacuously true — a gate that looks
// like it requires a crowbar but opens for everyone.
func TestBareItemMeansAtLeastOne(t *testing.T) {
	s := newState()

	p := Predicate{Op: OpItem, Item: "crowbar"}
	if Eval(p, s) {
		t.Error("a bare item test must be false when the team has none")
	}

	s.items["crowbar"] = 1
	if !Eval(p, s) {
		t.Error("a bare item test must be true when the team has one")
	}

	// An explicit quantity still works.
	s.items["coin"] = 3
	if !Eval(Predicate{Op: OpItem, Item: "coin", N: 3}, s) {
		t.Error("explicit quantity should compare with Gte by default")
	}
	if Eval(Predicate{Op: OpItem, Item: "coin", N: 4}, s) {
		t.Error("explicit quantity should fail below the threshold")
	}
	if !Eval(Predicate{Op: OpItem, Item: "coin", Cmp: Eq, N: 3}, s) {
		t.Error("explicit comparison should be honoured")
	}
}

func TestNumericOperators(t *testing.T) {
	s := newState()
	s.score = 150
	s.elapsed = 3600
	s.nodeElapsed = 90
	s.counters["alarm"] = 2

	cases := []struct {
		name string
		p    Predicate
		want bool
	}{
		{"score gte default", Predicate{Op: OpScore, N: 100}, true},
		{"score lt", Predicate{Op: OpScore, Cmp: Lt, N: 100}, false},
		{"elapsed", Predicate{Op: OpElapsed, Cmp: Gt, N: 60}, true},
		{"node elapsed", Predicate{Op: OpNodeElapsed, Cmp: Lte, N: 90}, true},
		{"counter eq", Predicate{Op: OpCounter, Counter: "alarm", Cmp: Eq, N: 2}, true},
		{"counter ne", Predicate{Op: OpCounter, Counter: "alarm", Cmp: Ne, N: 2}, false},
		{"team size", Predicate{Op: OpTeamSize, Cmp: Gte, N: 4}, true},
		{"unknown counter reads zero", Predicate{Op: OpCounter, Counter: "nope", Cmp: Eq, N: 0}, true},
	}
	for _, c := range cases {
		if got := Eval(c.p, s); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInZone(t *testing.T) {
	s := newState()
	s.zones["courtyard"] = true
	if !Eval(Predicate{Op: OpInZone, Zone: "courtyard"}, s) {
		t.Error("should be inside the courtyard")
	}
	// The inverted geofence: "correct only if you are NOT here".
	if Eval(Predicate{Op: OpNot, Of: []Predicate{{Op: OpInZone, Zone: "courtyard"}}}, s) {
		t.Error("negation of in_zone should be false while inside")
	}
	if !Eval(Predicate{Op: OpNot, Of: []Predicate{{Op: OpInZone, Zone: "attic"}}}, s) {
		t.Error("negation of in_zone should be true while outside")
	}
}

// The venue-closes-at-17:00 constraint, which legacy could only express
// as a block time limit that kicked teams out.
// Walked compares metres like any number; Drawn asks which card a draw
// dealt, and nothing dealt yet is no card at all.
func TestWalkedAndDrawn(t *testing.T) {
	s := newState()
	s.walked = 120
	if !Eval(Predicate{Op: OpWalked, N: 100}, s) || Eval(Predicate{Op: OpWalked, N: 150}, s) {
		t.Error("walked 120 m: want ≥ 100 true, ≥ 150 false")
	}
	drew := Predicate{Op: OpDrawn, Node: "tavern", Block: "deck", Is: "2"}
	if Eval(drew, s) {
		t.Error("nothing dealt yet, but drawn held")
	}
	s.picks = map[string]string{"tavern/deck": "2"}
	if !Eval(drew, s) {
		t.Error("dealt card 2, but drawn 2 failed")
	}
	if Eval(Predicate{Op: OpDrawn, Node: "tavern", Block: "deck"}, s) {
		t.Error("a drawn condition naming no card held")
	}
}

func TestWallClock(t *testing.T) {
	s := newState() // 14:30 UTC, a Tuesday

	if !Eval(Predicate{Op: OpTimeOfDay, Cmp: Lt, Is: "17:00"}, s) {
		t.Error("14:30 should be before 17:00")
	}
	if Eval(Predicate{Op: OpTimeOfDay, Cmp: Gte, Is: "17:00"}, s) {
		t.Error("14:30 should not be at or after 17:00")
	}
	// A malformed time must fail closed rather than compare against zero.
	if Eval(Predicate{Op: OpTimeOfDay, Cmp: Lt, Is: "not a time"}, s) {
		t.Error("an unparseable time must fail closed")
	}

	if !Eval(Predicate{Op: OpWeekday, Members: []string{"sat", "tue"}}, s) {
		t.Error("Tuesday should match a list containing tue")
	}
	if !Eval(Predicate{Op: OpWeekday, Members: []string{"Tuesday"}}, s) {
		t.Error("a full weekday name should match")
	}
	if Eval(Predicate{Op: OpWeekday, Members: []string{"sat", "sun"}}, s) {
		t.Error("Tuesday should not match a weekend list")
	}
	if Eval(Predicate{Op: OpWeekday}, s) {
		t.Error("an empty weekday list should match nothing")
	}
}

// A graph authored by a newer build must not silently open a gate this
// build does not understand.
func TestUnknownOperatorFailsClosed(t *testing.T) {
	if Eval(Predicate{Op: "invented_later"}, newState()) {
		t.Error("an unknown operator must evaluate false")
	}
}

func TestCollectGathersEveryReference(t *testing.T) {
	p := Predicate{Op: OpAll, Of: []Predicate{
		{Op: OpSolved, Node: "bakery"},
		{Op: OpAny, Of: []Predicate{
			{Op: OpItem, Item: "crowbar"},
			{Op: OpCounter, Counter: "alarm"},
			{Op: OpNot, Of: []Predicate{{Op: OpInZone, Zone: "attic"}}},
		}},
		{Op: OpOutcome, Node: "bakery", Is: "left"}, // duplicate node
	}}

	r := Collect(p)
	assertSet(t, "nodes", r.Nodes, []string{"bakery"})
	assertSet(t, "items", r.Items, []string{"crowbar"})
	assertSet(t, "counters", r.Counters, []string{"alarm"})
	assertSet(t, "zones", r.Zones, []string{"attic"})
}

func assertSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}

func TestDescribeReadsAsASentence(t *testing.T) {
	p := Predicate{Op: OpAll, Of: []Predicate{
		{Op: OpSolved, Node: "bakery"},
		{Op: OpOutcome, Node: "door", Is: "left"},
		{Op: OpNot, Of: []Predicate{{Op: OpItem, Item: "crowbar"}}},
	}}
	got := Describe(p)
	want := `(solved "bakery" and left "door" via "left" and not has ≥ 1 × "crowbar")`
	if got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}

	if Describe(Predicate{}) != "always" {
		t.Errorf("an empty predicate should describe as always, got %q", Describe(Predicate{}))
	}
	if Describe(Predicate{Op: OpAny}) != "never" {
		t.Errorf("an empty any should describe as never, got %q", Describe(Predicate{Op: OpAny}))
	}
}

// Describe is what the editor shows the author, so every operator needs
// a rendering. An operator added without one would surface in the UI as
// "unknown condition", which is worse than useless when you are trying
// to work out why a gate is shut.
func TestDescribeCoversEveryOperator(t *testing.T) {
	cases := []struct {
		p    Predicate
		want string
	}{
		{Predicate{Op: OpSolved, Node: "a"}, `solved "a"`},
		{Predicate{Op: OpVisited, Node: "a"}, `visited "a"`},
		{Predicate{Op: OpOutcome, Node: "a", Is: "left"}, `left "a" via "left"`},
		{Predicate{Op: OpAttempts, Node: "a", Cmp: Gt, N: 3}, `attempts at "a" > 3`},
		{Predicate{Op: OpItem, Item: "coin", Cmp: Eq, N: 2}, `has = 2 × "coin"`},
		{Predicate{Op: OpCounter, Counter: "alarm", Cmp: Ne, N: 1}, `counter "alarm" ≠ 1`},
		{Predicate{Op: OpScore, Cmp: Lt, N: 50}, `score < 50`},
		{Predicate{Op: OpElapsed, Cmp: Lte, N: 60}, `elapsed ≤ 60s`},
		{Predicate{Op: OpNodeElapsed, N: 30}, `time on this step ≥ 30s`},
		{Predicate{Op: OpTeamSize, Cmp: Gte, N: 2}, `team size ≥ 2`},
		{Predicate{Op: OpInZone, Zone: "attic"}, `inside zone "attic"`},
		{Predicate{Op: OpTimeOfDay, Cmp: Lt, Is: "17:00"}, `time of day < 17:00`},
		{Predicate{Op: OpWeekday, Members: []string{"sat", "sun"}}, `weekday is sat/sun`},
		{Predicate{Op: OpNot}, `never`},
		{Predicate{Op: OpAll, Of: []Predicate{{Op: OpSolved, Node: "a"}}}, `solved "a"`},
		{Predicate{Op: OpAny, Of: []Predicate{
			{Op: OpSolved, Node: "a"}, {Op: OpSolved, Node: "b"},
		}}, `(solved "a" or solved "b")`},
		{Predicate{Op: "invented_later"}, `unknown condition "invented_later"`},
	}
	for _, c := range cases {
		if got := Describe(c.p); got != c.want {
			t.Errorf("Describe(%q) = %q, want %q", c.p.Op, got, c.want)
		}
	}
}

func TestCompareOperators(t *testing.T) {
	cases := []struct {
		cmp  Cmp
		want bool
	}{
		{Eq, false}, {Ne, true}, {Lt, false}, {Lte, false},
		{Gt, true}, {Gte, true}, {"", true},
		{"nonsense", false}, // an unknown comparison fails closed
	}
	s := newState()
	s.score = 10
	for _, c := range cases {
		got := Eval(Predicate{Op: OpScore, Cmp: c.cmp, N: 5}, s)
		if got != c.want {
			t.Errorf("score 10 %q 5 = %v, want %v", c.cmp, got, c.want)
		}
	}
}

// The tree is data, so it must survive a JSON round-trip unchanged —
// that is what lets the editor store it and the validator read it back
// with no parser on either side.
func TestJSONRoundTrip(t *testing.T) {
	p := Predicate{Op: OpAll, Of: []Predicate{
		{Op: OpSolved, Node: "bakery"},
		{Op: OpScore, Cmp: Gte, N: 100},
	}}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back Predicate
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	s := newState()
	s.solved["bakery"] = true
	s.score = 120
	if Eval(p, s) != Eval(back, s) {
		t.Error("a round-tripped predicate must evaluate identically")
	}
	if Describe(p) != Describe(back) {
		t.Errorf("round-trip changed the description: %q vs %q", Describe(p), Describe(back))
	}
}

// Deeply nested trees must not blow the stack. An author cannot write
// recursion, but an import could carry a pathological tree.
func TestDeepNestingTerminates(t *testing.T) {
	p := Predicate{Op: OpSolved, Node: "a"}
	for range 2000 {
		p = Predicate{Op: OpNot, Of: []Predicate{p}}
	}
	s := newState()
	s.solved["a"] = true
	// 2000 negations: even count, so it should come back to true.
	if !Eval(p, s) {
		t.Error("deeply nested negation should still evaluate correctly")
	}
	if len(Collect(p).Nodes) != 1 {
		t.Error("Collect should find the single node reference through the nesting")
	}
}
