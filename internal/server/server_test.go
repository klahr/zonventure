package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/dbtest"
	"git.sr.ht/~klahr/zv/internal/server"
)

// newTestServer spins up the full HTTP stack against a per-test Postgres
// database plus the real demo adventure shipped in the repo. Returns an
// assignment id the seeded arranger can consume to create an event.
func newTestServer(t *testing.T) (*httptest.Server, int64, func()) {
	t.Helper()
	sqlDB, dbCleanup := dbtest.Open(t)
	lib := adventure.New(sqlDB)

	// Seed the demo adventure directly (fine for tests — production uses the
	// admin editor or the seed-demo flow).
	var advID int64
	if err := sqlDB.QueryRow(`
		INSERT INTO adventures(slug, title, description, author) VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "demo", "Demo: First Steps", "", "zv").Scan(&advID); err != nil {
		dbCleanup()
		t.Fatalf("seed adventure: %v", err)
	}
	pages := []struct {
		title, prompt, accept string
		lat, lng              float64
		radius                any
	}{
		{"Welcome", "What word do you say when you meet someone for the first time?",
			`["hello","hi"]`, 59.3308, 18.0731, nil},
		{"Landmark", "Find the fountain in the central park. What colour are its tiles?",
			`["blue","azure"]`, 59.3326, 18.0649, 50.0},
		{"The Finish", `Name the Swedish word for "adventure".`,
			`["äventyr","aventyr"]`, 59.3252, 18.0708, nil},
	}
	for i, p := range pages {
		if _, err := sqlDB.Exec(`
			INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, radius_m)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, advID, i, p.title, p.prompt, p.accept, p.lat, p.lng, p.radius); err != nil {
			dbCleanup()
			t.Fatalf("seed page %d: %v", i, err)
		}
	}

	// Seed an organization + an org_admin (boss) + a global admin (root)
	// + one assignment. Two users are cheaper than bcrypt-hashing on
	// demand inside individual tests.
	var orgID int64
	if err := sqlDB.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, "TestOrg").Scan(&orgID); err != nil {
		dbCleanup()
		t.Fatalf("seed org: %v", err)
	}
	hash, _ := auth.HashPassword("s3cret")
	if _, err := sqlDB.Exec(
		`INSERT INTO users(username, password_hash, role, org_id) VALUES ($1, $2, 'org_admin', $3)`,
		"boss", hash, orgID); err != nil {
		dbCleanup()
		t.Fatalf("seed arranger: %v", err)
	}
	rootHash, _ := auth.HashPassword("rootpw")
	if _, err := sqlDB.Exec(
		`INSERT INTO users(username, password_hash, role, org_id) VALUES ($1, $2, 'admin', NULL)`,
		"root", rootHash); err != nil {
		dbCleanup()
		t.Fatalf("seed admin: %v", err)
	}
	var assignmentID int64
	if err := sqlDB.QueryRow(
		`INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams) VALUES ($1, $2, $3) RETURNING id`,
		orgID, "demo", 3).Scan(&assignmentID); err != nil {
		dbCleanup()
		t.Fatalf("assign: %v", err)
	}

	h := server.New(server.Config{DB: sqlDB, Library: lib})
	ts := httptest.NewServer(h)
	cleanup := func() {
		ts.Close()
		dbCleanup()
	}
	return ts, assignmentID, cleanup
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

	// --- Arranger logs in and schedules an event. ---
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
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
	if s := arr.do("POST", "/api/admin/events", map[string]any{"assignment_id": assignmentID}, &run); s != 201 {
		t.Fatalf("create run = %d", s)
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
	var state struct {
		Status    string `json:"status"`
		PageIndex int    `json:"page_index"`
		Prompt    string `json:"prompt"`
		Gate      *struct {
			Reached bool `json:"reached"`
		} `json:"gate"`
	}
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 {
		t.Fatalf("get state = %d", s)
	}
	if state.Status != "waiting" {
		t.Fatalf("expected waiting, got %q", state.Status)
	}

	// Submitting before start must be rejected.
	var answerResp struct {
		Accepted bool `json:"accepted"`
	}
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "hello"}, &answerResp); s != 409 {
		t.Fatalf("early answer status = %d, want 409", s)
	}

	// --- Start the event. ---
	if s := arr.do("POST", "/api/admin/events/"+itoa(run.ID)+"/start", map[string]string{}, nil); s != 204 {
		t.Fatalf("start run = %d", s)
	}

	// State should now be running on page 0 with a prompt.
	state = struct {
		Status    string `json:"status"`
		PageIndex int    `json:"page_index"`
		Prompt    string `json:"prompt"`
		Gate      *struct {
			Reached bool `json:"reached"`
		} `json:"gate"`
	}{}
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 {
		t.Fatalf("get state = %d", s)
	}
	if state.Status != "running" || state.Prompt == "" {
		t.Fatalf("unexpected state after start: %+v", state)
	}

	// Wrong answer -> accepted=false, no progression.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "goodbye"}, &answerResp); s != 200 {
		t.Fatalf("wrong answer = %d", s)
	}
	if answerResp.Accepted {
		t.Fatalf("wrong answer was accepted")
	}

	// Right answer (upper-case + padding to check normalisation) -> advances
	// to page 1 which has a gate.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "  HELLO  "}, &answerResp); s != 200 {
		t.Fatalf("right answer = %d", s)
	}
	if !answerResp.Accepted {
		t.Fatalf("right answer rejected")
	}

	// Before any ping, page 1 is gated — but the prompt and gate info
	// are both visible so the team can read the puzzle while they walk
	// there. Arrival is the prerequisite for answering, not for seeing
	// the content.
	state.Prompt = ""
	state.Gate = nil
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 {
		t.Fatalf("get state gated = %d", s)
	}
	if state.Status != "running" || state.Prompt == "" || state.Gate == nil || state.Gate.Reached {
		t.Fatalf("expected running with prompt and unreached gate, got %+v", state)
	}

	// Submitting the page-1 answer while still gated is refused — the
	// content being visible doesn't mean the answer counts yet.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "blue"}, &answerResp); s != 403 {
		t.Fatalf("gated answer = %d, want 403", s)
	}

	// Post a location ping near the gate (Sergels torg coords from demo/02).
	if s := alice.do("POST", "/api/player/location", map[string]any{"lat": 59.3326, "lng": 18.0649}, nil); s != 204 {
		t.Fatalf("post location = %d", s)
	}

	// Now the gate is reached and the prompt appears.
	state.Prompt = ""
	state.Gate = nil
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 {
		t.Fatalf("get state after ping = %d", s)
	}
	if state.Status != "running" || state.Prompt == "" || state.Gate == nil || !state.Gate.Reached {
		t.Fatalf("expected running with reached gate, got %+v", state)
	}

	// Correct answer, advance to final page.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "BLUE"}, &answerResp); s != 200 || !answerResp.Accepted {
		t.Fatalf("page 2 answer = %d accepted=%v", s, answerResp.Accepted)
	}

	// Final page, correct answer completes the event.
	if s := alice.do("POST", "/api/player/answer", map[string]string{"answer": "äventyr"}, &answerResp); s != 200 || !answerResp.Accepted {
		t.Fatalf("final answer = %d accepted=%v", s, answerResp.Accepted)
	}

	// State becomes "completed".
	state.Status = ""
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 || state.Status != "completed" {
		t.Fatalf("final state status = %q (status %d)", state.Status, s)
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
}

func TestAuthRoleSeparation(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	anon := newClient(t, ts)
	if s := anon.do("GET", "/api/player/state", nil, nil); s != 401 {
		t.Fatalf("anonymous state = %d, want 401", s)
	}
	if s := anon.do("GET", "/api/admin/adventures", nil, nil); s != 401 {
		t.Fatalf("anonymous admin = %d, want 401", s)
	}

	// Arranger has a staff cookie but no slot cookie, so player endpoints
	// reject with 401 ("open your magic link").
	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("arranger login = %d", s)
	}
	if s := arr.do("GET", "/api/player/state", nil, nil); s != 401 {
		t.Fatalf("arranger on player = %d, want 401", s)
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
