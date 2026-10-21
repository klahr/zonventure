// Package tracks draws an event's teams on a map from their phones'
// pings: a trail per team, and per group of a team split up (a split
// step), for race control and the live board alike.
//
// Each ping remembers the group its phone was walking as it was taken
// (location_pings.groups, PingGroups), so a group's trail is the walk of
// whoever walked it while it was out — its own players, or others for
// players gone — nothing from before the split, and stays apart after
// the groups meet again. A ping in no group is the team's.
package tracks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/icscal"
	"github.com/klahr/zonventure/internal/play"
)

type Point struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	ServerTS string  `json:"server_ts"`
}

type Track struct {
	TeamID    int64   `json:"team_id"`
	TeamName  string  `json:"team_name"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Points    []Point `json:"points"`
	// Group and GroupLabel are set on the trail of one group of a team
	// split up: its key ("a", "a>b" for a group of a group) and its name.
	Group      string `json:"group,omitempty"`
	GroupLabel string `json:"group_label,omitempty"`
	// GroupLabels are the names GroupLabel is joined from, one for each
	// split the group came down, outermost first.
	GroupLabels []string `json:"group_labels,omitempty"`
	// Past says no one is on the trail now: a group that has met the
	// others again, the team's own while it is split up, or a stretch of
	// a trail left and taken up again later — a team or a group has a
	// trail per stretch, the newest last, and only it may be without
	// Past. Draw its line, but no marker at its end.
	Past bool `json:"past,omitempty"`
}

// Group is one group of a team split up now: its key, its name, its
// step and the players in it, and At its latest ping — whoever walked
// it — if any was taken in it.
type Group struct {
	Key, Label, Node string
	Labels           []string
	Members          []int64
	At               *Point
}

// Labels names each fork a group came down by itself, outermost first:
// GroupLabel's parts, apart, so a name with " › " in it stays one.
func Labels(g *graph.Graph, forks []session.Fork) []string {
	out := make([]string, len(forks))
	for i, f := range forks {
		out[i] = session.GroupLabel(g, "", []session.Fork{f})
	}
	return out
}

// Tag is how a ping names a group it was walking (location_pings.groups):
// the forks it came down, outermost first, as JSON — [{"split":…,"port":…}]
// — which no slug or port can make read as another group's, so a trail
// can be named after its group has gone. Pings stored before named them
// "split.port" joined by ">", which forks still reads.
func Tag(forks []session.Fork) string {
	b, err := json.Marshal(forks)
	if err != nil {
		return ""
	}
	return string(b)
}

// oldTag is a group's name as pings stored before Tag named it.
func oldTag(forks []session.Fork) string {
	parts := make([]string, len(forks))
	for i, f := range forks {
		parts[i] = f.Split + "." + f.Port
	}
	return strings.Join(parts, ">")
}

// forks reads a ping's group back: the forks, and the group's key.
func forks(tag string) ([]session.Fork, string) {
	var out []session.Fork
	if strings.HasPrefix(tag, "[") {
		if json.Unmarshal([]byte(tag), &out) != nil {
			return nil, ""
		}
	} else {
		// "split.port>split.port", as pings were tagged before Tag.
		for _, p := range strings.Split(tag, ">") {
			i := strings.LastIndex(p, ".")
			if i < 0 {
				continue
			}
			out = append(out, session.Fork{Split: p[:i], Port: p[i+1:]})
		}
	}
	ports := make([]string, len(out))
	for i, f := range out {
		ports[i] = f.Port
	}
	return out, strings.Join(ports, ">")
}

// awayAfter is how long a phone may stay silent before its player counts
// as away, as the engine counts it: a group whose players all are is
// anyone's to walk.
const awayAfter = play.AwayAfter

// PingGroups are the groups the phone slot of the team teamID walks as it
// pings, as Tags, for the ping's groups column: the one it chose, while
// that is on its way, or else its first own group not yet at the join,
// or one no one playing is left in — as the engine has it
// (session.Team.Walkers). A phone joining mid-split, not yet stored in a
// group, is dealt into the one the engine deals it on the team's next
// view or action (session.Team.Deal, as play deals it), so its pings are
// that group's from the first. Nil for a team together, or at its split
// with no one dealt yet — not set off: its pings are the team's. One
// query, for a team split up only, and the graph from the cache — and
// the roles read where someone is still to be dealt.
func PingGroups(ctx context.Context, db *sql.DB, teamID, slot int64, now time.Time) ([]string, error) {
	var (
		graphID    int64
		split      string
		rawStrands []byte
		rawPlayers []byte
	)
	// The players as the engine reads them (play.Players), each with when
	// its phone was last heard from — a ping, or a request of the player
	// app — or else its player opened their link (play.away).
	err := db.QueryRowContext(ctx, `
		SELECT gs.graph_id, gs.split, gs.strands, COALESCE((
		  SELECT jsonb_agg(jsonb_build_object('slot', s.id, 'seen',
		           COALESCE(GREATEST((SELECT max(p.server_ts) FROM location_pings p WHERE p.slot_id = s.id), s.last_seen_at),
		                    s.last_login_at, ''))
		         ORDER BY s.id)
		  FROM player_slots s
		  WHERE s.team_id = gs.team_id AND (s.last_login_at IS NOT NULL OR s.id = $2) AND s.name <> '(forgotten)'), '[]')
		FROM team_graph_state gs
		WHERE gs.team_id = $1 AND gs.split IS NOT NULL AND jsonb_typeof(gs.strands) = 'array'`,
		teamID, slot).Scan(&graphID, &split, &rawStrands, &rawPlayers)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var strands []session.Strand
	if err := json.Unmarshal(rawStrands, &strands); err != nil {
		return nil, err
	}
	in := func(slot int64) bool {
		return slices.ContainsFunc(strands, func(st session.Strand) bool { return slices.Contains(st.Members, slot) })
	}
	if !slices.ContainsFunc(strands, func(st session.Strand) bool { return len(st.Members) > 0 }) {
		// No one is dealt until the team sets off: its pings are its own.
		return nil, nil
	}
	type heard struct {
		Slot int64  `json:"slot"`
		Seen string `json:"seen"`
	}
	var seen []heard
	if err := json.Unmarshal(rawPlayers, &seen); err != nil {
		return nil, err
	}
	// As play.Players: the phone pinging plays even where the query left
	// it out — forgotten as it pinged — last.
	if slot != 0 && !slices.ContainsFunc(seen, func(p heard) bool { return p.Slot == slot }) {
		seen = append(seen, heard{Slot: slot})
	}
	g, err := Graph(ctx, db, graphID)
	if err != nil {
		return nil, err
	}
	t := &session.Team{G: g, Split: split, Strands: strands, Away: map[int64]bool{}}
	players := make([]int64, 0, len(seen))
	undealt := false
	for _, p := range seen {
		players = append(players, p.Slot)
		// The phone pinging is not away.
		if at, ok := icscal.ParseTimestamp(p.Seen); p.Slot != slot && (!ok || now.Sub(at) > awayAfter) {
			t.Away[p.Slot] = true
		}
		undealt = undealt || !in(p.Slot)
	}
	if undealt {
		// Dealt as the engine deals anyone playing in no group yet, with
		// the roles as it reads them; stored by the team's next action.
		var holders map[string]int64
		if roles := g.DealtRoles(); len(roles) > 0 {
			dealt, err := play.DealRoles(ctx, db, teamID, slot, roles)
			if err != nil {
				return nil, err
			}
			holders = make(map[string]int64, len(dealt))
			for role, h := range dealt {
				holders[role] = h.Slot
			}
		}
		t.Deal(players, holders)
	}
	walkers := t.Walkers(players)
	for _, st := range strands {
		if slices.Contains(walkers[st.Key()], slot) {
			return []string{Tag(st.Lineage(split))}, nil
		}
	}
	return nil, nil
}

// graphs keeps the graphs groups are named from, by id, as every ping
// and push asks again: a release or a party's own copy never changes; a
// dev row (version 0) follows the draft, saved over in place, so it is
// kept with its row's version (xmin) and read again once that moves.
var graphs = struct {
	mu   sync.Mutex
	m    map[int64]*cachedGraph
	tick uint64
}{m: map[int64]*cachedGraph{}}

type cachedGraph struct {
	g *graph.Graph
	// mark is a dev row's version as it was read; "" for one that never
	// changes.
	mark string
	used uint64
}

// maxGraphs bounds the cache; past it, the graph used longest ago goes.
const maxGraphs = 64

// Graph is the stored graph id, from the cache while it is as stored.
func Graph(ctx context.Context, db *sql.DB, id int64) (*graph.Graph, error) {
	graphs.mu.Lock()
	c := graphs.m[id]
	if c != nil && c.mark == "" {
		graphs.tick++
		c.used = graphs.tick
		graphs.mu.Unlock()
		return c.g, nil
	}
	graphs.mu.Unlock()
	// A dev row, or a graph not read yet: its version first, so a draft
	// saved while it is read is read again next time.
	var (
		version int
		mark    string
	)
	if err := db.QueryRowContext(ctx, `SELECT version, xmin::text FROM adventure_graphs WHERE id = $1`, id).Scan(&version, &mark); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, graphstore.ErrNotFound
		}
		return nil, err
	}
	if version != 0 {
		mark = ""
	}
	if c != nil && c.mark == mark {
		graphs.mu.Lock()
		graphs.tick++
		c.used = graphs.tick
		graphs.mu.Unlock()
		return c.g, nil
	}
	st, err := graphstore.ByID(ctx, db, id)
	if err != nil {
		return nil, err
	}
	graphs.mu.Lock()
	defer graphs.mu.Unlock()
	if _, ok := graphs.m[id]; !ok && len(graphs.m) >= maxGraphs {
		oldest, least := int64(0), uint64(0)
		for k, e := range graphs.m {
			if least == 0 || e.used < least {
				oldest, least = k, e.used
			}
		}
		delete(graphs.m, oldest)
	}
	graphs.tick++
	graphs.m[id] = &cachedGraph{g: st.Graph, mark: mark, used: graphs.tick}
	return st.Graph, nil
}

// team is a team's graph state, as tracks read it.
type team struct {
	graphID int64
	split   string
	strands []session.Strand
	// since bounds the pings of the team's groups this time out, as
	// server_ts: none of them is older than the step any group is on
	// now was entered. It is the split's start while any group is on its
	// first step, and never older: a group's pings of an earlier walk of
	// the split, or the team's before it, are older. "" where not known.
	since string
}

// sinceOf is team.since for strands: the earliest a group entered its
// step, in server_ts's form.
func sinceOf(strands []session.Strand) string {
	var at time.Time
	for _, st := range strands {
		if st.EnteredAt.IsZero() {
			return ""
		}
		if at.IsZero() || st.EnteredAt.Before(at) {
			at = st.EnteredAt
		}
	}
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.DateTime)
}

func teams(ctx context.Context, db *sql.DB, eventID int64) (map[int64]team, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT team_id, graph_id, COALESCE(split, ''), strands FROM team_graph_state WHERE event_id = $1`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]team{}
	for rows.Next() {
		var id int64
		var t team
		var raw []byte
		if err := rows.Scan(&id, &t.graphID, &t.split, &raw); err != nil {
			return nil, err
		}
		if t.split != "" && len(raw) > 0 && json.Unmarshal(raw, &t.strands) != nil {
			t.strands = nil
		}
		t.since = sinceOf(t.strands)
		out[id] = t
	}
	return out, rows.Err()
}

