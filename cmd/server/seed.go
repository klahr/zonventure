package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/play"
)

// demo adventure data — previously in adventures/demo/*.toml, now seeded
// into the database as a graph. Lat/Lng are Stockholm landmarks so the
// map looks alive when the demo tracks run through them.
type demoPage struct {
	Slug     string
	Title    string
	Prompt   string
	Hints    []demoHint
	Accept   []string
	Lat, Lng float64
	// Radius > 0 gates the puzzle on a zone around Lat/Lng.
	Radius float64
}

// demoHint is a short text revealed delay seconds after the team
// reached the puzzle.
type demoHint struct {
	Text     string
	DelaySec int
}

func demoPages() []demoPage {
	return []demoPage{
		{
			Slug:   "welcome",
			Title:  "Welcome",
			Prompt: "What word do you say when you meet someone for the first time?",
			Accept: []string{"hello", "hi"},
			Lat:    59.3308, Lng: 18.0731, // Kungsträdgården
		},
		{
			Slug:   "landmark",
			Title:  "Landmark",
			Prompt: "Find the fountain in the central park. What colour are its tiles?",
			Hints: []demoHint{
				{Text: "Look at the inside of the basin, not the rim.", DelaySec: 60},
			},
			Accept: []string{"blue", "azure"},
			Lat:    59.3326, Lng: 18.0649, // Sergels torg
			Radius: 50,
		},
		{
			Slug:   "finish",
			Title:  "The Finish",
			Prompt: `Name the Swedish word for "adventure".`,
			Accept: []string{"äventyr", "aventyr"},
			Lat:    59.3252, Lng: 18.0708, // Stortorget, Gamla stan
		},
	}
}

// demoGraph chains the demo pages from start to end, each showing the
// picture imageID.
func demoGraph(pages []demoPage, imageID int64) *graph.Graph {
	g := &graph.Graph{}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "start", Type: graph.Start, Title: "Start"})
	prev, prevPort := "start", graph.PortOut
	for i, p := range pages {
		zone := ""
		if p.Radius > 0 {
			zone = "zone-" + p.Slug
			g.Zones = append(g.Zones, graph.Zone{Slug: zone, Name: p.Title, Lat: p.Lat, Lng: p.Lng, RadiusM: p.Radius})
		}
		matchers := make([]match.Matcher, len(p.Accept))
		for j, a := range p.Accept {
			matchers[j] = match.Matcher{Kind: match.Exact, Value: a}
		}
		hints := make([]graph.Hint, len(p.Hints))
		for j, h := range p.Hints {
			hints[j] = graph.Hint{Text: h.Text, DelaySeconds: h.DelaySec}
		}
		g.Nodes = append(g.Nodes, graph.Node{
			Slug: p.Slug, Type: graph.Puzzle, Title: p.Title, Y: float64(i+1) * 140,
			PuzzleCfg: &graph.PuzzleConfig{
				Blocks: graph.LegacyBlocks(p.Prompt, []graph.Media{{ID: imageID, Kind: graph.MediaImage}}),
				Zone:   zone,
				Hints:  hints,
				Outputs: []graph.Output{{
					Port: "solved", Label: "Solved", Trigger: graph.OnAnswer, Matchers: matchers,
				}},
			},
		})
		g.Edges = append(g.Edges, graph.Edge{From: prev, FromPort: prevPort, To: p.Slug})
		prev, prevPort = p.Slug, "solved"
	}
	g.Nodes = append(g.Nodes, graph.Node{Slug: "end", Type: graph.End, Title: "End", Y: float64(len(pages)+1) * 140})
	g.Edges = append(g.Edges, graph.Edge{From: prev, FromPort: prevPort, To: "end"})
	return g
}

