package admin_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/ws"
)

// questFixture returns a router pre-wired with the events handlers
// and a freshly-seeded org + adventure + unused assignment. Each
// caller gets its own DB and unused assignment so tests don't
// step on each other.
func questFixture(t *testing.T) (db *sql.DB, orgID, assID int64, router chi.Router) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)

	if err := db.QueryRow(
		`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1)
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		userID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventures(slug, title, author, org_id, published)
		VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1)
	`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, 'treasure', 50)
		RETURNING id
	`, orgID).Scan(&assID); err != nil {
		t.Fatal(err)
	}

	user := auth.User{
		ID: userID, Role: auth.RoleOrgAdmin, OrgID: &orgID,
		Email: "boss@acme.com", EmailVerified: true,
	}
	h := &admin.Handlers{DB: db, Lib: adventure.New(db), Hub: ws.NewHub()}
	r := chi.NewRouter()
	r.Use(injectUser(user))
	r.Get("/api/admin/events", h.ListEvents)
	r.Post("/api/admin/events", h.CreateEvent)
	r.Patch("/api/admin/events/{eventID}", h.UpdateEvent)
	r.Post("/api/admin/events/{eventID}/start", h.StartEvent)
	return db, orgID, assID, r
}

// TestQuest_CreateAndStartEmpty exercises the canonical quest flow:
// CreateEvent with public_for_sale=true, then StartEvent with no
// teams. The empty-team guard must yield since teams come from
// /store ticket purchases AFTER the quest is running.
func TestQuest_CreateAndStartEmpty(t *testing.T) {
	_, _, assID, r := questFixture(t)

	createBody, _ := json.Marshal(map[string]any{
		"assignment_id":    assID,
		"name":             "Treasure Hunt — always-on",
		"attest_no_minors": true,
		"public_for_sale":  true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(createBody)))
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

	w = httptest.NewRecorder()
	r.ServeHTTP(w,
		httptest.NewRequest("POST", "/api/admin/events/"+strconv.FormatInt(created.ID, 10)+"/start", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("start: status=%d body=%s, want 204 (empty-team guard should be skipped for quests)",
			w.Code, w.Body.String())
	}
}

// TestQuest_CreateWithoutAssignment locks in the slug-only path:
// quests bill per ticket via /store, so an org-owned adventure
// is enough access — no pre-purchased assignment required. The
// regression this guards against is the old "Buy slots from the
// Events page first" 409 that bounced quest creation.
func TestQuest_CreateWithoutAssignment(t *testing.T) {
	_, _, _, r := questFixture(t)

	createBody, _ := json.Marshal(map[string]any{
		"adventure_slug":   "treasure",
		"name":             "Treasure Hunt — always-on",
		"attest_no_minors": true,
		"public_for_sale":  true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(createBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s, want 201 (quest on org-owned adventure should not need an assignment)",
			w.Code, w.Body.String())
	}
}

// TestEvent_SlugOnlyOwnedStillBlocked is the inverse: a regular
// event on an org-owned adventure must still hit the buy-an-
// assignment 409 — the relaxation is conditioned on public_for_sale,
// not adventure ownership.
func TestEvent_SlugOnlyOwnedStillBlocked(t *testing.T) {
	_, _, _, r := questFixture(t)

	createBody, _ := json.Marshal(map[string]any{
		"adventure_slug":   "treasure",
		"name":             "Sunday hunt",
		"attest_no_minors": true,
		// public_for_sale omitted → false → regular event
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(createBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("create: status=%d body=%s, want 409 (regular events still need an assignment)",
			w.Code, w.Body.String())
	}
}

// TestEvent_StartEmptyStillBlocked is the inverse: a regular event
// with no teams must still hit the empty-team guard. Locks in
// that the guard relaxation is conditioned on public_for_sale,
// not unconditional.
func TestEvent_StartEmptyStillBlocked(t *testing.T) {
	_, _, assID, r := questFixture(t)

	createBody, _ := json.Marshal(map[string]any{
		"assignment_id":    assID,
		"name":             "Sunday hunt",
		"attest_no_minors": true,
		// public_for_sale omitted → false → regular event
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(createBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)

	w = httptest.NewRecorder()
	r.ServeHTTP(w,
		httptest.NewRequest("POST", "/api/admin/events/"+strconv.FormatInt(created.ID, 10)+"/start", nil))
	if w.Code != http.StatusConflict {
		t.Errorf("start: status=%d body=%s, want 409 (regular events still need at least one team)",
			w.Code, w.Body.String())
	}
}

// TestUpdateEvent_RefusesPublicForSaleFlip locks in the immutable-
// type-marker rule: once an event is created with a given
// public_for_sale value, UpdateEvent refuses to flip it. Without
// this an arranger could silently turn a regular event into a
// quest mid-flight (or vice versa), which the /store listing and
// the public-store-fields validation logic don't expect.
func TestUpdateEvent_RefusesPublicForSaleFlip(t *testing.T) {
	_, _, assID, r := questFixture(t)

	createBody, _ := json.Marshal(map[string]any{
		"assignment_id":    assID,
		"name":             "Regular event",
		"attest_no_minors": true,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/events", strings.NewReader(string(createBody)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)

	flipBody, _ := json.Marshal(map[string]any{"public_for_sale": true})
	w = httptest.NewRecorder()
	patchReq := httptest.NewRequest("PATCH",
		"/api/admin/events/"+strconv.FormatInt(created.ID, 10),
		strings.NewReader(string(flipBody)))
	patchReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, patchReq)
	if w.Code != http.StatusConflict {
		t.Errorf("flip: status=%d body=%s, want 409 (public_for_sale is immutable)",
			w.Code, w.Body.String())
	}

	// Same value (idempotent no-op) must NOT error — the handler
	// would otherwise reject any PATCH that includes the field
	// unchanged, breaking partial-update use cases.
	sameBody, _ := json.Marshal(map[string]any{"public_for_sale": false, "name": "Renamed event"})
	w = httptest.NewRecorder()
	sameReq := httptest.NewRequest("PATCH",
		"/api/admin/events/"+strconv.FormatInt(created.ID, 10),
		strings.NewReader(string(sameBody)))
	sameReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, sameReq)
	if w.Code != http.StatusNoContent {
		t.Errorf("same-value patch: status=%d body=%s, want 204 (idempotent no-op)",
			w.Code, w.Body.String())
	}
}

// TestListEvents_PublicForSaleFilter partitions the events list
// into quests and non-quests via the ?public_for_sale=true|false
// query param. Without the partition the Quests page would mix
// in regular events.
func TestListEvents_PublicForSaleFilter(t *testing.T) {
	db, orgID, _, r := questFixture(t)

	// Insert two events directly so we don't have to mint a second
	// assignment — the filter behaviour is what's under test, not
	// the create-flow's assignment validation.
	// `created_by_user_id` is a real FK to users.id post-migration —
	// look up the seeded boss instead of stuffing in a literal 0
	// (which violates the FK).
	if _, err := db.Exec(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token, name, public_for_sale, attested_no_minors_at)
		VALUES
		  ('treasure', $1, (SELECT id FROM users WHERE username = 'boss'), 'scheduled', 'tok-quest', 'My Quest', TRUE, now_text()),
		  ('treasure', $1, (SELECT id FROM users WHERE username = 'boss'), 'scheduled', 'tok-event', 'My Event', FALSE, now_text())
	`, orgID); err != nil {
		t.Fatal(err)
	}

	check := func(filter string, wantNames ...string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/events?"+filter, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("filter=%q: status=%d body=%s", filter, w.Code, w.Body.String())
		}
		var resp struct {
			Events []struct {
				Name          string `json:"name"`
				PublicForSale bool   `json:"public_for_sale"`
			} `json:"events"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, e := range resp.Events {
			got[e.Name] = e.PublicForSale
		}
		if len(got) != len(wantNames) {
			t.Errorf("filter=%q: got %d events (%v), want %d (%v)",
				filter, len(got), got, len(wantNames), wantNames)
		}
		for _, n := range wantNames {
			if _, ok := got[n]; !ok {
				t.Errorf("filter=%q: missing %q in result %v", filter, n, got)
			}
		}
	}
	check("public_for_sale=true", "My Quest")
	check("public_for_sale=false", "My Event")
	check("", "My Quest", "My Event") // unfiltered = both
}