// Groups are the groups of each of the event's teams that is split up
// now, by team.
func Groups(ctx context.Context, db *sql.DB, eventID int64) (map[int64][]Group, error) {
	ts, err := teams(ctx, db, eventID)
	if err != nil {
		return nil, err
	}
	return groups(ctx, db, ts)
}

func groups(ctx context.Context, db *sql.DB, ts map[int64]team) (map[int64][]Group, error) {
	out := map[int64][]Group{}
	var (
		teamIDs          []int64
		tags, old, since []string
	)
	for id, t := range ts {
		if t.split == "" || len(t.strands) == 0 {
			continue
		}
		g, err := Graph(ctx, db, t.graphID)
		if err != nil {
			return nil, err
		}
		for _, st := range t.strands {
			lin := st.Lineage(t.split)
			out[id] = append(out[id], Group{Key: st.Key(), Label: session.GroupLabel(g, "", lin), Labels: Labels(g, lin), Node: st.Current, Members: st.Members})
			teamIDs, tags, old = append(teamIDs, id), append(tags, Tag(lin)), append(old, oldTag(lin))
			since = append(since, t.since)
		}
	}
	if len(tags) == 0 {
		return out, nil
	}
	// Each group's latest ping by its tag — or the tag before Tag, for a
	// group out as the server was updated — this time out (team.since),
	// read off idx_pings_team_ts newest first: a group no one has pinged
	// in yet reads the team's pings since the split, not its whole walk,
	// and has no place from an earlier walk of the split.
	rows, err := db.QueryContext(ctx, `
		SELECT q.team, q.tag, p.lat, p.lng, p.server_ts
		FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[]) AS q(team, tag, old, since)
		JOIN LATERAL (
			SELECT lat, lng, server_ts FROM location_pings
			WHERE team_id = q.team AND server_ts >= q.since AND groups && ARRAY[q.tag, q.old]
			ORDER BY server_ts DESC
			LIMIT 1
		) p ON true`, teamIDs, tags, old, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id  int64
			tag string
			p   Point
		)
		if err := rows.Scan(&id, &tag, &p.Lat, &p.Lng, &p.ServerTS); err != nil {
			return nil, err
		}
		gs := out[id]
		for i, st := range ts[id].strands {
			if Tag(st.Lineage(ts[id].split)) == tag {
				gs[i].At = &p
			}
		}
	}
	return out, rows.Err()
}