// publishDemoGraph stores the placeholder picture as the adventure's
// media and publishes the demo graph.
func publishDemoGraph(ctx context.Context, d *sql.DB, advID int64) error {
	sum := sha256.Sum256([]byte(placeholderImage))
	var imageID int64
	if err := d.QueryRowContext(ctx, `
		INSERT INTO adventure_media(adventure_id, kind, mime_type, size_bytes, sha256, content)
		VALUES ($1, 'image', 'image/svg+xml', $2, $3, $4)
		ON CONFLICT (adventure_id, sha256) DO UPDATE SET kind = excluded.kind
		RETURNING id
	`, advID, len(placeholderImage), hex.EncodeToString(sum[:]), []byte(placeholderImage)).Scan(&imageID); err != nil {
		return err
	}
	_, _, err := graphstore.Publish(ctx, d, advID, 0, demoGraph(demoPages(), imageID), "Demo")
	return err
}

// demoLocator places each demo team wherever the seed says it is, so
// the engine lets it answer the gated puzzle.
type demoLocator map[int64][2]float64

func (l demoLocator) Position(_ context.Context, _ play.Querier, teamID int64) (float64, float64, bool, error) {
	p, ok := l[teamID]
	return p[0], p[1], ok, nil
}

// seedArrangerUser upserts an org_admin ("arranger") from a "username:password"
// flag value, creating the default "Arrangers" organization on first run.
// Used by `make seed` to bootstrap a working login next to the global admin.
func seedArrangerUser(d *sql.DB, creds string) error {
	username, password, err := splitCreds(creds)
	if err != nil {
		return err
	}
	const orgName = "Arrangers"
	if _, err := d.Exec(
		`INSERT INTO organizations(name) VALUES ($1) ON CONFLICT(name) DO NOTHING`,
		orgName); err != nil {
		return err
	}
	var orgID int64
	if err := d.QueryRow(
		`SELECT id FROM organizations WHERE name = $1`, orgName).Scan(&orgID); err != nil {
		return err
	}
	_, err = upsertUser(d, username, password, auth.RoleOrgAdmin, &orgID)
	return err
}

// seedPublisherUser upserts an org_admin in the catalog publisher org
// (creating that org on first run) — the login that authors the
// platform catalog, which the global admin no longer can.
func seedPublisherUser(d *sql.DB, creds string) error {
	username, password, err := splitCreds(creds)
	if err != nil {
		return err
	}
	orgID, err := ensurePublisherOrg(d)
	if err != nil {
		return err
	}
	_, err = upsertUser(d, username, password, auth.RoleOrgAdmin, &orgID)
	return err
}

// ensurePublisherOrg returns the catalog publisher org's id, creating
// "Zonventure Catalog" (the name migration 0065 uses) when no org
// publishes yet.
func ensurePublisherOrg(d *sql.DB) (int64, error) {
	var id int64
	err := d.QueryRow(`SELECT id FROM organizations WHERE is_publisher`).Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}
	err = d.QueryRow(`
		INSERT INTO organizations(name, is_publisher) VALUES ('Zonventure Catalog', TRUE)
		ON CONFLICT(name) DO UPDATE SET is_publisher = TRUE
		RETURNING id
	`).Scan(&id)
	return id, err
}

// splitCreds parses a "username:password" flag value. Empty parts are
// rejected so the caller doesn't end up with a blank username or an
// unset password.
func splitCreds(creds string) (string, string, error) {
	i := strings.IndexByte(creds, ':')
	if i <= 0 || i == len(creds)-1 {
		return "", "", errors.New(`expected "username:password"`)
	}
	return creds[:i], creds[i+1:], nil
}

// seedDemoAdventure upserts the "demo" adventure and its three pages
// with the placeholder illustration. Idempotent — re-running replaces
// the pages without touching any runs or assignments that reference the
// adventure by slug. The adventure belongs to the catalog publisher
// org. For the full demo dataset (org, users, run, teams, tracks), use
// -seed-demo.
func seedDemoAdventure(d *sql.DB) error {
	pubID, err := ensurePublisherOrg(d)
	if err != nil {
		return err
	}
	var advID int64
	if err := d.QueryRow(`
		INSERT INTO adventures(slug, title, description, author, org_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(slug) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			author = excluded.author,
			org_id = excluded.org_id,
			updated_at = now_text()
		RETURNING id
	`, "demo", "Demo: First Steps",
		"A tiny three-page adventure used as a smoke test.", "zv", pubID).Scan(&advID); err != nil {
		return err
	}
	return publishDemoGraph(context.Background(), d, advID)
}

