package server_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// tinyPDF is the start of a PDF: enough for the type sniffer.
var tinyPDF = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")

// get fetches path and returns its status, headers and body.
func (c *client) get(path string) (int, http.Header, []byte) {
	c.t.Helper()
	resp, err := c.c.Get(c.ts.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

// The organisers leave files for an event's players, who see them in
// the lobby before the start and download them; players of another
// event can't.
func TestEventFiles(t *testing.T) {
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{})
	defer cleanup()

	arr := newClient(t, ts)
	if s := arr.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	// One player on a team of a fresh event of the org.
	newEvent := func() (int64, *client) {
		t.Helper()
		id := seedEventInOrg(t, db, orgID)
		if _, err := db.Exec(`UPDATE events SET live_token = 'tok-' || id WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		var tm struct {
			Members []struct {
				Token string `json:"token"`
			} `json:"members"`
		}
		if s := arr.do("POST", "/api/admin/events/"+itoa(id)+"/teams", map[string]any{"name": "Team 1", "member_names": []string{"Player 1"}}, &tm); s != 201 {
			t.Fatalf("create team = %d", s)
		}
		p := newClient(t, ts)
		if s := p.do("GET", "/play?t="+tm.Members[0].Token, nil, nil); s != 200 {
			t.Fatalf("magic-link login = %d", s)
		}
		return id, p
	}
	evID, player := newEvent()
	_, stranger := newEvent()
	files := "/api/admin/events/" + itoa(evID) + "/files"

	s, out := arr.uploadNamed(files, "../Regler för dagen.pdf", tinyPDF)
	if s != 201 {
		t.Fatalf("upload = %d %v", s, out)
	}
	id := int64(out["id"].(float64))
	if out["name"] != "Regler för dagen.pdf" || out["mime"] != "application/pdf" {
		t.Errorf("stored as %v", out)
	}

	if s, out := arr.uploadNamed(files, "evil.html", []byte("<html><script>alert(1)</script></html>")); s != 400 {
		t.Errorf("html upload = %d %v, want 400", s, out)
	}

	var st struct {
		Event struct {
			State string `json:"state"`
			Files []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"files"`
		} `json:"event"`
	}
	if s := player.doAny("GET", "/api/player/play", nil, &st); s != 200 {
		t.Fatalf("state = %d", s)
	}
	if st.Event.State != "scheduled" || len(st.Event.Files) != 1 || st.Event.Files[0].ID != id || st.Event.Files[0].Size != int64(len(tinyPDF)) {
		t.Fatalf("lobby state = %+v", st.Event)
	}

	s, h, body := player.get("/api/player/files/" + itoa(id))
	if s != 200 || !bytes.Equal(body, tinyPDF) {
		t.Fatalf("download = %d (%d bytes)", s, len(body))
	}
	if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=UTF-8''Regler%20f%C3%B6r%20dagen.pdf") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if h.Get("Content-Type") != "application/pdf" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", h)
	}

	if s, _, _ := stranger.get("/api/player/files/" + itoa(id)); s != 404 {
		t.Errorf("another event's player = %d, want 404", s)
	}
	// The emails' links: by the team's share token, no game session.
	// The event's team first, then the other event's.
	var tokens []string
	rows, err := db.Query(`SELECT token FROM teams ORDER BY event_id = $1 DESC`, evID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, tok)
	}
	rows.Close()
	if len(tokens) != 2 {
		t.Fatalf("teams = %d, want 2", len(tokens))
	}
	anon := newClient(t, ts)
	if s, h, body := anon.get("/team/" + tokens[0] + "/files/" + itoa(id)); s != 200 || !bytes.Equal(body, tinyPDF) || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") {
		t.Errorf("download by team token = %d", s)
	}
	// The team page lists the file, linked the same way; the other
	// event's team page doesn't.
	if s, _, body := anon.get("/team/" + tokens[0]); s != 200 || !strings.Contains(string(body), `href="/team/`+tokens[0]+`/files/`+itoa(id)+`"`) || !strings.Contains(string(body), "Regler för dagen.pdf") {
		t.Errorf("team page = %d, lacks the file link", s)
	}
	// An adventure in Swedish has its team page in Swedish.
	if _, err := db.Exec(`UPDATE adventures SET language = 'sv' WHERE slug = 'demo'`); err != nil {
		t.Fatal(err)
	}
	if _, _, body := anon.get("/team/" + tokens[0]); !strings.Contains(string(body), `<html lang="sv">`) || !strings.Contains(string(body), "Filer från arrangörerna") || strings.Contains(string(body), "Files from the organisers") || !strings.Contains(string(body), `addEventListener("change"`) {
		t.Errorf("Swedish team page:\n%s", body)
	}
	if _, err := db.Exec(`UPDATE adventures SET language = 'en' WHERE slug = 'demo'`); err != nil {
		t.Fatal(err)
	}
	if _, _, body := anon.get("/team/" + tokens[1]); strings.Contains(string(body), "Files from the organisers") {
		t.Errorf("another event's team page lists the file")
	}
	if s, _, _ := anon.get("/team/" + tokens[1] + "/files/" + itoa(id)); s != 404 {
		t.Errorf("another event's team token = %d, want 404", s)
	}
	if s, _, _ := anon.get("/team/nope/files/" + itoa(id)); s != 404 {
		t.Errorf("unknown team token = %d, want 404", s)
	}
	if s, _, _ := arr.get(files + "/" + itoa(id)); s != 200 {
		t.Errorf("organiser download = %d", s)
	}

	if s := arr.do("DELETE", files+"/"+itoa(id), nil, nil); s != 204 {
		t.Fatalf("delete = %d", s)
	}
	if s, _, _ := player.get("/api/player/files/" + itoa(id)); s != 404 {
		t.Errorf("deleted file = %d, want 404", s)
	}

	for i := 0; i < 10; i++ {
		if s, out := arr.uploadNamed(files, "f.pdf", tinyPDF); s != 201 {
			t.Fatalf("upload %d = %d %v", i, s, out)
		}
	}
	if s, _ := arr.uploadNamed(files, "one-too-many.pdf", tinyPDF); s != 400 {
		t.Errorf("eleventh file = %d, want 400", s)
	}
}
