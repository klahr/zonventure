package tracks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
)

// at is a bucket at minute m past ten, the m-th ping, at latitude m.
func at(m int) bucket {
	return bucket{Point: Point{Lat: float64(m), ServerTS: fmt.Sprintf("2026-10-21 10:%02d:00", m)}, id: int64(m)}
}

func mk(port string, ms ...int) *line {
	l := &line{}
	if port != "" {
		l.group, l.tag = &fork{Split: "s", Port: port}, play.GroupTag("s", port)
	}
	for _, m := range ms {
		l.pts = append(l.pts, at(m))
	}
	return l
}

// drawn is a piece as "key:lat,lat…" with a "*" for the one marked.
func drawn(ps []piece) []string {
	var out []string
	for _, p := range ps {
		s := ":"
		if p.group != nil {
			s = p.group.Port + ":"
		}
		for i, b := range p.pts {
			if i > 0 {
				s += ","
			}
			s += fmt.Sprint(b.Lat)
		}
		if !p.past {
			s += "*"
		}
		out = append(out, s)
	}
	return out
}

func split(members bool, ports ...string) team {
	t := team{split: "s"}
	for i, p := range ports {
		st := session.Strand{Port: p, Cursor: session.Cursor{Current: "p" + p}}
		if members {
			st.Members = []int64{int64(i + 1)}
		}
		t.strands = append(t.strands, st)
	}
	return t
}

// atSplit is t with every group still at the split.
func atSplit(t team) team {
	for i := range t.strands {
		t.strands[i].Current = t.split
	}
	return t
}

// since is t split up since minute m.
func since(t team, m int) team {
	t.since = at(m).ServerTS
	return t
}

func TestCut(t *testing.T) {
	cases := []struct {
		name  string
		lines []*line
		team  team
		known bool
		want  []string
	}{{
		// The team's walk before the split and after the join are two
		// trails; the one after starts where the groups met (b's last).
		name:  "rejoined",
		lines: []*line{mk("", 0, 1, 5, 6), mk("a", 2, 3), mk("b", 2, 4)},
		team:  team{}, known: true,
		want: []string{":0,1", ":4,5,6*", "a:2,3", "b:2,4"},
	}, {
		// Met again, no ping for the team since: its marker where a
		// group's phone pinged last, not back where it split.
		name:  "met, not yet pinged",
		lines: []*line{mk("", 0, 1), mk("a", 2, 3), mk("b", 2, 4)},
		team:  team{}, known: true,
		want: []string{":0,1", "a:2,3", "b:2,4", ":4*"},
	}, {
		// Split again: a's second round is a trail of its own; b, not yet
		// out this round, has no marker at its last; the team's none.
		name:  "second round",
		lines: []*line{mk("", 0, 1, 5, 6), mk("a", 2, 3, 8, 9), mk("b", 2, 4)},
		team:  split(true, "a", "b"), known: true,
		want: []string{":0,1", ":4,5,6", "a:2,3", "a:8,9*", "b:2,4"},
	}, {
		// Split as the server was updated: no ping says a group yet, so
		// the team's trail is where it is.
		name:  "no group pinged",
		lines: []*line{mk("", 0, 1, 2)},
		team:  split(true, "a", "b"), known: true,
		want: []string{":0,1,2*"},
	}, {
		// Split, a phone dealt into no group still pings for the team:
		// its trail is walked, and not drawn from where a group was.
		name:  "late joiner",
		lines: []*line{mk("", 0, 1, 4, 5, 6), mk("a", 2, 3, 4, 5, 6)},
		team:  split(true, "a", "b"), known: true,
		want: []string{":0,1", ":4,5,6*", "a:2,3,4,5,6*"},
	}, {
		// Split, the team's last ping before a group's first: no one on it.
		name:  "split",
		lines: []*line{mk("", 0, 1), mk("a", 1, 2), mk("b", 2, 3)},
		team:  split(true, "a", "b"), known: true,
		want: []string{":0,1", "a:1,2*", "b:2,3*"},
	}, {
		// Dealt into groups, but none set off from the split: the team's
		// trail is where it is.
		name:  "not set off",
		lines: []*line{mk("", 0, 1, 3), mk("a", 2)},
		team:  atSplit(split(true, "a", "b")), known: true,
		want: []string{":0,1", ":2,3*", "a:2"},
	}, {
		// Split afresh at the split, no group set off since: the team is
		// where it last was, though a group's trail is newer.
		name:  "split afresh, none out",
		lines: []*line{mk("", 0, 1), mk("a", 2, 3)},
		team:  atSplit(split(true, "a", "b")), known: true,
		want: []string{":0,1*", "a:2,3*"},
	}, {
		// Split since minute 2, a phone in no group pings at 4: a's trail
		// is not cut there, and b, quiet since 3, keeps its marker.
		name:  "stray since the split",
		lines: []*line{mk("", 0, 1, 4), mk("a", 2, 3, 5, 6), mk("b", 2, 3)},
		team:  since(split(true, "a", "b"), 2), known: true,
		want: []string{":0,1", ":4*", "a:2,3,5,6*", "b:2,3*"},
	}, {
		// Split again since minute 7: the team's walk between the rounds
		// still parts a's two.
		name:  "stray, second round",
		lines: []*line{mk("", 0, 1, 5, 9), mk("a", 2, 3, 7, 8, 10), mk("b", 2, 4)},
		team:  since(split(true, "a", "b"), 7), known: true,
		want: []string{":0,1", ":4,5", ":9*", "a:2,3", "a:7,8,10*", "b:2,4"},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := drawn(cut(c.lines, c.team, c.known, 10*time.Second))
			if !slices.Equal(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func splitGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "s", Type: graph.Split, SplitCfg: &graph.SplitConfig{Paths: []graph.SplitPath{{Port: "a", Label: "Church"}, {Port: "b", Label: "Well"}}}},
			{Slug: "pa", Type: graph.Story}, {Slug: "pb", Type: graph.Story},
			{Slug: "join", Type: graph.Join},
			{Slug: "end", Type: graph.End, EndCfg: &graph.EndConfig{Outcome: graph.Complete}},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: "out", To: "s"},
			{From: "s", FromPort: "a", To: "pa"}, {From: "s", FromPort: "b", To: "pb"},
			{From: "pa", FromPort: "out", To: "join"}, {From: "pb", FromPort: "out", To: "join"},
			{From: "join", FromPort: "out", To: "end"},
		},
	}
}