// seedDemo installs the full demo dataset: admin, org, users, demo adventure,
// assignments, a running demo run, teams with slot magic-links, backfilled
// tracks, and an event log.
// dbTimeLayout matches now_text(). Timestamp columns are TEXT and
// queries like the gate check ORDER BY them, so seeded rows must use
// the same layout as live ones: an RFC3339 "…T…Z" sorts after every
// "… …" row and would masquerade as the newest ping forever.
const dbTimeLayout = "2006-01-02 15:04:05"

func seedDemo(ctx context.Context, d *sql.DB) error {
	// Wipe the mutable parts. adventures are re-created from demoPages().
	for _, stmt := range []string{
		`DELETE FROM users WHERE is_admin = FALSE`,
		`DELETE FROM organizations`,
		`DELETE FROM events`,
		`DELETE FROM adventure_assignments`,
		`DELETE FROM adventures`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			return err
		}
	}

	// Demo adventure in DB, authored by the catalog publisher org.
	pubID, err := ensurePublisherOrg(d)
	if err != nil {
		return err
	}
	if _, err := upsertUser(d, "publisher", "publisher", auth.RoleOrgAdmin, &pubID); err != nil {
		return err
	}
	var advID int64
	if err := d.QueryRow(`
		INSERT INTO adventures(slug, title, description, author, org_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, "demo", "Demo: First Steps",
		"A tiny three-page adventure used as a smoke test.", "zv", pubID).Scan(&advID); err != nil {
		return err
	}
	pages := demoPages()
	if err := publishDemoGraph(ctx, d, advID); err != nil {
		return err
	}

	if _, err := upsertUser(d, "admin", "admin", auth.RoleAdmin, nil); err != nil {
		return err
	}

	var orgID int64
	if err := d.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, "Demo Arrangers").Scan(&orgID); err != nil {
		return err
	}

	bossID, err := upsertUser(d, "boss", "s3cret", auth.RoleOrgAdmin, &orgID)
	if err != nil {
		return err
	}
	if _, err := upsertUser(d, "jane", "jane", auth.RoleOrgMember, &orgID); err != nil {
		return err
	}

	var assignmentID int64
	if err := d.QueryRow(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, $2, $3)
		RETURNING id
	`, orgID, "demo", 25).Scan(&assignmentID); err != nil {
		return err
	}
	if _, err := d.Exec(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, $2, $3)
	`, orgID, "demo", 15); err != nil {
		return err
	}

	startedAt := time.Now().UTC().Add(-30 * time.Minute).Format(dbTimeLayout)
	liveToken := "demo-live-" + fmt.Sprintf("%x", time.Now().UnixNano())
	var eventID int64
	if err := d.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, started_at, created_at, assignment_id, live_token, show_map)
		VALUES ($1, $2, $3, 'running', $4, $5, $6, $7, 1)
		RETURNING id
	`, "demo", orgID, bossID, startedAt, startedAt, assignmentID, liveToken).Scan(&eventID); err != nil {
		return err
	}
	activity.Record(ctx, d, eventID, nil, activity.UserActor(bossID), activity.EventCreated,
		map[string]any{"adventure_slug": "demo"})
	activity.Record(ctx, d, eventID, nil, activity.UserActor(bossID), activity.EventStarted, nil)

	type teamPlan struct {
		Name     string
		Slots    []string
		PageIdx  int
		Progress float64
	}
	plans := []teamPlan{
		{"Team Alfa", []string{"alice", "bob", "celine"}, 2, 0.55},
		{"Team Beta", []string{"dmitri", "erika", "fredrik"}, 1, 0.70},
		{"Team Gamma", []string{"greta", "henrik", "ingrid"}, 0, 0.25},
	}

	rng := rand.New(rand.NewSource(1))
	teamIDs := make([]int64, len(plans))
	for i, plan := range plans {
		teamTok, err := auth.NewSlotToken()
		if err != nil {
			return err
		}
		var teamID int64
		if err := d.QueryRow(
			`INSERT INTO teams(event_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
			eventID, plan.Name, teamTok).Scan(&teamID); err != nil {
			return err
		}
		teamIDs[i] = teamID

		slotIDs := make([]int64, 0, len(plan.Slots))
		for _, name := range plan.Slots {
			token, err := auth.NewSlotToken()
			if err != nil {
				return err
			}
			var id int64
			if err := d.QueryRow(
				`INSERT INTO player_slots(team_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
				teamID, name, token).Scan(&id); err != nil {
				return err
			}
			slotIDs = append(slotIDs, id)
		}
		activity.Record(ctx, d, eventID, &teamID, activity.UserActor(bossID), activity.TeamCreated,
			map[string]any{"name": plan.Name, "members": plan.Slots})

		if err := seedTrackAndEvents(ctx, d, rng, pages, eventID, teamID, slotIDs, plan.PageIdx, plan.Progress); err != nil {
			return err
		}
	}

	// Pin the graph, place every team, and play each up to its page.
	if err := play.Start(ctx, d, eventID); err != nil {
		return err
	}
	at := demoLocator{}
	engine := play.New(d)
	engine.Locator = at
	for i, plan := range plans {
		for _, p := range pages[:plan.PageIdx] {
			at[teamIDs[i]] = [2]float64{p.Lat, p.Lng}
			if _, err := engine.Answer(ctx, teamIDs[i], p.Slug, p.Accept[0]); err != nil {
				return fmt.Errorf("playing %s past %s: %w", plan.Name, p.Slug, err)
			}
		}
	}
	return nil
}

