package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/public"
	"github.com/klahr/zonventure/internal/storage/dbblob"
)

// listingFixture is the party fixture's organisation, unverified, with a
// self-guided event to sell, a platform admin to review it and the
// public store to read it from.
type listingFixture struct {
	*partyFixture
	reviewer   chi.Router
	store      chi.Router
	eventID    int64
	adminEmail string
}

func newListingFixture(t *testing.T) (*listingFixture, func()) {
	t.Helper()
	pf, cleanup := newPartyFixture(t)
	f := &listingFixture{partyFixture: pf, adminEmail: "root@zv.example.com"}
	db := f.h.DB
	f.h.Storage = dbblob.New(db)
	if _, err := db.Exec(`UPDATE organizations SET verified_creator = FALSE WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO org_memberships(user_id, org_id, role)
		SELECT id, $1, 'org_admin' FROM users WHERE username = 'boss'`, f.orgID); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	if err := db.QueryRow(`
		INSERT INTO users(username, email, email_verified, is_admin) VALUES ('root', $1, 1, TRUE) RETURNING id`,
		f.adminEmail).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, public_for_sale)
		VALUES ('treasure', $1, 'running', 'lt-' || md5(random()::text), 'Treasure', TRUE)
		RETURNING id`, f.orgID).Scan(&f.eventID); err != nil {
		t.Fatal(err)
	}

	f.console.Patch("/api/admin/events/{eventID}", f.h.UpdateEvent)
	f.console.Post("/api/admin/events/{eventID}/cover", f.h.SetEventCover)
	f.console.Get("/store-cover/event/{eventID}", f.h.ServeEventCover)

	f.reviewer = chi.NewRouter()
	f.reviewer.Use(injectUser(auth.User{ID: adminID, Role: auth.RoleAdmin, IsAdmin: true}))
	f.reviewer.Get("/api/admin/listing-reviews", f.h.ListListingReviews)
	f.reviewer.Post("/api/admin/listing-reviews/{kind}/{id}/approve", f.h.ApproveListing)
	f.reviewer.Post("/api/admin/listing-reviews/{kind}/{id}/reject", f.h.RejectListing)
	f.reviewer.Post("/api/admin/organizations/{orgID}/verified-creator", f.h.SetOrgVerifiedCreator)

	publicH := &public.Handlers{DB: db, Lib: adventure.New(db)}
	f.store = chi.NewRouter()
	f.store.Get("/api/o/{slug}/store/items", publicH.StoreListByOrg)
	f.store.Get("/store-cover/event/{eventID}", f.h.ServeEventCover)
	return f, cleanup
}

func (f *listingFixture) patchEvent(t *testing.T, body map[string]any) {
	t.Helper()
	if w := call(t, f.console, "PATCH", fmt.Sprintf("/api/admin/events/%d", f.eventID), body); w.Code != http.StatusNoContent {
		t.Fatalf("patch %v: %d %s", body, w.Code, w.Body)
	}
}

// storeItems is what the public store lists.
func (f *listingFixture) storeItems(t *testing.T) []public.StoreItemDTO {
	t.Helper()
	w := call(t, f.store, "GET", "/api/o/acme/store/items", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("store: %d %s", w.Code, w.Body)
	}
	var resp struct {
		Items []public.StoreItemDTO `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Items
}

type pendingListing struct {
	Kind     string `json:"kind"`
	ID       int64  `json:"id"`
	Revision string `json:"revision"`
	Draft    struct {
		Blurb      string `json:"blurb"`
		PriceCents int    `json:"price_cents"`
		CoverURL   string `json:"cover_url"`
	} `json:"draft"`
	Live *struct {
		PriceCents int `json:"price_cents"`
	} `json:"live"`
}