// seedTeam makes a team split at s on the published splitGraph, with
// players as named, each opened their link a minute ago; it returns the
// event, the team and the players' slots by name.
func seedTeam(t *testing.T, db *sql.DB, names ...string) (int64, int64, map[string]int64) {
	t.Helper()
	ctx := context.Background()
	var orgID, advID, eventID, teamID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO adventures(slug, title, org_id) VALUES ('demo', 'Demo', $1) RETURNING id`, orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	st, _, err := graphstore.Publish(ctx, db, advID, 0, splitGraph(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token) VALUES ('demo', $1, 'running', 'lt') RETURNING id`, orgID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'T', 'tok') RETURNING id`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_graph_state(team_id, event_id, graph_id, current_node) VALUES ($1, $2, $3, 's')`, teamID, eventID, st.ID); err != nil {
		t.Fatal(err)
	}
	slots := map[string]int64{}
	for _, n := range names {
		var id int64
		if err := db.QueryRow(`INSERT INTO player_slots(team_id, name, token, last_login_at)
			VALUES ($1, $2, 'slot-' || $2, to_char(now() AT TIME ZONE 'UTC' - interval '1 minute', 'YYYY-MM-DD HH24:MI:SS')) RETURNING id`,
			teamID, n).Scan(&id); err != nil {
			t.Fatal(err)
		}
		slots[n] = id
	}
	return eventID, teamID, slots
}

// setStrands stores the team's groups at split s, each since now unless
// it says when.
func setStrands(t *testing.T, db *sql.DB, teamID int64, strands []session.Strand) {
	t.Helper()
	for i := range strands {
		if strands[i].NodeEnteredAt.IsZero() {
			strands[i].NodeEnteredAt = time.Now()
		}
	}
	doc, _ := json.Marshal(strands)
	if _, err := db.Exec(`UPDATE team_graph_state SET split = 's', strands = $1 WHERE team_id = $2`, doc, teamID); err != nil {
		t.Fatal(err)
	}
}

