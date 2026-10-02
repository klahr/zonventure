package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/cond"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/valid"
	"github.com/klahr/zonventure/internal/graph/walk"
)

// mystery is Obscurium in miniature: a home screen with a scanner, one
// suspect and the accusation. The suspect is reached on location, opens
// with an introduction, and has a hub of topics of their own; the
// "place" topic only appears once the murder topic has been read.
func mystery() *graph.Graph {
	story := func(slug, text string, once bool, unlocks ...string) graph.Node {
		return graph.Node{Slug: slug, Type: graph.Story, Title: slug, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks(text, nil), Once: once}, Unlocks: unlocks}
	}
	answer := func(v string) []graph.Output {
		return []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: v}}}}
	}
	arrive := story("carl-arrive", "Find Carl.", true)
	arrive.StoryCfg.Zone = "office"
	return &graph.Graph{
		Zones:   []graph.Zone{{Slug: "office", Lat: 59.34, Lng: 18.07, RadiusM: 30}},
		Journal: []graph.JournalEntry{{Slug: "carl", Title: "Carl"}, {Slug: "carl-murder", Title: "Carl on the murder"}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "home", Type: graph.Hub, Title: "Obscurium", HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, Branches: []graph.Branch{
				{Port: "scanner", Label: "Scanner"}, {Port: "carl", Label: "Carl"}, {Port: "accuse", Label: "Solve the case"},
			}}},
			story("scanner", "Nothing here.", false),
			arrive,
			story("carl-intro", "Carl works at the foundation.", true, "carl"),
			{Slug: "carl-hub", Type: graph.Hub, Title: "Carl", HubCfg: &graph.HubConfig{Policy: graph.PolicyChoice, Home: true, HideLocked: true, Branches: []graph.Branch{
				{Port: "murder", Label: "The murder"},
				{Port: "place", Label: "The place", Condition: cond.Predicate{Op: cond.OpVisited, Node: "carl-murder-a"}},
			}}},
			{Slug: "carl-murder-q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Blocks: graph.LegacyBlocks("When?", nil), Outputs: answer("1997")}},
			story("carl-murder-a", "Just so.", false, "carl-murder"),
			story("carl-place", "The park.", false),
			{Slug: "accuse", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{Blocks: graph.LegacyBlocks("Who?", nil), Outputs: answer("erika")}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "home"},
			{From: "home", FromPort: "scanner", To: "scanner"},
			{From: "scanner", FromPort: "out", To: "home"},
			{From: "home", FromPort: "carl", To: "carl-arrive"},
			{From: "carl-arrive", FromPort: "out", To: "carl-intro"},
			{From: "carl-intro", FromPort: "out", To: "carl-hub"},
			{From: "carl-hub", FromPort: "murder", To: "carl-murder-q"},
			{From: "carl-murder-q", FromPort: "solved", To: "carl-murder-a"},
			{From: "carl-murder-a", FromPort: "out", To: "carl-hub"},
			{From: "carl-hub", FromPort: "place", To: "carl-place"},
			{From: "carl-place", FromPort: "out", To: "carl-hub"},
			{From: "home", FromPort: "accuse", To: "accuse"},
			{From: "accuse", FromPort: "solved", To: "end"},
		},
	}
}

// act applies an action's step to the team, failing the test on error.
func act(t *testing.T, tm *Team, what string, _ Result, step *walk.Step, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if step != nil {
		tm.Apply(*step)
	}
}

func offered(v View) map[string]OfferEntry {
	out := map[string]OfferEntry{}
	for _, o := range v.Offer {
		out[o.Port] = o
	}
	return out
}

