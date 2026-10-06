package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/server"
)

// newTestServer spins up the full HTTP stack against a per-test Postgres
// database plus a published three-puzzle demo adventure (demoGraph).
// Returns an assignment id the seeded arranger can consume to create an
// event.
func newTestServer(t *testing.T) (*httptest.Server, int64, func()) {
	t.Helper()
	return newTestServerWith(t, server.Config{})
}

// newTestServerWith is newTestServer with extra server config; DB and
// Library are filled in.
func newTestServerWith(t *testing.T, cfg server.Config) (*httptest.Server, int64, func()) {
	t.Helper()
	sqlDB, dbCleanup := dbtest.Open(t)
	lib := adventure.New(sqlDB)

	// Seed the demo adventure directly (fine for tests — production uses the
	// editor or the seed-demo flow). It belongs to the catalog publisher
	// org, whose org_admin "pub" authors it.
	var advID int64
	if err := sqlDB.QueryRow(`
		INSERT INTO adventures(slug, title, description, author, org_id) VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, "demo", "Demo: First Steps", "", "zv", seedPublisher(t, sqlDB)).Scan(&advID); err != nil {
		dbCleanup()
		t.Fatalf("seed adventure: %v", err)
	}
	if _, _, err := graphstore.Publish(context.Background(), sqlDB, advID, 0, demoGraph(), ""); err != nil {
		dbCleanup()
		t.Fatalf("publish demo graph: %v", err)
	}

	// Seed an organization + an org_admin (boss) + a global admin (root)
	// + one assignment. Two users are cheaper than bcrypt-hashing on
	// demand inside individual tests.
	var orgID int64
	if err := sqlDB.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, "TestOrg").Scan(&orgID); err != nil {
		dbCleanup()
		t.Fatalf("seed org: %v", err)
	}
	// Post-migration 0020 schema: role column dropped from users.
	// Org-scoped role lives on org_memberships; admin status on
	// users.is_admin.
	hash := fixtureHash(t, "s3cret")
	var bossID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO users(username, password_hash, email_verified) VALUES ($1, $2, 1) RETURNING id`,
		"boss", hash).Scan(&bossID); err != nil {
		dbCleanup()
		t.Fatalf("seed arranger: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		bossID, orgID); err != nil {
		dbCleanup()
		t.Fatalf("seed arranger membership: %v", err)
	}
	rootHash := fixtureHash(t, "rootpw")
	if _, err := sqlDB.Exec(
		`INSERT INTO users(username, password_hash, email_verified, is_admin) VALUES ($1, $2, 1, TRUE)`,
		"root", rootHash); err != nil {
		dbCleanup()
		t.Fatalf("seed admin: %v", err)
	}
	_ = bossID // referenced by the membership insert above
	// Demo adventure is a catalog adventure (publisher-owned, not
	// published) so the catalog-vs-org-owned ownership tests can rely
	// on it. To let boss's org consume it for events / runs, drop in a
	// share — the operator commissioning it for the org.
	if _, err := sqlDB.Exec(
		`INSERT INTO adventure_shares(adventure_slug, org_id) VALUES ('demo', $1)`,
		orgID); err != nil {
		dbCleanup()
		t.Fatalf("share demo to org: %v", err)
	}
	var assignmentID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO adventure_assignments(org_id, adventure_slug, max_players) VALUES ($1, $2, $3) RETURNING id`,
		orgID, "demo", 20).Scan(&assignmentID); err != nil {
		dbCleanup()
		t.Fatalf("assign: %v", err)
	}

	cfg.DB, cfg.Library = sqlDB, lib
	h := server.New(cfg)
	ts := httptest.NewServer(h)
	cleanup := func() {
		ts.Close()
		dbCleanup()
	}
	return ts, assignmentID, cleanup
}

// demoGraph is the fixture adventure: start → welcome → landmark (gated
// on a zone at the fountain) → finish → end.
func demoGraph() *graph.Graph {
	puzzle := func(slug, title, prompt, zone string, answers ...string) graph.Node {
		ms := make([]match.Matcher, len(answers))
		for i, a := range answers {
			ms[i] = match.Matcher{Kind: match.Exact, Value: a}
		}
		return graph.Node{Slug: slug, Type: graph.Puzzle, Title: title, PuzzleCfg: &graph.PuzzleConfig{
			Blocks:  graph.LegacyBlocks(prompt, nil),
			Zone:    zone,
			Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: ms}},
		}}
	}
	return &graph.Graph{
		Zones: []graph.Zone{{Slug: "fountain", Name: "Landmark", Lat: 59.3326, Lng: 18.0649, RadiusM: 50}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			puzzle("welcome", "Welcome", "What word do you say when you meet someone for the first time?", "", "hello", "hi"),
			puzzle("landmark", "Landmark", "Find the fountain in the central park. What colour are its tiles?", "fountain", "blue", "azure"),
			puzzle("finish", "The Finish", `Name the Swedish word for "adventure".`, "", "äventyr", "aventyr"),
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{
			{From: "start", FromPort: graph.PortOut, To: "welcome"},
			{From: "welcome", FromPort: "solved", To: "landmark"},
			{From: "landmark", FromPort: "solved", To: "finish"},
			{From: "finish", FromPort: "solved", To: "end"},
		},
	}
}

// client is a tiny helper that keeps cookies across requests and does JSON
// encode/decode, because net/http.Client on its own is too verbose for this
// many round-trips.
type client struct {
	t  *testing.T
	c  *http.Client
	ts *httptest.Server
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &client{t: t, c: &http.Client{Jar: jar}, ts: ts}
	// Seed the zv_csrf cookie the same way a real browser would: by
	// loading the entry page first. Subsequent mutating calls echo
	// the cookie value as X-CSRF-Token so the RequireCSRF middleware
	// lets them through.
	if _, err := c.c.Get(ts.URL + "/api/health"); err != nil {
		t.Fatalf("csrf seed: %v", err)
	}
	return c
}

// in returns a copy of c that reports to t but shares c's cookies, so a
// subtest can reuse a client its parent logged in: failing the parent's
// t from inside a subtest is not allowed.
func (c *client) in(t *testing.T) *client {
	cp := *c
	cp.t = t
	return &cp
}

// csrfCookie returns the current zv_csrf cookie value from the jar, or
// "" if absent. The jar is keyed by URL host, so we ask via the test
// server's URL.
func (c *client) csrfCookie() string {
	u, err := url.Parse(c.ts.URL)
	if err != nil {
		return ""
	}
	for _, ck := range c.c.Jar.Cookies(u) {
		if ck.Name == auth.CSRFCookieName {
			return ck.Value
		}
	}
	return ""
}

func (c *client) do(method, path string, body any, into any) int {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		if tok := c.csrfCookie(); tok != "" {
			req.Header.Set(auth.CSRFHeaderName, tok)
		}
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if into != nil && resp.StatusCode < 400 && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil && err != io.EOF {
			c.t.Fatalf("decode: %v", err)
		}
	}
	return resp.StatusCode
}

func TestFullFlow(t *testing.T) {
	ts, assignmentID, cleanup := newTestServer(t)
	defer cleanup()

	// --- Anonymous callers are refused on both sides. ---
	anon := newClient(t, ts)
	if s := anon.do("GET", "/api/player/play", nil, nil); s != 401 {
		t.Fatalf("anonymous state = %d, want 401", s)
	}
	if s := anon.do("GET", "/api/admin/adventures", nil, nil); s != 401 {
		t.Fatalf("anonymous admin = %d, want 401", s)
	}
	// A magic link the server can't honour leaves /play with the token
	// stripped and a reason the page can show, rather than asking the
	// player to open the link they just opened.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(ts.URL + "/play?t=not-a-real-token")
	if err != nil {
		t.Fatalf("GET bad magic link: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("bad magic link status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/play?link=invalid" {
		t.Fatalf("bad magic link Location = %q, want /play?link=invalid", got)
	}

	// --- Arranger logs in and schedules an event. ---
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	// The arranger has a staff cookie but no slot cookie, so player
	// endpoints reject with 401 ("open your magic link").
	if s := arr.do("GET", "/api/player/play", nil, nil); s != 401 {
		t.Fatalf("arranger on player = %d, want 401", s)
	}
	var advList struct {
		Adventures []struct {
			Slug      string `json:"slug"`
			PageCount int    `json:"page_count"`
		} `json:"adventures"`
	}
	if s := arr.do("GET", "/api/admin/adventures", nil, &advList); s != 200 {
		t.Fatalf("list adventures = %d", s)
	}
	if len(advList.Adventures) == 0 || advList.Adventures[0].Slug != "demo" {
		t.Fatalf("expected demo adventure, got %+v", advList.Adventures)
	}

	var run struct {
		ID int64 `json:"id"`
	}
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID, "attest_no_minors": true}, &run); s != 201 {
		t.Fatalf("create event = %d", s)
	}

	// Create a team with one slot — the server mints a magic-link token.
	var team struct {
		ID      int64 `json:"id"`
		Members []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Token string `json:"token"`
		} `json:"members"`
	}
	teamReq := map[string]any{
		"name":         "Team Alpha",
		"member_names": []string{"alice"},
	}
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/teams", teamReq, &team); s != 201 {
		t.Fatalf("create team = %d", s)
	}
	if len(team.Members) != 1 || team.Members[0].Token == "" {
		t.Fatalf("expected one slot with a token, got %+v", team.Members)
	}

	// --- Player logs in by following the magic-link URL. The 302 response
	//     drops the zv_slot cookie in the jar; subsequent API calls inherit it.
	alice := newClient(t, ts)
	if s := alice.do("GET", "/play?t="+team.Members[0].Token, nil, nil); s != 200 {
		t.Fatalf("magic-link login = %d", s)
	}
	var st playStateDTO
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View != nil {
		t.Fatalf("before start: %d %+v", s, st.View)
	}

	// Submitting before start must be rejected.
	answer := func(node, guess string) (int, session.Result) {
		var res session.Result
		s := alice.doAny("POST", "/api/player/play/answer", map[string]string{"node": node, "guess": guess}, &res)
		return s, res
	}
	if s, _ := answer("welcome", "hello"); s == 200 {
		t.Fatalf("an answer before the start was accepted")
	}

	// --- Start the event. ---
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start event = %d", s)
	}
	if s := alice.doAny("GET", "/api/player/play", nil, &st); s != 200 || st.View == nil || st.View.Node != "welcome" {
		t.Fatalf("after start: %d %+v", s, st.View)
	}

	// Wrong answer -> not accepted, no progression.
	if s, res := answer("welcome", "goodbye"); s != 200 || res.Accepted {
		t.Fatalf("wrong answer = %d accepted=%v", s, res.Accepted)
	}
	// Right answer (upper-case + padding to check normalisation) moves
	// the team on to the gated landmark.
	if s, res := answer("welcome", "  HELLO  "); s != 200 || !res.Accepted || res.View.Node != "landmark" {
		t.Fatalf("right answer = %d %+v", s, res)
	}

	// Away from the fountain the puzzle is readable but its answer is
	// refused: arrival is the prerequisite for answering, not reading.
	if s, _ := answer("landmark", "blue"); s != 403 {
		t.Fatalf("answer away from the zone = %d, want 403", s)
	}
	if s := alice.do("POST", "/api/player/location", map[string]any{"lat": 59.3326, "lng": 18.0649}, nil); s != 204 {
		t.Fatalf("post location = %d", s)
	}
	if s, res := answer("landmark", "BLUE"); s != 200 || !res.Accepted {
		t.Fatalf("answer at the zone = %d accepted=%v", s, res.Accepted)
	}

	// Final puzzle, correct answer completes the event.
	if s, res := answer("finish", "äventyr"); s != 200 || !res.Accepted || !res.View.Finished {
		t.Fatalf("final answer = %d %+v", s, res)
	}

	// Once the event completes, MaybeCompleteEvent expires every
	// slot session so the player is bounced off the active screen
	// rather than left lingering on a frozen view. The shape this
	// pins is "401 on subsequent player calls" — the older
	// "GET state still works and reports status=completed" posture
	// changed when slot-session expiration moved into the
	// completion / cancellation flow. Recap-token surfaces remain
	// available; that's where players land for the post-event
	// summary, not /api/player/play.
	if s := alice.do("GET", "/api/player/play", nil, nil); s != 401 {
		t.Fatalf("final state after completion = %d, want 401 (slot session expired on completion)", s)
	}

	// Arranger positions listing contains Team Alpha with latest ping.
	var pos struct {
		Positions []struct {
			TeamName string  `json:"team_name"`
			Lat      float64 `json:"lat"`
			Lng      float64 `json:"lng"`
		} `json:"positions"`
	}
	if s := arr.do("GET", "/api/admin/events/"+itoa(run.ID)+"/positions", nil, &pos); s != 200 {
		t.Fatalf("positions = %d", s)
	}
	if len(pos.Positions) != 1 || pos.Positions[0].TeamName != "Team Alpha" {
		t.Fatalf("unexpected positions: %+v", pos.Positions)
	}
	if pos.Positions[0].Lat == 0 && pos.Positions[0].Lng == 0 {
		t.Fatalf("expected non-zero coords")
	}

	// The game refused alice's session once the event was over, but she
	// can still erase her data from the phone; afterwards the session is
	// gone for erasing too. (Last: erasing takes her pings with it.)
	if s := alice.doAny("POST", "/api/player/forget", map[string]any{}, nil); s/100 != 2 {
		t.Fatalf("forget after the event ended = %d", s)
	}
	if s := alice.doAny("POST", "/api/player/forget", map[string]any{}, nil); s != 401 {
		t.Errorf("after forgetting, the session should be gone: %d", s)
	}
}

func itoa(i int64) string {
	buf := make([]byte, 0, 20)
	neg := i < 0
	if neg {
		i = -i
	}
	if i == 0 {
		return "0"
	}
	for i > 0 {
		buf = append(buf, byte('0'+i%10))
		i /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	return string(buf)
}

// fixtureHashes caches one real, full-cost password hash per fixture
// password for the whole test process. Each hash is still produced by
// auth.HashPassword at production strength — it is just computed once
// instead of in every fixture, since hashing dominates fixture setup.
// Logins still verify against it at full cost.
var fixtureHashes struct {
	sync.Mutex
	m map[string]string
}

func fixtureHash(t *testing.T, password string) string {
	t.Helper()
	fixtureHashes.Lock()
	defer fixtureHashes.Unlock()
	if h, ok := fixtureHashes.m[password]; ok {
		return h
	}
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash %q: %v", password, err)
	}
	if fixtureHashes.m == nil {
		fixtureHashes.m = map[string]string{}
	}
	fixtureHashes.m[password] = h
	return h
}

// seedPublisher creates the catalog publisher org with an org_admin
// "pub" / "pubpw" who authors its adventures, and returns the org id.
func seedPublisher(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	orgID := dbtest.PublisherOrg(t, db)
	hash := fixtureHash(t, "pubpw")
	var userID int64
	if err := db.QueryRow(
		`INSERT INTO users(username, password_hash, email_verified) VALUES ('pub', $1, 1) RETURNING id`,
		hash).Scan(&userID); err != nil {
		t.Fatalf("seed publisher user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgID); err != nil {
		t.Fatalf("seed publisher membership: %v", err)
	}
	return orgID
}
