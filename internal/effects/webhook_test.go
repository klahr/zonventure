package effects

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/webhooks"
)

// queue makes an event with one team and queues a webhook for it, as a
// team passing a "Call a webhook" step does.
func queue(t *testing.T, db *sql.DB, hook Webhook) (eventID, teamID, rowID int64) {
	t.Helper()
	var orgID int64
	must(t, db.QueryRow(`INSERT INTO organizations(name) VALUES ('fx') RETURNING id`).Scan(&orgID))
	if _, err := db.Exec(`INSERT INTO adventures(slug, title, description, author, language, org_id) VALUES ('fx', 'fx', '', '', 'en', $1)`, orgID); err != nil {
		t.Fatal(err)
	}
	must(t, db.QueryRow(`INSERT INTO events(adventure_slug, org_id, state, live_token, name, show_map) VALUES ('fx', $1, 'running', 'fx-tok', 'fx', 0) RETURNING id`, orgID).Scan(&eventID))
	must(t, db.QueryRow(`INSERT INTO teams(event_id, name, token) VALUES ($1, 'Owls', 'fx-team') RETURNING id`, eventID).Scan(&teamID))
	doc, _ := json.Marshal(hook)
	must(t, db.QueryRow(`INSERT INTO graph_effect_outbox (event_id, team_id, node, kind, payload) VALUES ($1, $2, 'bell', 'webhook', $3) RETURNING id`, eventID, teamID, doc).Scan(&rowID))
	return eventID, teamID, rowID
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestWebhookDelivered(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must2(io.ReadAll(r.Body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	eventID, teamID, id := queue(t, db, Webhook{URL: srv.URL + "/hook", Method: "PUT", Headers: []graph.Header{{Name: "Authorization", Value: "Bearer s3"}}})

	w := &Worker{DB: db, Client: webhooks.NewClient(), AllowLoopback: true}
	if sent, err := w.Once(context.Background()); !sent || err != nil {
		t.Fatalf("once = %v, %v", sent, err)
	}
	if got == nil || got.Method != "PUT" || got.URL.Path != "/hook" || got.Header.Get("Authorization") != "Bearer s3" || got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %+v", got)
	}
	var b Body
	if err := json.Unmarshal(body, &b); err != nil || b.EventID != eventID || b.TeamID != teamID || b.Team != "Owls" || b.Node != "bell" {
		t.Errorf("body = %s (%v)", body, err)
	}
	var delivered sql.NullTime
	must(t, db.QueryRow(`SELECT delivered_at FROM graph_effect_outbox WHERE id = $1`, id).Scan(&delivered))
	if !delivered.Valid {
		t.Error("a 2xx marks the row delivered")
	}
	if sent, _ := w.Once(context.Background()); sent {
		t.Error("a delivered webhook is not sent again")
	}
}

func TestWebhookGETQueryAndRetry(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, _, id := queue(t, db, Webhook{URL: srv.URL + "/?keep=1", Method: "GET"})

	w := &Worker{DB: db, Client: webhooks.NewClient(), AllowLoopback: true}
	if sent, err := w.Once(context.Background()); !sent || err != nil {
		t.Fatalf("once = %v, %v", sent, err)
	}
	if !contains(query, "keep=1") || !contains(query, "team=Owls") || !contains(query, "node=bell") {
		t.Errorf("GET query = %q", query)
	}
	var attempts int
	var status sql.NullInt64
	var later bool
	must(t, db.QueryRow(`SELECT attempts, last_status, next_attempt_at > now() + interval '30 seconds' FROM graph_effect_outbox WHERE id = $1`, id).Scan(&attempts, &status, &later))
	if attempts != 1 || status.Int64 != 502 || !later {
		t.Errorf("after a 502: attempts %d, status %v, retried later %v", attempts, status, later)
	}
	if sent, _ := w.Once(context.Background()); sent {
		t.Error("a failed webhook waits for its backoff")
	}
}

// Without the development switch, a webhook may not call the server's
// own machine: any author could otherwise reach its private services.
func TestWebhookRefusesLoopback(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	_, _, id := queue(t, db, Webhook{URL: srv.URL})

	w := &Worker{DB: db, Client: webhooks.NewClient()}
	if _, err := w.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	var why sql.NullString
	must(t, db.QueryRow(`SELECT last_error FROM graph_effect_outbox WHERE id = $1`, id).Scan(&why))
	if called || !why.Valid {
		t.Errorf("loopback called %v, recorded %q", called, why.String)
	}
}

func must2(b []byte, _ error) []byte { return b }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