// Query builds the event's trails: per team, its own from the pings in
// no group, then one per group its pings were in — each cut in two where
// it was left and taken up again (cut), so the team's walk before a split
// and after the join, or a group's in one round and the next, are two
// trails rather than one line across the map.
func Query(ctx context.Context, db *sql.DB, eventID int64) ([]Track, error) {
	ts, err := teams(ctx, db, eventID)
	if err != nil {
		return nil, err
	}
	// Smooth each line by collapsing pings into time buckets and emitting
	// one centroid per bucket. Buckets are 10s wide, widened in 10s steps
	// once the event's ping history spans more than 20 minutes so a track
	// never exceeds ~120 points: this is resent on every push, and full
	// 10s resolution over a multi-hour event made it megabytes. server_ts
	// is a UTC 'YYYY-MM-DD HH24:MI:SS' string from now_text(), and the
	// bucket start goes back out in that format. The centroid is an
	// inverse-variance weighted mean: pings with smaller `accuracy` (in
	// metres) dominate, with a 1m floor to bound the weight and a 50m
	// fallback when accuracy is null. The newest ping's id orders buckets
	// of one time. A ping names one group (PingGroups); one stored naming
	// more counts for the first. Read as groups[1], not unnested, the
	// planner sizes the aggregate right; the span reads idx_pings_run_ts'
	// two ends, and the teams are joined once bucketed — hours of pings
	// stay a scan and a hash aggregate, well under JIT's thresholds.
	rows, err := db.QueryContext(ctx, trailsSQL, eventID)
	if err != nil {
		return nil, err
	}
	type teamLines struct {
		id     int64
		name   string
		avatar bool
		lines  []*line
		byTag  map[string]*line
	}
	var (
		all   []*teamLines
		width float64
	)
	for rows.Next() {
		var (
			id        int64
			name, lin string
			hasAvatar int
			b         bucket
		)
		if err := rows.Scan(&id, &name, &hasAvatar, &lin, &b.Lat, &b.Lng, &b.ServerTS, &b.id, &width); err != nil {
			rows.Close()
			return nil, err
		}
		if len(all) == 0 || all[len(all)-1].id != id {
			all = append(all, &teamLines{id: id, name: name, avatar: hasAvatar != 0, byTag: map[string]*line{}})
		}
		tl := all[len(all)-1]
		// A group's pings under the tag before Tag and under Tag are one
		// line; a tag not read is the team's.
		var l line
		if lin != "" {
			l.forks, l.key = forks(lin)
		}
		if len(l.forks) > 0 {
			l.tag = Tag(l.forks)
		} else {
			l.forks, l.key = nil, ""
		}
		at := tl.byTag[l.tag]
		if at == nil {
			at = &l
			tl.byTag[l.tag] = at
			tl.lines = append(tl.lines, at)
		}
		at.pts = append(at.pts, b)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var out []Track
	for _, tl := range all {
		t, known := ts[tl.id]
		var g *graph.Graph
		if known {
			g, _ = Graph(ctx, db, t.graphID)
		}
		for _, l := range tl.lines {
			l.merge()
		}
		for _, pc := range cut(tl.lines, t, known, time.Duration(width)*time.Second) {
			tr := Track{TeamID: tl.id, TeamName: tl.name, Points: make([]Point, len(pc.pts)), Past: pc.past}
			if tl.avatar {
				tr.AvatarURL = fmt.Sprintf("/adventure-assets/team-avatar/%d", tl.id)
			}
			for i, b := range pc.pts {
				tr.Points[i] = b.Point
			}
			if len(pc.forks) > 0 {
				tr.Group, tr.GroupLabel = pc.key, pc.key
				if g != nil {
					tr.GroupLabel, tr.GroupLabels = session.GroupLabel(g, "", pc.forks), Labels(g, pc.forks)
				}
			}
			out = append(out, tr)
		}
	}
	if out == nil {
		out = []Track{}
	}
	return out, nil
}

const trailsSQL = `
		WITH span AS (
		  SELECT GREATEST(10, CEIL(EXTRACT(EPOCH FROM
		           (SELECT MAX(server_ts) FROM location_pings WHERE event_id = $1)::timestamp -
		           (SELECT MIN(server_ts) FROM location_pings WHERE event_id = $1)::timestamp) / 1200) * 10)::float8 AS w
		), pts AS (
		  SELECT p.team_id, COALESCE(p.groups[1], '') AS lin, p.lat, p.lng, p.id,
		         FLOOR(date_part('epoch', p.server_ts::timestamp) / s.w) AS k,
		         1 / power(GREATEST(COALESCE(p.accuracy, 50), 1), 2) AS wt
		  FROM location_pings p CROSS JOIN span s
		  WHERE p.event_id = $1
		), buckets AS (
		  SELECT team_id, lin, k, SUM(lat * wt) / SUM(wt) AS lat, SUM(lng * wt) / SUM(wt) AS lng, MAX(id) AS id
		  FROM pts
		  GROUP BY team_id, lin, k
		)
		SELECT b.team_id, t.name, (t.avatar_mime_type IS NOT NULL)::int AS has_avatar, b.lin, b.lat, b.lng,
		       to_char(to_timestamp(b.k * s.w) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS') AS server_ts,
		       b.id, s.w
		FROM buckets b JOIN teams t ON t.id = b.team_id CROSS JOIN span s
		ORDER BY b.team_id, b.lin, b.k
	`

// bucket is a trail's point: its pings' centroid over one time bucket,
// and the newest one's id.
type bucket struct {
	Point
	id int64
}

// newer reports whether a was walked after b: a later bucket, or of one
// time, a later ping.
func (a bucket) newer(b bucket) bool {
	return a.ServerTS > b.ServerTS || a.ServerTS == b.ServerTS && a.id > b.id
}

// line is all of a team's pings in one group, by time — or in none, the
// team's own, its forks nil.
type line struct {
	forks []session.Fork
	key   string
	tag   string
	pts   []bucket
}

// merge orders the line's points by time, one per bucket: two tags of
// one group give it two of a time.
func (l *line) merge() {
	slices.SortStableFunc(l.pts, func(a, b bucket) int { return strings.Compare(a.ServerTS, b.ServerTS) })
	out := l.pts[:0]
	for _, b := range l.pts {
		if n := len(out); n > 0 && out[n-1].ServerTS == b.ServerTS {
			p := &out[n-1]
			p.Lat, p.Lng, p.id = (p.Lat+b.Lat)/2, (p.Lng+b.Lng)/2, max(p.id, b.id)
			continue
		}
		out = append(out, b)
	}
	l.pts = out
}

func (l *line) last() bucket { return l.pts[len(l.pts)-1] }

// under reports whether the group that came down a holds the one that
// came down b: the team, every group; a group, the groups it split into.
func under(a, b []session.Fork) bool {
	return len(a) < len(b) && slices.Equal(a, b[:len(a)])
}

// piece is one trail drawn: a stretch of a line.
type piece struct {
	*line
	pts  []bucket
	past bool
}

// cut draws a team's lines as trails. A line is cut where its walk was
// left for another's and taken up again: where a point of a line holding
// it or held by it — the team's, before a split or after the join, and a
// group's — falls between two of its own. The team's line taken up after
// a join starts where the groups were last; a group's taken up again,
// for a second round, is another trail.
//
// A team split up has its own pings this time out (team.since) from a
// phone in no group — one joining late, as the server was updated, or
// one whose group could not be read: they are not the team taken up
// again, so they cut no group's trail, nor make a group's walk one of a
// round before.
//
// Only the newest trail of a line anyone is on now has no past: a group
// out now whose trail is of this round (fresh); the team's while it is
// together — ending at its newest ping, a group's at the join until a
// phone pings for the team — and, split up, while no one is dealt into a
// group, no group has walked since the split, or a phone in none (one
// joining late) still pings for it. A team not known keeps its every line.
func cut(lines []*line, t team, known bool, width time.Duration) []piece {
	// stray: a point of the team's own taken while it is split up now, by
	// a phone in no group.
	stray := func(l *line, p bucket) bool {
		return known && l.forks == nil && t.split != "" && t.since != "" && p.ServerTS >= t.since
	}
	segs := map[*line][][]bucket{}
	for _, l := range lines {
		var others []string
		for _, o := range lines {
			if under(l.forks, o.forks) || under(o.forks, l.forks) {
				for _, p := range o.pts {
					if !stray(o, p) {
						others = append(others, p.ServerTS)
					}
				}
			}
		}
		slices.Sort(others)
		var cur []bucket
		for i, p := range l.pts {
			if i > 0 && between(others, l.pts[i-1].ServerTS, p.ServerTS) {
				segs[l] = append(segs[l], cur)
				cur = nil
			}
			cur = append(cur, p)
		}
		segs[l] = append(segs[l], cur)
	}
	byTag := map[string]*line{}
	for _, l := range lines {
		byTag[l.tag] = l
	}
	// begun is where the newest trail of l starts — of the team's, the
	// newest not of strays; "" if it has none.
	begun := func(l *line) string {
		s := segs[l]
		for k := len(s) - 1; k >= 0; k-- {
			if !stray(l, s[k][0]) {
				return s[k][0].ServerTS
			}
		}
		return ""
	}
	// fresh: l's newest trail was walked since every line holding it was
	// taken up last — this round's, not one before.
	fresh := func(l *line) bool {
		for _, a := range lines {
			if under(a.forks, l.forks) && l.last().ServerTS < begun(a) {
				return false
			}
		}
		return true
	}

	live := map[*line]bool{}
	// together are the lines a whole is on now that its groups may have
	// met at: the team's, together; a group of groups met again.
	var together []string
	switch {
	case !known:
		for _, l := range lines {
			live[l] = true
		}
	case t.split == "":
		live[byTag[""]] = true
		together = append(together, "")
	default:
		dealt := false
		var out []*line
		for _, st := range t.strands {
			dealt = dealt || len(st.Members) > 0
			tag := Tag(st.Lineage(t.split))
			together = append(together, tag)
			if l := byTag[tag]; l != nil && fresh(l) {
				live[l] = true
				out = append(out, l)
			}
		}
		if own := byTag[""]; own != nil {
			live[own] = !dealt || len(out) == 0 || stillWalked(own, out, begun, width)
		}
	}
	delete(live, nil)

	// Joins: a line taken up after the lines it holds have met starts
	// where they were last.
	for _, l := range lines {
		s := segs[l]
		for k := range s {
			lower := ""
			if k > 0 {
				lower = s[k-1][len(s[k-1])-1].ServerTS
			}
			if stray(l, s[k][0]) {
				continue // the groups are out still
			}
			from, to := s[k][0].ServerTS, s[k][len(s[k])-1].ServerTS
			var at *bucket
			met := true
			for _, o := range lines {
				if !under(l.forks, o.forks) {
					continue
				}
				in := false
				for i, p := range o.pts {
					if p.ServerTS > lower && s[k][0].newer(p) {
						in = true
						if at == nil || p.newer(*at) {
							at = &o.pts[i]
						}
					}
				}
				if in && slices.ContainsFunc(o.pts, func(p bucket) bool { return p.ServerTS > from && p.ServerTS < to }) {
					met = false // still out: a phone in no group, not a join
				}
			}
			if at != nil && met {
				s[k] = append([]bucket{*at}, s[k]...)
			}
		}
	}

	var out []piece
	for _, l := range lines {
		for k, s := range segs[l] {
			out = append(out, piece{line: l, pts: s, past: !live[l] || k < len(segs[l])-1})
		}
	}
	// Met again: the whole is where any of its groups' phones pinged
	// last, until one of its own pings.
	for _, tag := range together {
		l := byTag[tag]
		if l != nil && !live[l] {
			continue
		}
		var fs []session.Fork
		key := ""
		if l != nil {
			fs, key = l.forks, l.key
		} else if tag != "" {
			fs, key = forks(tag)
		}
		var at *bucket
		for _, o := range lines {
			if under(fs, o.forks) && (at == nil || o.last().newer(*at)) {
				p := o.last()
				at = &p
			}
		}
		if at == nil || l != nil && !at.newer(l.last()) {
			continue
		}
		if slices.ContainsFunc(lines, func(a *line) bool { return under(a.forks, fs) && at.ServerTS < begun(a) }) {
			continue // a round before
		}
		for i := range out {
			if out[i].line == l {
				out[i].past = true
			}
		}
		out = append(out, piece{line: &line{forks: fs, key: key, tag: tag}, pts: []bucket{*at}})
	}
	return out
}

// between reports whether a sorted list has a time after a, before b.
func between(sorted []string, a, b string) bool {
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i] > a })
	return i < len(sorted) && sorted[i] < b
}

// stillWalked reports whether the team's own line is walked while its
// groups are out: a phone in no group — one joining late — pinged after
// the first group set off, and lately, as its groups have.
func stillWalked(own *line, groups []*line, begun func(*line) string, width time.Duration) bool {
	first, newest := begun(groups[0]), groups[0].last().ServerTS
	for _, l := range groups {
		first = min(first, begun(l))
		newest = max(newest, l.last().ServerTS)
	}
	last := own.last().ServerTS
	if last <= first {
		return false
	}
	a, ok1 := icscal.ParseTimestamp(last)
	b, ok2 := icscal.ParseTimestamp(newest)
	return ok1 && ok2 && b.Sub(a) <= max(awayAfter, 2*width)
}
