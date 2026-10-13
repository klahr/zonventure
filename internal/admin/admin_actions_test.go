package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
)

// Void and undo it: each successful change lands in admin_actions with
// who and which route; a failed call and a read do not.
func TestAdminActions_VoidUnvoidLogged(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var orgID, adminID, purchaseID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('root', 1, TRUE) RETURNING id`).Scan(&adminID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure', 'Acme', $1, 1)`, orgID)
	must(err)
	must(db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state)
		VALUES ($1, 'treasure', 30, 90000, 'sek', 'invoice', 'paid') RETURNING id`, orgID).Scan(&purchaseID))

	h := &admin.Handlers{DB: db}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: adminID, Username: "root", Role: auth.RoleAdmin}))
	r.Use(admin.LogAdminActions(db))
	r.Get("/api/admin/admin-actions", h.ListAdminActions)
	r.Post("/api/admin/purchases/{purchaseID}/void", h.VoidPurchase)
	r.Post("/api/admin/purchases/{purchaseID}/unvoid", h.UnvoidPurchase)
	call := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	id := strconv.FormatInt(purchaseID, 10)

	if rec := call(http.MethodPost, "/api/admin/purchases/"+id+"/void"); rec.Code != http.StatusOK {
		t.Fatalf("void: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPost, "/api/admin/purchases/"+id+"/unvoid"); rec.Code != http.StatusOK {
		t.Fatalf("unvoid: %d %s", rec.Code, rec.Body)
	}
	var voided *string
	must(db.QueryRow(`SELECT voided_at FROM purchases WHERE id = $1`, purchaseID).Scan(&voided))
	if voided != nil {
		t.Fatalf("voided_at after unvoid = %q, want NULL", *voided)
	}
	if rec := call(http.MethodPost, "/api/admin/purchases/999999/unvoid"); rec.Code != http.StatusNotFound {
		t.Fatalf("unvoid missing: %d", rec.Code)
	}

	rec := call(http.MethodGet, "/api/admin/admin-actions")
	var got struct {
		Actions []struct {
			Username string `json:"username"`
			Action   string `json:"action"`
			Target   string `json:"target"`
		} `json:"actions"`
	}
	must(json.Unmarshal(rec.Body.Bytes(), &got))
	if len(got.Actions) != 2 {
		t.Fatalf("actions = %+v, want void and unvoid only", got.Actions)
	}
	if a := got.Actions[0]; a.Username != "root" || a.Action != "POST /api/admin/purchases/{purchaseID}/unvoid" || a.Target != "/api/admin/purchases/"+id+"/unvoid" {
		t.Errorf("newest action = %+v", a)
	}
	if a := got.Actions[1]; a.Action != "POST /api/admin/purchases/{purchaseID}/void" {
		t.Errorf("older action = %+v", a)
	}
}

// Unmarking billed reads differently from marking it, and the admin's
// own-account routes (/me) stay out of the log.
func TestAdminActions_UnbilledNotedMeSkipped(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var orgID, adminID, purchaseID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('root', 1, TRUE) RETURNING id`).Scan(&adminID))
	_, err := db.Exec(`INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure', 'Acme', $1, 1)`, orgID)
	must(err)
	must(db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state)
		VALUES ($1, 'treasure', 30, 90000, 'sek', 'invoice', 'paid') RETURNING id`, orgID).Scan(&purchaseID))

	h := &admin.Handlers{DB: db}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: adminID, Username: "root", Role: auth.RoleAdmin}))
	r.Use(admin.LogAdminActions(db))
	r.Post("/api/admin/purchases/{purchaseID}/billed", h.SetPurchaseBilled)
	r.Put("/api/admin/me/language", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	call := func(method, path, body string) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code >= 400 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
	}
	id := strconv.FormatInt(purchaseID, 10)
	call(http.MethodPost, "/api/admin/purchases/"+id+"/billed", `{"billed":true}`)
	call(http.MethodPost, "/api/admin/purchases/"+id+"/billed", `{"billed":false}`)
	call(http.MethodPut, "/api/admin/me/language", `{}`)

	var actions []string
	rows, err := db.Query(`SELECT action FROM admin_actions ORDER BY id`)
	must(err)
	defer rows.Close()
	for rows.Next() {
		var a string
		must(rows.Scan(&a))
		actions = append(actions, a)
	}
	want := []string{"POST /api/admin/purchases/{purchaseID}/billed", "POST /api/admin/purchases/{purchaseID}/billed (unbilled)"}
	if strings.Join(actions, "|") != strings.Join(want, "|") {
		t.Errorf("actions = %q, want %q", actions, want)
	}
}

type simulatedStub struct{ stubProvider }

func (s *simulatedStub) Name() string { return "simulated" }

// A purchase paid through the simulated provider has no charge to
// refund, yet Refund marks it refunded at once, as a Stripe refund
// ends up after its webhook.
func TestRefundPurchase_Simulated(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	hash, err := auth.HashPassword("hunter2")
	must(err)
	var orgID, adminID, purchaseID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin, password_hash) VALUES ('root', 1, TRUE, $1) RETURNING id`, hash).Scan(&adminID))
	_, err = db.Exec(`INSERT INTO adventures(slug, title, author, org_id, published) VALUES ('treasure', 'Treasure', 'Acme', $1, 1)`, orgID)
	must(err)
	must(db.QueryRow(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state)
		VALUES ($1, 'treasure', 20, 5000, 'sek', 'simulated', 'paid') RETURNING id`, orgID).Scan(&purchaseID))

	stub := &simulatedStub{}
	h := &admin.Handlers{DB: db, Provider: stub, PublicURL: "http://localhost"}
	r := chi.NewRouter()
	r.Use(injectUser(auth.User{ID: adminID, Username: "root", Role: auth.RoleAdmin}))
	r.Post("/api/admin/purchases/{purchaseID}/refund", h.RefundPurchase)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/purchases/"+strconv.FormatInt(purchaseID, 10)+"/refund", nil)
	req.Header.Set("X-Confirm-Password", "hunter2")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("refund: %d %s", rec.Code, rec.Body)
	}
	var state string
	must(db.QueryRow(`SELECT state FROM purchases WHERE id = $1`, purchaseID).Scan(&state))
	if state != "refunded" {
		t.Fatalf("state = %q, want refunded", state)
	}
	if stub.refunded {
		t.Error("simulated refund should not call the provider")
	}
}

// A row names what its route's ids named and which way a switch went,
// a deleted organisation included; the User filter keeps one admin's.
// Mounted under /api/admin as in production, so the route ids are
// found through the subrouter.
func TestAdminActions_TargetNameNoteAndUserFilter(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var orgID, goneID, adminID int64
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`).Scan(&orgID))
	must(db.QueryRow(`INSERT INTO organizations(name) VALUES ('Gone Ltd') RETURNING id`).Scan(&goneID))
	must(db.QueryRow(`INSERT INTO users(username, email_verified, is_admin) VALUES ('root', 1, TRUE) RETURNING id`).Scan(&adminID))
	_, err := db.Exec(`INSERT INTO admin_actions (username, action, target) VALUES ('other', 'POST /api/admin/organizations', '/api/admin/organizations')`)
	must(err)

	h := &admin.Handlers{DB: db}
	r := chi.NewRouter()
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(injectUser(auth.User{ID: adminID, Username: "root", Role: auth.RoleAdmin}))
		r.Use(admin.LogAdminActions(db))
		r.Get("/admin-actions", h.ListAdminActions)
		r.Group(func(r chi.Router) {
			r.Post("/organizations/{orgID}/trusted", h.SetOrgTrusted)
			r.Delete("/organizations/{orgID}", func(w http.ResponseWriter, r *http.Request) {
				_, _ = db.Exec(`DELETE FROM organizations WHERE id = $1`, chi.URLParam(r, "orgID"))
				w.WriteHeader(http.StatusNoContent)
			})
		})
	})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code >= 400 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		return rec
	}
	call(http.MethodPost, "/api/admin/organizations/"+strconv.FormatInt(orgID, 10)+"/trusted", `{"trusted":false}`)
	call(http.MethodDelete, "/api/admin/organizations/"+strconv.FormatInt(goneID, 10), "")

	var got struct {
		Actions []struct {
			Username   string `json:"username"`
			Action     string `json:"action"`
			TargetName string `json:"target_name"`
		} `json:"actions"`
	}
	must(json.Unmarshal(call(http.MethodGet, "/api/admin/admin-actions?username=RO", "").Body.Bytes(), &got))
	if len(got.Actions) != 2 {
		t.Fatalf("actions = %+v, want root's two", got.Actions)
	}
	if a := got.Actions[0]; a.Action != "DELETE /api/admin/organizations/{orgID}" || a.TargetName != "Gone Ltd" {
		t.Errorf("delete row = %+v", a)
	}
	if a := got.Actions[1]; a.Action != "POST /api/admin/organizations/{orgID}/trusted (off)" || a.TargetName != "Acme" {
		t.Errorf("trusted row = %+v", a)
	}
	got.Actions = nil
	must(json.Unmarshal(call(http.MethodGet, "/api/admin/admin-actions", "").Body.Bytes(), &got))
	if len(got.Actions) != 3 {
		t.Errorf("unfiltered = %d rows, want 3", len(got.Actions))
	}
}