func TestHomeScreen(t *testing.T) {
	tm, step, err := Start(mystery(), t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	v := tm.View()
	if tm.Current != "home" || len(v.Offer) != 3 || v.CanFinish || v.BackTo != "" {
		t.Fatalf("start lands on the home screen, offering all three, no finish, no back: %+v", v)
	}

	// The scanner loops straight back, and stays on offer.
	res, st, err := tm.Choose("home", "scanner")
	act(t, tm, "scanner", res, st, err)
	if v := tm.View(); v.BackTo != "home" || v.BackLabel != "Obscurium" {
		t.Errorf("inside a home branch the team can go back: %+v", v)
	}
	res, st, err = tm.Continue("scanner")
	act(t, tm, "scanner done", res, st, err)
	if o := offered(tm.View()); tm.Current != "home" || !o["scanner"].Visited || len(o) != 3 {
		t.Fatalf("back home, the scanner is visited and still offered: %+v", o)
	}

	// Carl: arrive on location, read the introduction, reach his topics.
	res, st, err = tm.Choose("home", "carl")
	act(t, tm, "carl", res, st, err)
	if _, _, err := tm.Continue("carl-arrive"); err != ErrNotHere {
		t.Fatalf("arriving needs the team at the office, got %v", err)
	}
	tm.Position = &Position{Lat: 59.34, Lng: 18.07}
	res, st, err = tm.Continue("carl-arrive")
	act(t, tm, "arrive", res, st, err)
	res, st, err = tm.Continue("carl-intro")
	act(t, tm, "intro", res, st, err)
	if !tm.Journal["carl"] {
		t.Error("reaching the introduction notes Carl in the journal")
	}
	v = tm.View()
	if tm.Current != "carl-hub" || v.BackTo != "home" {
		t.Fatalf("Carl's topics are a hub of their own, and back leads home: %s %+v", tm.Current, v)
	}
	if o := offered(v); len(o) != 1 {
		t.Errorf("the place topic is hidden until the murder is read: %+v", o)
	}

	// Walk away mid-question, then come back: the introduction is not
	// shown again, and the question is still open.
	res, st, err = tm.Choose("carl-hub", "murder")
	act(t, tm, "murder", res, st, err)
	if v := tm.View(); v.BackTo != "carl-hub" || v.BackLabel != "Carl" {
		t.Errorf("inside a topic, back leads to Carl: %+v", v)
	}
	res, st, err = tm.Back("carl-murder-q")
	act(t, tm, "back to carl", res, st, err)
	if tm.Current != "carl-hub" || tm.Solved("carl-murder-q") {
		t.Fatalf("back leaves the question unanswered: at %s", tm.Current)
	}
	res, st, err = tm.Back("carl-hub")
	act(t, tm, "back home", res, st, err)
	tm.Position = nil
	res, st, err = tm.Choose("home", "carl")
	act(t, tm, "carl again", res, st, err)
	if tm.Current != "carl-hub" {
		t.Fatalf("a second visit skips the arrival and introduction, even away from the office: at %s", tm.Current)
	}

	// Answer, read, and the place opens up.
	res, st, err = tm.Choose("carl-hub", "murder")
	act(t, tm, "murder again", res, st, err)
	res, st, err = tm.Answer("carl-murder-q", "1997")
	act(t, tm, "answer", res, st, err)
	res, st, err = tm.Continue("carl-murder-a")
	act(t, tm, "read", res, st, err)
	if !tm.Journal["carl-murder"] {
		t.Error("the answer's page is noted in the journal")
	}
	if o := offered(tm.View()); !o["murder"].Visited || len(o) != 2 {
		t.Errorf("the murder is visited and the place now on offer: %+v", o)
	}

	// A home screen has no finish; the accusation ends the adventure.
	res, st, err = tm.Back("carl-hub")
	act(t, tm, "home", res, st, err)
	if _, _, err := tm.Choose("home", graph.PortDone); err == nil {
		t.Error("a home screen cannot be finished")
	}
	res, st, err = tm.Choose("home", "accuse")
	act(t, tm, "accuse", res, st, err)
	res, st, err = tm.Answer("accuse", "erika")
	act(t, tm, "solve", res, st, err)
	if !tm.Completed {
		t.Error("solving the case ends the adventure")
	}
	if _, _, err := tm.Back("end"); err == nil {
		t.Error("there is no back from the end")
	}
}

// Back needs a home hub in front of the step.
func TestBackNeedsAHomeHub(t *testing.T) {
	tm := started(t) // the hunt has no hubs at all
	if v := tm.View(); v.BackTo != "" {
		t.Errorf("no hub, no back: %+v", v)
	}
	if _, _, err := tm.Back("door"); err != ErrNotAllowed {
		t.Errorf("back without a home hub is not allowed, got %v", err)
	}
}

// The mystery is a valid graph: a home screen needs no done output, and
// its loops are not progress-free cycles, because every pass waits on
// the team. A home hub that does not let the team choose, and a note
// that names no journal entry, are errors.
func TestHomeScreenValidates(t *testing.T) {
	if r := valid.Check(mystery()); r.HasErrors() {
		t.Fatalf("the mystery should validate: %v", r.Errors())
	}
	g := mystery()
	for i := range g.Nodes {
		switch g.Nodes[i].Slug {
		case "home":
			g.Nodes[i].HubCfg.Policy = graph.PolicySequence
		case "scanner":
			g.Nodes[i].Unlocks = []string{"nope"}
		}
	}
	codes := map[string]bool{}
	for _, i := range valid.Check(g).Errors() {
		codes[i.Code] = true
	}
	if !codes["hub.home.policy"] || !codes["ref.journal.unknown"] {
		t.Errorf("expected hub.home.policy and ref.journal.unknown, got %v", codes)
	}
}

// A step two hubs both reach has no single owner, so no back.
func TestOwnerHubIsUnique(t *testing.T) {
	g := mystery()
	if h, ok := g.OwnerHub("carl-murder-q"); !ok || h != "carl-hub" {
		t.Errorf("a topic belongs to Carl's hub, got %q %v", h, ok)
	}
	if h, ok := g.OwnerHub("carl-hub"); !ok || h != "home" {
		t.Errorf("Carl's hub belongs to the home screen, got %q %v", h, ok)
	}
	if _, ok := g.OwnerHub("home"); ok {
		t.Error("the home screen has no owner")
	}
	// Let the home screen also lead straight to Carl's answer page.
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "home" {
			g.Nodes[i].HubCfg.Branches = append(g.Nodes[i].HubCfg.Branches, graph.Branch{Port: "shortcut"})
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: "home", FromPort: "shortcut", To: "carl-murder-a"})
	if _, ok := g.OwnerHub("carl-murder-a"); ok {
		t.Error("a page both hubs reach has no single owner")
	}
}

