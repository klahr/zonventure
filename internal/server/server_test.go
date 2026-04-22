package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/db"
	"git.sr.ht/~klahr/zv/internal/server"
)

// newTestServer spins up the full HTTP stack against a fresh on-disk SQLite
// database plus the real demo adventure shipped in the repo. Returns an
// assignment id the seeded arranger can consume to create a run.
func newTestServer(t *testing.T) (*httptest.Server, int64, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "zv.db")
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	lib, err := adventure.Load(filepath.Join("..", "..", "adventures"))
	if err != nil {
		sqlDB.Close()
		t.Fatalf("load adventures: %v", err)
	}
	if lib.Get("demo") == nil {
		sqlDB.Close()
		t.Fatalf("demo adventure missing; tests rely on it")
	}

	// Seed an organization + an org_admin (boss) + one assignment. The test
	// exercises the arranger flow without depending on CLI flags.
	orgRes, err := sqlDB.Exec(`INSERT INTO organizations(name) VALUES (?)`, "TestOrg")
	if err != nil {
		sqlDB.Close()
		t.Fatalf("seed org: %v", err)
	}
	orgID, _ := orgRes.LastInsertId()
	hash, _ := auth.HashPassword("s3cret")
	if _, err := sqlDB.Exec(
		`INSERT INTO users(username, password_hash, role, org_id) VALUES (?, ?, 'org_admin', ?)`,
		"boss", hash, orgID); err != nil {
		sqlDB.Close()
		t.Fatalf("seed arranger: %v", err)
	}
	ar, err := sqlDB.Exec(`INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams) VALUES (?, ?, ?)`, orgID, "demo", 3)
	if err != nil {
		sqlDB.Close()
		t.Fatalf("assign: %v", err)
	}
	assignmentID, _ := ar.LastInsertId()

	h := server.New(server.Config{DB: sqlDB, Library: lib})
	ts := httptest.NewServer(h)
	cleanup := func() {
		ts.Close()
		sqlDB.Close()
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
	return &client{t: t, c: &http.Client{Jar: jar}, ts: ts}
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

	// --- Arranger logs in and schedules a run. ---
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
	if s := arr.do("POST", "/api/admin/runs", map[string]any{"assignment_id": assignmentID}, &run); s != 201 {
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
	if s := arr.do("POST", "/api/admin/runs/"+itoa(run.ID)+"/teams", teamReq, &team); s != 201 {
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

	// --- Start the run. ---
	if s := arr.do("POST", "/api/admin/runs/"+itoa(run.ID)+"/start", map[string]string{}, nil); s != 204 {
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

	// Before any ping, page 1 is gated: no prompt ships.
	state.Prompt = ""
	state.Gate = nil
	if s := alice.do("GET", "/api/player/state", nil, &state); s != 200 {
		t.Fatalf("get state gated = %d", s)
	}
	if state.Status != "gated" || state.Prompt != "" || state.Gate == nil || state.Gate.Reached {
		t.Fatalf("expected gated with no prompt, got %+v", state)
	}

	// Submitting the page-1 answer while still gated is refused.
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

	// Final page, correct answer completes the run.
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
	if s := arr.do("GET", "/api/admin/runs/"+itoa(run.ID)+"/positions", nil, &pos); s != 200 {
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