// A ping is the group its phone walks (play.Engine.WalkingGroup): one
// of two it is in, the one it chose, one whose players are all away
// once its own met the others — and none for a team together.
func TestPingGroups(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	_, teamID, p := seedTeam(t, db, "Ann", "Bo", "Cy")
	ann, bo, cy := p["Ann"], p["Bo"], p["Cy"]
	a, b := play.GroupTag("s", "a"), play.GroupTag("s", "b")
	eng := play.New(db)
	tagOf := func(slot int64) string {
		t.Helper()
		split, port, err := eng.WalkingGroup(ctx, teamID, slot)
		if err != nil {
			t.Fatal(err)
		}
		if port == "" {
			return ""
		}
		return play.GroupTag(split, port)
	}
	if got := tagOf(ann); got != "" {
		t.Errorf("a team together: %q", got)
	}

	// Ann in both groups: the first of them.
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "pa"}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb"}, Members: []int64{ann, bo}}})
	if got := tagOf(ann); got != a {
		t.Errorf("Ann in two groups walks %q, want the first", got)
	}
	// Groups that no longer fit the graph — the church party on a step
	// past the join — are split afresh by the team's next action, not
	// walked meanwhile: the ping is the team's.
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "end", NodeEnteredAt: time.Now()}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb", NodeEnteredAt: time.Now()}, Members: []int64{ann, bo}}})
	if got := tagOf(ann); got != "" {
		t.Errorf("Ann in groups that no longer fit walks %q", got)
	}
	// Ann chose the well's.
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "pa"}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb"}, Members: []int64{ann, bo}, Chosen: []int64{ann}}})
	if got := tagOf(ann); got != b {
		t.Errorf("Ann chose the well's group, walks %q", got)
	}
	// Cy, joining late, not yet stored in a group: dealt as the engine
	// will, to the church party with fewer players.
	if got := tagOf(cy); got != a {
		t.Errorf("Cy, not yet dealt, walks %q, want the church party", got)
	}
	// Cy forgotten as he pings: still dealt as the engine deals him
	// (play.Players), not left in no group.
	if _, err := db.Exec(`UPDATE player_slots SET name = '(forgotten)' WHERE id = $1`, cy); err != nil {
		t.Fatal(err)
	}
	if got := tagOf(cy); got != a {
		t.Errorf("Cy, forgotten, walks %q, want the church party", got)
	}

	// Bo's phone silent a while: once Ann's church party is at the
	// join, she walks his.
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "join"}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb"}, Members: []int64{bo}}})
	if got := tagOf(ann); got != a {
		t.Errorf("Bo still here: Ann walks %q", got)
	}
	if _, err := db.Exec(`UPDATE player_slots SET last_login_at = '2020-01-01 00:00:00' WHERE id = $1`, bo); err != nil {
		t.Fatal(err)
	}
	if got := tagOf(ann); got != b {
		t.Errorf("Bo away: Ann walks %q, want his group", got)
	}
}

// Each group's place is its latest ping, whoever pinged — not its own
// players' latest fix.
func TestGroupsAt(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	eventID, teamID, p := seedTeam(t, db, "Ann", "Bo")
	ann, bo := p["Ann"], p["Bo"]
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "pa"}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb"}, Members: []int64{bo}}})
	ping := func(slot int64, lat float64, ts string, tag string) {
		t.Helper()
		var groups []string
		if tag != "" {
			groups = []string{tag}
		}
		if _, err := db.Exec(`INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, server_ts, groups) VALUES ($1, $2, $3, $4, 18, $5, $6)`,
			slot, teamID, eventID, lat, ts, groups); err != nil {
			t.Fatal(err)
		}
	}
	a, b := play.GroupTag("s", "a"), play.GroupTag("s", "b")
	ping(ann, 1, "2026-10-21 10:00:00", a)
	ping(bo, 2, "2026-10-21 10:00:00", b)
	// Ann walks Bo's group for him, then back to her own church party
	// with a fix of her own in no group.
	ping(ann, 3, "2026-10-21 10:01:00", b)
	ping(ann, 4, "2026-10-21 10:02:00", "")
	gs, err := eventGroups(ctx, db, eventID)
	if err != nil {
		t.Fatal(err)
	}
	g := gs[teamID]
	if len(g) != 2 || g[0].At == nil || g[0].At.Lat != 1 || g[1].At == nil || g[1].At.Lat != 3 {
		t.Fatalf("groups %+v", g)
	}

	// Split again at 10:30: no group has a place from the walk before
	// until it pings; then its own.
	split := time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC)
	if _, err := db.Exec(`UPDATE team_graph_state SET node_entered_at = $2 WHERE team_id = $1`, teamID, split); err != nil {
		t.Fatal(err)
	}
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "pa", NodeEnteredAt: split}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb", NodeEnteredAt: split.Add(time.Minute)}, Members: []int64{bo}}})
	gs, _ = eventGroups(ctx, db, eventID)
	if g := gs[teamID]; g[0].At != nil || g[1].At != nil {
		t.Errorf("split again, a place from before: %+v %+v", g[0].At, g[1].At)
	}
	ping(bo, 6, "2026-10-21 10:31:00", b)
	gs, _ = eventGroups(ctx, db, eventID)
	if g := gs[teamID]; g[0].At != nil || g[1].At == nil || g[1].At.Lat != 6 {
		t.Errorf("split again, b pinged: %+v %+v", g[0].At, g[1].At)
	}
	// A group that has moved on since keeps its place from before it
	// did: its phone pinged on its last step, not yet on this one.
	ping(ann, 7, "2026-10-21 10:32:00", a)
	setStrands(t, db, teamID, []session.Strand{{Port: "a", Cursor: session.Cursor{Current: "pa", NodeEnteredAt: split.Add(5 * time.Minute)}, Members: []int64{ann}}, {Port: "b", Cursor: session.Cursor{Current: "pb", NodeEnteredAt: split.Add(10 * time.Minute)}, Members: []int64{bo}}})
	gs, _ = eventGroups(ctx, db, eventID)
	if g := gs[teamID]; g[0].At == nil || g[0].At.Lat != 7 {
		t.Errorf("a, moved on since its last ping, lost its place: %+v", g[0].At)
	}
}