// The journal lists notes in the author's order by default, or in the
// order the team found them; a note's picture is fetchable only once
// the note is unlocked.
func TestJournalOrderAndMedia(t *testing.T) {
	g := mystery()
	g.Journal[1].Media = []graph.Media{{ID: 42, Kind: graph.MediaImage}}
	g.Journal[0].Section = "Carl"
	tm := &Team{G: g}
	tm.Init()
	tm.ApplyEffect("x", graph.EffectConfig{Kind: graph.UnlockJournal, JournalEntry: "carl-murder"})
	if !tm.CanSeeMedia(42, true) {
		t.Error("an unlocked note's picture is visible")
	}
	tm.ApplyEffect("x", graph.EffectConfig{Kind: graph.UnlockJournal, JournalEntry: "carl"})
	tm.ApplyEffect("x", graph.EffectConfig{Kind: graph.UnlockJournal, JournalEntry: "carl"})
	v := tm.View()
	if !v.HasJournal || len(v.Journal) != 2 || v.Journal[0].Slug != "carl" || v.Journal[0].Section != "Carl" {
		t.Fatalf("author order, once each, with sections: %+v", v.Journal)
	}
	g.JournalByUnlock = true
	if v := tm.View(); v.Journal[0].Slug != "carl-murder" {
		t.Errorf("found order puts the first note found first: %+v", v.Journal)
	}

	fresh := &Team{G: g}
	fresh.Init()
	if fresh.CanSeeMedia(42, false) {
		t.Error("a locked note's picture is not visible, even without strict assets")
	}
}

// The scanner: pressing search where the team stands takes the output of
// the zone it is in, and says which grid cell that is even where there
// is nothing to find.
func TestSearchByPlace(t *testing.T) {
	cell := func(slug string, lat, lng float64) graph.Zone {
		d := 0.0005
		return graph.Zone{Slug: slug, Name: strings.ToUpper(slug), Group: "grid", Lat: lat, Lng: lng,
			Polygon: [][2]float64{{lat + d, lng - d}, {lat + d, lng + d}, {lat - d, lng + d}, {lat - d, lng - d}}}
	}
	g := &graph.Graph{
		Zones: []graph.Zone{cell("a1", 59.340, 18.070), cell("c3", 59.342, 18.072)},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "scan", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer:     &graph.Answer{Kind: graph.AnswerSearch, Group: "grid"},
				Repeatable: true,
				Outputs:    []graph.Output{{Port: "c3", Trigger: graph.OnAnswer, Zone: "c3"}},
				Responses:  []graph.Response{{Text: "Nothing on record here."}},
			}},
			{Slug: "found", Type: graph.Story, StoryCfg: &graph.StoryConfig{Blocks: graph.LegacyBlocks("A shell casing.", nil)}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "scan"},
			{From: "scan", FromPort: "c3", To: "found"},
			{From: "found", FromPort: "out", To: "end"},
		},
	}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("a search needs no matchers: %v", r.Errors())
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if in := tm.View().Input; in == nil || in.Kind != "search" || in.At != "" {
		t.Fatalf("a search, and nowhere yet: %+v", in)
	}

	tm.Position = &Position{Lat: 59.340, Lng: 18.070}
	if at := tm.View().Input.At; at != "A1" {
		t.Errorf("the team is reported in A1, got %q", at)
	}
	res, st, err := tm.Answer("scan", "")
	if err != nil || st != nil || res.Response != "Nothing on record here." {
		t.Fatalf("a cell with nothing to find is a wrong guess with its reply: %+v %v", res, err)
	}

	tm.Position = &Position{Lat: 59.342, Lng: 18.072}
	res, st, err = tm.Answer("scan", "anything at all")
	if err != nil || !res.Accepted || res.Port != "c3" || st == nil {
		t.Fatalf("searching in C3 takes its output: %+v %v", res, err)
	}
}