// upsertUser inserts or updates a staff user by username. Pass
// role=auth.RoleAdmin and orgID=nil for global admins; pass
// role=auth.RoleOrgAdmin / auth.RoleOrgMember and orgID for arrangers
// (creates / updates the matching org_memberships row).
func upsertUser(d *sql.DB, username, password, role string, orgID *int64) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	isAdmin := role == auth.RoleAdmin
	// Mark accounts seeded with well-known default passwords so the
	// in-app banner fires until the operator changes them. Demo flow
	// values come from internal docs / docs/selfhosting.md examples;
	// keeping them in one list here means a future "let's add a
	// 'guest' demo user" only needs one new entry.
	isDefault := 0
	switch password {
	case "admin", "rootpw", "s3cret", "jane", "publisher":
		isDefault = 1
	}
	var id int64
	if err := d.QueryRow(`
		INSERT INTO users(username, password_hash, is_admin, password_is_default)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(username) DO UPDATE SET
			password_hash = excluded.password_hash,
			is_admin = excluded.is_admin,
			password_is_default = excluded.password_is_default
		RETURNING id
	`, username, hash, isAdmin, isDefault).Scan(&id); err != nil {
		return 0, err
	}
	if !isAdmin && orgID != nil {
		if _, err := d.Exec(`
			INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, $3)
			ON CONFLICT(user_id, org_id) DO UPDATE SET role = excluded.role
		`, id, *orgID, role); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// seedTrackAndEvents writes plausible walking pings + answer events for a
// team, using slot ids as the actors.
func seedTrackAndEvents(ctx context.Context, d *sql.DB, rng *rand.Rand,
	pages []demoPage, eventID, teamID int64,
	slotIDs []int64, currentPage int, partialProgress float64) error {

	type leg struct {
		fromLat, fromLng, toLat, toLng float64
	}
	legs := []leg{}
	for i := 0; i < currentPage && i+1 < len(pages); i++ {
		legs = append(legs, leg{pages[i].Lat, pages[i].Lng, pages[i+1].Lat, pages[i+1].Lng})
	}
	if currentPage+1 < len(pages) {
		legs = append(legs, leg{pages[currentPage].Lat, pages[currentPage].Lng,
			pages[currentPage+1].Lat, pages[currentPage+1].Lng})
	}
	legDists := make([]float64, len(legs))
	total := 0.0
	for i, l := range legs {
		legDists[i] = haversine(l.fromLat, l.fromLng, l.toLat, l.toLng)
		if i == len(legs)-1 && partialProgress < 1 {
			legDists[i] *= partialProgress
		}
		total += legDists[i]
	}
	if total == 0 || len(slotIDs) == 0 {
		return nil
	}
	runDuration := 30 * time.Minute
	startTS := time.Now().UTC().Add(-runDuration)

	legStart := startTS
	for li, l := range legs {
		frac := legDists[li] / total
		legDuration := time.Duration(float64(runDuration) * frac)
		legEnd := legStart.Add(legDuration)

		steps := int(legDuration.Seconds() / 20)
		if steps < 3 {
			steps = 3
		}
		progressEnd := 1.0
		if li == len(legs)-1 && partialProgress < 1 {
			progressEnd = partialProgress
		}
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			lat := lerp(l.fromLat, l.toLat, t*progressEnd) + (rng.Float64()-0.5)*0.00008
			lng := lerp(l.fromLng, l.toLng, t*progressEnd) + (rng.Float64()-0.5)*0.00012
			ts := legStart.Add(time.Duration(float64(legDuration) * t))
			sid := slotIDs[s%len(slotIDs)]
			if _, err := d.ExecContext(ctx, `
				INSERT INTO location_pings(slot_id, team_id, event_id, lat, lng, accuracy, server_ts)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, sid, teamID, eventID, lat, lng, 12.0+rng.Float64()*6, ts.Format(dbTimeLayout)); err != nil {
				return err
			}
		}
		if li < currentPage {
			payload, _ := json.Marshal(map[string]any{"page_index": li})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO event_activity(event_id, team_id, actor_slot_id, kind, payload, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, eventID, teamID, slotIDs[0], activity.AnswerRight, string(payload),
				legEnd.Add(-2*time.Minute).Format(dbTimeLayout))
			payload, _ = json.Marshal(map[string]any{"page_index": li, "submitted": pickWrongGuess(rng)})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO event_activity(event_id, team_id, actor_slot_id, kind, payload, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, eventID, teamID, slotIDs[0], activity.AnswerWrong, string(payload),
				legEnd.Add(-3*time.Minute).Format(dbTimeLayout))
		}
		legStart = legEnd
	}
	return nil
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

var wrongGuesses = []string{"hej", "goodbye", "green", "red", "adventure", "final"}

func pickWrongGuess(rng *rand.Rand) string {
	return wrongGuesses[rng.Intn(len(wrongGuesses))]
}

// placeholderImage is an SVG treasure-map illustration seeded on every demo
// puzzle. Brand raspberry dashed trail from a starting dot to an X-mark,
// compass rose bottom-right, soft mountain silhouette under the horizon.
// Small enough (~1 KB) to embed without bloating the binary.
const placeholderImage = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 240" role="img" aria-label="Treasure map">
  <defs>
    <linearGradient id="paper" x1="0" x2="0" y1="0" y2="1">
      <stop offset="0" stop-color="#faf3e0"/>
      <stop offset="1" stop-color="#eeddb7"/>
    </linearGradient>
  </defs>
  <rect width="400" height="240" fill="url(#paper)"/>
  <path d="M0 240 L60 180 L120 220 L200 160 L260 200 L340 170 L400 210 L400 240 Z"
        fill="#1f3349" opacity="0.18"/>
  <g stroke="#ad1357" stroke-width="3" fill="none" stroke-dasharray="7 5" stroke-linecap="round">
    <path d="M30 205 C 100 140, 160 200, 220 130 S 330 40, 360 35"/>
  </g>
  <circle cx="30" cy="205" r="6" fill="#1f3349" stroke="#fff" stroke-width="2"/>
  <g transform="translate(360 35) rotate(-15)" stroke="#ad1357" stroke-width="5" stroke-linecap="round">
    <line x1="-11" y1="-11" x2="11" y2="11"/>
    <line x1="11" y1="-11" x2="-11" y2="11"/>
  </g>
  <g transform="translate(340 190)">
    <circle r="26" fill="#fff" stroke="#1f3349" stroke-width="1.8"/>
    <polygon points="0,-22 4,0 0,8 -4,0" fill="#ad1357"/>
    <polygon points="0,22 4,0 0,-8 -4,0" fill="#1f3349"/>
    <text y="-10" text-anchor="middle" font-family="Open Sans, sans-serif"
          font-weight="700" font-size="9" fill="#1f3349">N</text>
  </g>
</svg>`

// silence the unused errors import in builds where seedDemo is not changed.
var _ = errors.New
