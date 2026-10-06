package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A pause that read 'running' must not overwrite a completion that
// commits before its UPDATE lands (activity.MaybeCompleteEvent racing
// the arranger).
func TestPauseEvent_DoesNotOverwriteCompletion(t *testing.T) {
	t.Parallel()
	db, _, assID, r := questFixture(t)

	body, _ := json.Marshal(map[string]any{
		"assignment_id": assID, "name": "Race", "attest_no_minors": true, "public_for_sale": true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// Starting a public_for_sale quest with no teams is the canonical
	// quest flow: the empty-team guard must yield since teams come
	// from /store ticket purchases AFTER the quest is running.
	base := "/api/admin/events/" + strconv.FormatInt(created.ID, 10)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", base+"/start", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("start: status=%d body=%s, want 204 (empty-team guard should be skipped for quests)",
			w.Code, w.Body.String())
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT 1 FROM events WHERE id = $1 FOR UPDATE`, created.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", base+"/pause", nil))
		done <- w
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
		`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pause never blocked on the event row")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := tx.Exec(`UPDATE events SET state = 'completed', completed_at = now_text() WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if w := <-done; w.Code != http.StatusConflict {
		t.Errorf("pause: status=%d body=%s, want 409", w.Code, w.Body.String())
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM events WHERE id = $1`, created.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "completed" {
		t.Errorf("state = %q, want completed", state)
	}
}