func (f *listingFixture) queue(t *testing.T) []pendingListing {
	t.Helper()
	w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("queue: %d %s", w.Code, w.Body)
	}
	var resp struct {
		Reviews []pendingListing `json:"reviews"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Reviews
}

func (f *listingFixture) decide(t *testing.T, kind string, id int64, action, revision, reason string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, f.reviewer, "POST", fmt.Sprintf("/api/admin/listing-reviews/%s/%d/%s", kind, id, action),
		map[string]string{"revision": revision, "reason": reason})
}

func (f *listingFixture) eventState(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.h.DB.QueryRow(`SELECT COALESCE(listing_review, '') FROM events WHERE id = $1`, f.eventID).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// mailTo is the subjects of the mail sent to addr.
func (f *listingFixture) mailTo(addr string) []string {
	f.mail.mu.Lock()
	defer f.mail.mu.Unlock()
	var out []string
	for _, m := range f.mail.messages {
		for _, to := range m.To {
			if to == addr {
				out = append(out, m.Subject)
			}
		}
	}
	return out
}

// An unverified organisation's listing goes public only once approved;
// an edit to it waits for review behind what was approved, and a
// rejection leaves that up.
func TestListingReview_Event(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Find it"})
	if got := f.eventState(t); got != "pending" {
		t.Fatalf("state after first save = %q, want pending", got)
	}
	if items := f.storeItems(t); len(items) != 0 {
		t.Fatalf("store lists an unreviewed listing: %+v", items)
	}
	if got := f.mailTo(f.adminEmail); len(got) != 1 || !strings.Contains(got[0], "waiting for review") {
		t.Fatalf("platform admin mail = %v, want one review notice", got)
	}

	q := f.queue(t)
	if len(q) != 1 || q[0].Kind != "event" || q[0].ID != f.eventID || q[0].Draft.PriceCents != 9900 || q[0].Live != nil {
		t.Fatalf("queue = %+v", q)
	}
	if w := f.decide(t, "event", f.eventID, "approve", "stale", ""); w.Code != http.StatusConflict {
		t.Fatalf("approve with a stale revision: %d %s", w.Code, w.Body)
	}
	if w := f.decide(t, "event", f.eventID, "approve", q[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	items := f.storeItems(t)
	if len(items) != 1 || items[0].PriceCents != 9900 || items[0].Blurb != "Find it" || items[0].OrgVerified {
		t.Fatalf("store after approval = %+v", items)
	}
	if got := f.mailTo("boss@acme.com"); len(got) != 1 || !strings.Contains(got[0], "approved") {
		t.Fatalf("org admin mail = %v, want an approval", got)
	}

	// An edit waits; the approved price stays up.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 12900})
	if got := f.eventState(t); got != "pending" {
		t.Fatalf("state after edit = %q, want pending", got)
	}
	if items := f.storeItems(t); len(items) != 1 || items[0].PriceCents != 9900 {
		t.Fatalf("store during review = %+v, want the approved 9900", items)
	}
	q = f.queue(t)
	if len(q) != 1 || q[0].Live == nil || q[0].Live.PriceCents != 9900 || q[0].Draft.PriceCents != 12900 {
		t.Fatalf("queue for an edit = %+v", q)
	}
	if w := f.decide(t, "event", f.eventID, "reject", q[0].Revision, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("reject without a reason: %d", w.Code)
	}
	if w := f.decide(t, "event", f.eventID, "reject", q[0].Revision, "Price looks wrong"); w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	if got := f.eventState(t); got != "rejected" {
		t.Fatalf("state after reject = %q", got)
	}
	if items := f.storeItems(t); len(items) != 1 || items[0].PriceCents != 9900 {
		t.Fatalf("store after reject = %+v, want the approved 9900", items)
	}
	if got := f.mailTo("boss@acme.com"); len(got) != 2 || !strings.Contains(got[1], "not approved") {
		t.Fatalf("org admin mail = %v, want a rejection", got)
	}
	if w := f.decide(t, "event", f.eventID, "approve", q[0].Revision, ""); w.Code != http.StatusConflict {
		t.Fatalf("approving a decided listing: %d", w.Code)
	}

	// Undoing the edit needs no review.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	if got := f.eventState(t); got != "approved" {
		t.Fatalf("state after undoing the edit = %q, want approved", got)
	}
	if q := f.queue(t); len(q) != 0 {
		t.Fatalf("queue after undo = %+v", q)
	}

	// An edit made while the reviewer looked can't be approved unseen.
	f.patchEvent(t, map[string]any{"public_blurb": "Seen"})
	seen := f.queue(t)[0].Revision
	f.patchEvent(t, map[string]any{"public_blurb": "Not seen"})
	if w := f.decide(t, "event", f.eventID, "approve", seen, ""); w.Code != http.StatusConflict {
		t.Fatalf("approve after a later edit: %d %s", w.Code, w.Body)
	}
	if items := f.storeItems(t); items[0].Blurb != "Find it" {
		t.Fatalf("blurb = %q, want the approved one", items[0].Blurb)
	}

	// Clearing the price takes it down at once.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 0})
	if items := f.storeItems(t); len(items) != 0 {
		t.Fatalf("store after clearing the price = %+v", items)
	}
}

// A verified creator's listings go up as saved, and verifying an
// organisation approves what of theirs waits.
func TestListingReview_VerifiedCreator(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	if len(f.storeItems(t)) != 0 {
		t.Fatal("listed before review")
	}
	if w := call(t, f.reviewer, "POST", fmt.Sprintf("/api/admin/organizations/%d/verified-creator", f.orgID),
		map[string]bool{"verified": true}); w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	items := f.storeItems(t)
	if len(items) != 1 || items[0].PriceCents != 9900 || !items[0].OrgVerified {
		t.Fatalf("store after verifying = %+v", items)
	}
	f.patchEvent(t, map[string]any{"ticket_price_cents": 14900})
	if got := f.eventState(t); got != "approved" {
		t.Fatalf("verified edit state = %q, want approved", got)
	}
	if items := f.storeItems(t); items[0].PriceCents != 14900 {
		t.Fatalf("verified edit price = %d, want 14900", items[0].PriceCents)
	}
	if q := f.queue(t); len(q) != 0 {
		t.Fatalf("queue = %+v, want none", q)
	}
	if got := f.mailTo(f.adminEmail); len(got) != 1 {
		t.Fatalf("platform admin mail = %v, want only the first submission's", got)
	}
}

// A new cover waits for review behind the approved one: the public gets
// the approved cover, the organisation and the reviewer the new one.
func TestListingReview_Cover(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	if w := f.decide(t, "event", f.eventID, "approve", f.queue(t)[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="cover.png"`)
	hdr.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hdr)
	_ = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	_ = mw.Close()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/admin/events/%d/cover", f.eventID), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	f.console.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cover upload: %d %s", w.Code, w.Body)
	}
	if got := f.eventState(t); got != "pending" {
		t.Fatalf("state after a new cover = %q, want pending", got)
	}
	coverPath := fmt.Sprintf("/store-cover/event/%d", f.eventID)
	if w := call(t, f.store, "GET", coverPath, nil); w.Code != http.StatusNotFound {
		t.Fatalf("public cover before review: %d, want 404", w.Code)
	}
	if w := call(t, f.store, "GET", coverPath+"?draft=1", nil); w.Code != http.StatusNotFound {
		t.Fatalf("draft cover without a session: %d, want 404", w.Code)
	}
	if w := call(t, f.console, "GET", coverPath+"?draft=1", nil); w.Code != http.StatusOK {
		t.Fatalf("draft cover for the org: %d", w.Code)
	}
	q := f.queue(t)
	if len(q) != 1 || q[0].Draft.CoverURL == "" {
		t.Fatalf("queue = %+v, want the new cover", q)
	}
	if w := f.decide(t, "event", f.eventID, "approve", q[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve cover: %d %s", w.Code, w.Body)
	}
	if w := call(t, f.store, "GET", coverPath, nil); w.Code != http.StatusOK {
		t.Fatalf("public cover after review: %d", w.Code)
	}
	if items := f.storeItems(t); items[0].CoverImageURL != coverPath {
		t.Fatalf("store cover = %q", items[0].CoverImageURL)
	}
}