// A dev graph is kept until its draft is saved over it.
func TestGraphDraft(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	_, teamID, _ := seedTeam(t, db)
	var advID, id int64
	if err := db.QueryRow(`SELECT g.adventure_id FROM team_graph_state s JOIN adventure_graphs g ON g.id = s.graph_id WHERE s.team_id = $1`, teamID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	id, err := graphstore.EnsureDev(ctx, db, advID, splitGraph())
	if err != nil {
		t.Fatal(err)
	}
	eng := play.New(db)
	g1, err := eng.Graph(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if g2, _ := eng.Graph(ctx, id); g2 != g1 {
		t.Error("a dev graph not saved over is read again")
	}
	edited := splitGraph()
	edited.Nodes[1].SplitCfg.Paths[0].Label = "Chapel"
	doc, _ := json.Marshal(edited)
	if _, err := db.Exec(`UPDATE adventure_graphs SET graph = $1 WHERE id = $2`, doc, id); err != nil {
		t.Fatal(err)
	}
	g3, err := eng.Graph(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if g3 == g1 || g3.Nodes[1].SplitCfg.Paths[0].Label != "Chapel" {
		t.Error("a dev graph saved over is not read again")
	}
	// A draft stored without a language takes its adventure's: changing
	// that is read again too.
	edited.Language = ""
	doc, _ = json.Marshal(edited)
	if _, err := db.Exec(`UPDATE adventure_graphs SET graph = $1 WHERE id = $2`, doc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE adventures SET language = 'sv' WHERE id = $1`, advID); err != nil {
		t.Fatal(err)
	}
	if g4, err := eng.Graph(ctx, id); err != nil || g4.Language != "sv" {
		t.Errorf("the adventure's language changed, the draft reads %q (%v)", g4.Language, err)
	}
	if _, err := db.Exec(`UPDATE adventures SET language = 'en' WHERE id = $1`, advID); err != nil {
		t.Fatal(err)
	}
	if g5, _ := eng.Graph(ctx, id); g5.Language != "en" {
		t.Errorf("the adventure's language changed back, the draft reads %q", g5.Language)
	}
}

// Two buckets of one time on a line are one centroid, each weighed as
// the SQL weighed its pings: the precise fix counts for more.
func TestMergeWeighs(t *testing.T) {
	l := &line{pts: []bucket{
		{Point: Point{Lat: 10, ServerTS: "2026-10-21 10:00:00"}, id: 1, weight: 1},
		{Point: Point{Lat: 20, ServerTS: "2026-10-21 10:00:00"}, id: 2, weight: 3},
	}}
	l.merge()
	if len(l.pts) != 1 || l.pts[0].Lat != 17.5 || l.pts[0].id != 2 || l.pts[0].weight != 4 {
		t.Fatalf("merged %+v", l.pts)
	}
}

// eventGroups are the groups of the event's split teams, as the console
// reads them (Groups, from the teams as stored).
func eventGroups(ctx context.Context, db *sql.DB, eventID int64) (map[int64][]Group, error) {
	ts, err := teams(ctx, db, eventID)
	if err != nil {
		return nil, err
	}
	return groups(ctx, play.New(db), ts)
}