// The accusation: a marked form always moves on, paying its points in
// shares for the questions answered correctly — and never tells the
// phone the answers.
func TestGradedChoices(t *testing.T) {
	opts := func(vs ...string) []graph.ChoiceOption {
		out := make([]graph.ChoiceOption, len(vs))
		for i, v := range vs {
			out[i] = graph.ChoiceOption{Value: v}
		}
		return out
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "accuse", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer: &graph.Answer{Kind: graph.AnswerChoice, Fields: []graph.ChoiceField{
					{Label: "Who?", Options: opts("carl", "erika", "olivia"), Answer: "erika"},
					{Label: "How?", Options: opts("poison", "firearm"), Answer: "firearm"},
				}},
				Outputs: []graph.Output{{Port: "done", Trigger: graph.OnAnswer, Points: 100}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "accuse"}, {From: "accuse", FromPort: "done", To: "end"}},
	}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("a marked form needs no matchers: %v", r.Errors())
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	in := tm.View().Input
	if in.Kind != "choice" || len(in.Fields) != 2 || len(in.Fields[0].Options) != 3 {
		t.Fatalf("the phone gets the questions: %+v", in)
	}
	if b, _ := json.Marshal(in); strings.Contains(string(b), "answer") {
		t.Errorf("the answers must not reach the phone: %s", b)
	}
	if _, st, _ := tm.Answer("accuse", "erika"); st != nil {
		t.Error("a guess missing a question is not marked")
	}
	res, st, err := tm.Answer("accuse", "erika|poison")
	if err != nil || !res.Accepted || res.Points != 50 || st == nil {
		t.Fatalf("one of two right pays half: %+v %v", res, err)
	}
}

// The time limit: once it passes, whatever the team asks for, it is moved
// to the accusation with the message — once. Before it, time-up is refused.
func TestAdventureTimeLimit(t *testing.T) {
	g := mystery()
	g.TimeLimit = &graph.TimeLimit{Seconds: 3 * 3600, To: "accuse", Text: "Time to send in your answer."}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); v.EndsAt == nil || !v.EndsAt.Equal(t0.Add(3*time.Hour)) {
		t.Fatalf("the countdown ends three hours in: %+v", v.EndsAt)
	}
	if _, _, err := tm.TimeUp(); err != ErrNotAllowed {
		t.Errorf("time-up before the deadline is refused, got %v", err)
	}
	res, st, err := tm.Choose("home", "scanner")
	act(t, tm, "scanner", res, st, err)

	tm.Now = t0.Add(3*time.Hour + time.Second)
	res, st, err = tm.Act(func() (Result, *walk.Step, error) { return tm.Continue("scanner") })
	if err != nil || !res.TimeUp || st == nil || st.To != "accuse" {
		t.Fatalf("past the deadline, any action moves the team to the accusation: %+v %+v %v", res, st, err)
	}
	tm.Apply(*st)
	v := tm.View()
	if !v.TimedOut || v.TimeUpText != "Time to send in your answer." || v.EndsAt != nil || tm.Solved("scanner") {
		t.Errorf("the message shows, the countdown stops, the left step keeps no outcome: %+v", v)
	}
	if tm.TimeUpDue() {
		t.Error("it fires once")
	}
	res, st, err = tm.Answer("accuse", "erika")
	act(t, tm, "accuse", res, st, err)
	if !tm.Completed {
		t.Error("the team can still finish after time is up")
	}

	bad := mystery()
	bad.TimeLimit = &graph.TimeLimit{Seconds: 60, To: "start"}
	codes := map[string]bool{}
	for _, i := range valid.Check(bad).Errors() {
		codes[i.Code] = true
	}
	if !codes["time_limit.to"] {
		t.Errorf("a limit back to the start is an error, got %v", codes)
	}
}