// A party offer goes on the page once approved; an edit shows, and is
// booked at, the approved price until it is approved in turn.
func TestListingReview_PartyOffer(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	offer := map[string]any{"enabled": true, "base_cents": 19900, "per_kid_cents": 2900, "currency": "sek", "max_kids": 12, "blurb": "Find the treasure"}
	w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"listing_review":"pending"`) {
		t.Fatalf("offer: %d %s", w.Code, w.Body)
	}
	parties := func() []admin.PublicPartyOfferDTO {
		t.Helper()
		w := call(t, f.public, "GET", "/api/o/acme/parties", nil)
		var resp struct {
			Parties []admin.PublicPartyOfferDTO `json:"parties"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Parties
	}
	if p := parties(); len(p) != 0 {
		t.Fatalf("page lists an unreviewed offer: %+v", p)
	}
	q := f.queue(t)
	if len(q) != 1 || q[0].Kind != "party" {
		t.Fatalf("queue = %+v", q)
	}
	if w := f.decide(t, "party", q[0].ID, "approve", q[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if p := parties(); len(p) != 1 || p[0].BaseCents != 19900 {
		t.Fatalf("page after approval = %+v", p)
	}

	offer["base_cents"] = 29900
	if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer); w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if p := parties(); len(p) != 1 || p[0].BaseCents != 19900 {
		t.Fatalf("page during review = %+v, want the approved 19900", p)
	}

	// Turning it off takes it down at once, with nothing to review.
	offer["enabled"] = false
	if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if p := parties(); len(p) != 0 {
		t.Fatalf("page after disabling = %+v", p)
	}
	if q := f.queue(t); len(q) != 0 {
		t.Fatalf("queue after disabling = %+v", q)
	}
}