// The combination lock: only a way of typing. The chosen symbols join
// into an ordinary answer, and the symbols' pictures come with the step.
func TestCombinationLock(t *testing.T) {
	img := func(id int64) *graph.Media { return &graph.Media{ID: id, Kind: graph.MediaImage} }
	g := &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "facade", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer: &graph.Answer{Kind: graph.AnswerLock, Wheels: 3, Start: []int{2, 0, 1}, Symbols: []graph.LockSymbol{
					{Value: "owl", Image: img(11)}, {Value: "lion", Image: img(12)}, {Value: "fox", Image: img(13)},
				}},
				Outputs:   []graph.Output{{Port: "open", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "lion|owl|fox"}}}},
				Responses: []graph.Response{{Text: "It does not budge."}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "facade"}, {From: "facade", FromPort: "open", To: "end"}},
	}
	if r := valid.Check(g); r.HasErrors() {
		t.Fatalf("valid: %v", r.Errors())
	}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	in := tm.View().Input
	if in.Kind != "lock" || in.Wheels != 3 || len(in.Symbols) != 3 || len(in.Start) != 3 {
		t.Fatalf("the phone gets the lock: %+v", in)
	}
	if ids := tm.VisibleMedia(true); len(ids) != 3 {
		t.Errorf("the symbols' pictures are fetchable at the step: %v", ids)
	}
	if res, st, _ := tm.Answer("facade", "owl|owl|owl"); st != nil || res.Response != "It does not budge." {
		t.Errorf("a wrong combination is a wrong guess: %+v", res)
	}
	if res, st, err := tm.Answer("facade", "lion|owl|fox"); err != nil || !res.Accepted || st == nil {
		t.Fatalf("the right combination opens it: %+v %v", res, err)
	}

	g.Nodes[1].PuzzleCfg.Answer = &graph.Answer{Kind: graph.AnswerLock, Wheels: 9, Start: []int{5}, Symbols: []graph.LockSymbol{{Value: "a|b"}}}
	codes := map[string]bool{}
	for _, i := range valid.Check(g).Errors() {
		codes[i.Code] = true
	}
	for _, c := range []string{"lock.wheels", "lock.symbols", "lock.value.pipe", "lock.start"} {
		if !codes[c] {
			t.Errorf("expected %s, got %v", c, codes)
		}
	}
}

// A translated adventure: the view, the replies and the journal in the
// team's language, the main text wherever a translation is missing, and
// answers accepted in either language.
func TestTranslations(t *testing.T) {
	g := mystery()
	g.Language = "sv"
	g.Journal[0].Section = "Carl"
	for i := range g.Nodes {
		if g.Nodes[i].Slug == "carl-murder-q" {
			g.Nodes[i].PuzzleCfg.Responses = []graph.Response{{Text: "Fel år."}}
			g.Nodes[i].PuzzleCfg.Hints = []graph.Hint{{Text: "Titta på sfären."}}
		}
	}
	g.Translations = map[string]map[string]string{"en": {
		graph.NodeKey("home", "branch:carl:label"):            "Carl (EN)",
		graph.BlockKey("carl-murder-q", "text"):               "Which year?",
		graph.NodeKey("carl-murder-q", "reply:0"):             "Wrong year.",
		graph.NodeKey("carl-murder-q", "hint:0"):              "Look at the sphere.",
		graph.NodeKey("carl-murder-q", "out:solved:answers"):  "nineteen ninety-seven\nninety-seven",
		graph.NodeKey("carl-hub", "title"):                    "Carl, in English",
		graph.JournalKey("carl", "body"):                      "Works at the foundation.",
		graph.NodeKey("carl-murder-q", "out:solved:contains"): "millennium",
		graph.SectionKey("Carl"):                              "Carl (notes)",
		graph.ZoneKey("office"):                               "The office",
	}}
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); len(v.Languages) != 2 || v.Languages[0] != "sv" || v.Lang != "sv" || offered(v)["carl"].Label != "Carl" {
		t.Fatalf("Swedish by default, English on offer: %+v", v)
	}
	if err := tm.SetLanguage("de"); err != ErrNotAllowed {
		t.Errorf("a language without text is refused, got %v", err)
	}
	if err := tm.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	v := tm.View()
	if v.Lang != "en" || offered(v)["carl"].Label != "Carl (EN)" || offered(v)["scanner"].Label != "Scanner" {
		t.Fatalf("translated where there is one, the main text where not: %+v", v.Offer)
	}

	for _, a := range []struct {
		what string
		f    func() (Result, *walk.Step, error)
	}{
		{"carl", func() (Result, *walk.Step, error) { return tm.Choose("home", "carl") }},
	} {
		res, st, err := a.f()
		act(t, tm, a.what, res, st, err)
	}
	if z := tm.View().Zone; z == nil || z.Name != "The office" {
		t.Errorf("zone names are translated: %+v", z)
	}
	tm.Position = &Position{Lat: 59.34, Lng: 18.07}
	for _, s := range []string{"carl-arrive", "carl-intro"} {
		res, st, err := tm.Continue(s)
		act(t, tm, s, res, st, err)
	}
	if j := tm.View().Journal; len(j) != 1 || j[0].Body != "Works at the foundation." || j[0].Section != "Carl (notes)" {
		t.Errorf("journal notes and their section are translated: %+v", j)
	}
	if v := tm.View(); v.Title != "Carl, in English" {
		t.Errorf("titles are translated: %q", v.Title)
	}
	res, st, err := tm.Choose("carl-hub", "murder")
	act(t, tm, "murder", res, st, err)
	if v := tm.View(); v.Text != "Which year?" || v.BackLabel != "Carl, in English" {
		t.Errorf("prompt and back label are translated: %q %q", v.Text, v.BackLabel)
	}
	if res, _, _ := tm.Answer("carl-murder-q", "1996"); res.Response != "Wrong year." {
		t.Errorf("replies are translated: %q", res.Response)
	}
	tm.Now = t0.Add(time.Hour)
	if res, _, _ := tm.RevealHint("carl-murder-q", 0); res.Hint != "Look at the sphere." {
		t.Errorf("hints are translated: %q", res.Hint)
	}
	if res, _, _ := tm.Answer("carl-murder-q", "the millennium bug"); !res.Accepted {
		t.Errorf("a translated contains-answer matches anywhere in the guess: %+v", res)
	}
	if g.Nodes[5].PuzzleCfg != nil && graph.TextOf(g.Nodes[5].PuzzleCfg.Blocks) != "When?" {
		t.Error("translating a view never changes the graph")
	}
}

// An adventure with no translations offers no language switch, but names
// its own language, so the phone speaks it.
func TestUntranslatedHasNoLanguages(t *testing.T) {
	g := mystery()
	g.Language = "en"
	tm, step, err := Start(g, t0)
	if err != nil {
		t.Fatal(err)
	}
	tm.Apply(step)
	if v := tm.View(); v.Languages != nil || v.Lang != "en" {
		t.Errorf("nothing to choose: %+v %q", v.Languages, v.Lang)
	}
	if err := tm.SetLanguage("en"); err != nil {
		t.Errorf("the main language is always allowed: %v", err)
	}
}

// A search tries its placed outputs first: one with no zone is the
// fallback, wherever the author listed it.
func TestSearchFallbackComesLast(t *testing.T) {
	z := graph.Zone{Slug: "c3", Lat: 59.342, Lng: 18.072, RadiusM: 30}
	g := &graph.Graph{
		Zones: []graph.Zone{z},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "scan", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Answer: &graph.Answer{Kind: graph.AnswerSearch},
				Outputs: []graph.Output{
					{Port: "elsewhere", Trigger: graph.OnAnswer},
					{Port: "c3", Trigger: graph.OnAnswer, Zone: "c3"},
				},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "scan"},
			{From: "scan", FromPort: "elsewhere", To: "end"},
			{From: "scan", FromPort: "c3", To: "end"},
		},
	}
	for _, c := range []struct {
		at   Position
		port string
	}{{Position{Lat: 59.342, Lng: 18.072}, "c3"}, {Position{Lat: 59.0, Lng: 18.0}, "elsewhere"}} {
		tm, step, err := Start(g, t0)
		if err != nil {
			t.Fatal(err)
		}
		tm.Apply(step)
		tm.Position = &c.at
		if res, _, err := tm.Answer("scan", ""); err != nil || res.Port != c.port {
			t.Errorf("at %v: want %s, got %+v %v", c.at, c.port, res, err)
		}
	}
}
