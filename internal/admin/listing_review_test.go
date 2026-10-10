package admin_test

import (
	"bytes"
	"context"
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
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/public"
	"github.com/klahr/zonventure/internal/storage/dbblob"
)

// listingFixture is the party fixture's organisation, unverified, with a
// self-guided event to sell, a platform admin to review it — in the
// console, and through an API token as an AI assistant would — and the
// public store to read it from.
type listingFixture struct {
	*partyFixture
	reviewer   chi.Router
	assistant  chi.Router
	store      chi.Router
	eventID    int64
	adminEmail string
	// passkey signs the reviewer's console decisions.
	passkey *virtualPasskey
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
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, public_for_sale, graph_id)
		SELECT 'treasure', $1, 'running', 'lt-' || md5(random()::text), 'Treasure', TRUE, published_graph_id
		FROM adventures WHERE slug = 'treasure'
		RETURNING id`, f.orgID).Scan(&f.eventID); err != nil {
		t.Fatal(err)
	}

	f.console.Patch("/api/admin/events/{eventID}", f.h.UpdateEvent)
	f.console.Post("/api/admin/events/{eventID}/cover", f.h.SetEventCover)
	f.console.Post("/api/admin/events/{eventID}/listing/submit", f.h.SubmitEventListing)
	f.console.Get("/store-cover/event/{eventID}", f.h.ServeEventCover)

	routes := func(r chi.Router) {
		r.Get("/api/admin/listing-reviews", f.h.ListListingReviews)
		r.Get("/api/admin/listing-reviews/log", f.h.ListListingLog)
		r.Get("/api/admin/listing-reviews/log/verify", f.h.VerifyListingLog)
		r.Get("/api/admin/listing-reviews/{kind}/{id}", f.h.GetListingReview)
		r.Get("/api/admin/listing-reviews/{kind}/{id}/media/{ref}", f.h.GetListingReviewMedia)
		r.Post("/api/admin/listing-reviews/{kind}/{id}/sign", f.h.SignListingDecision)
		r.Post("/api/admin/listing-reviews/{kind}/{id}/approve", f.h.ApproveListing)
		r.Get("/api/admin/me/reviewer-passkeys", f.h.ListReviewerPasskeys)
		r.Post("/api/admin/me/reviewer-passkeys/begin", f.h.BeginReviewerPasskey)
		r.Post("/api/admin/me/reviewer-passkeys/finish", f.h.FinishReviewerPasskey)
		r.Post("/api/admin/listing-reviews/{kind}/{id}/reject", f.h.RejectListing)
		r.Post("/api/admin/organizations/{orgID}/verified-creator", f.h.SetOrgVerifiedCreator)
	}
	root := auth.User{ID: adminID, Username: "root", Role: auth.RoleAdmin, IsAdmin: true, ListingReviewer: true}
	f.reviewer = chi.NewRouter()
	f.reviewer.Use(injectUser(root))
	routes(f.reviewer)
	f.assistant = chi.NewRouter()
	f.assistant.Use(injectUser(root), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithAPIToken(r.Context())))
		})
	})
	routes(f.assistant)

	publicH := &public.Handlers{DB: db, Lib: adventure.New(db)}
	f.store = chi.NewRouter()
	f.store.Get("/api/o/{slug}/store/items", publicH.StoreListByOrg)
	f.store.Get("/store-cover/event/{eventID}", f.h.ServeEventCover)
	f.addPasskey(t)
	return f, cleanup
}

func (f *listingFixture) patchEvent(t *testing.T, body map[string]any) {
	t.Helper()
	if w := call(t, f.console, "PATCH", fmt.Sprintf("/api/admin/events/%d", f.eventID), body); w.Code != http.StatusNoContent {
		t.Fatalf("patch %v: %d %s", body, w.Code, w.Body)
	}
}

// submitEvent submits the event's listing, confirming its content.
func (f *listingFixture) submitEvent(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, f.console, "POST", fmt.Sprintf("/api/admin/events/%d/listing/submit", f.eventID), map[string]bool{"attest": true})
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
	AttestedBy string `json:"attested_by"`
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

// decide is the reviewer's decision in the console, signed with their
// passkey.
func (f *listingFixture) decide(t *testing.T, kind string, id int64, action, revision, reason string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, f.reviewer, "POST", fmt.Sprintf("/api/admin/listing-reviews/%s/%d/%s", kind, id, action),
		map[string]any{"revision": revision, "reason": reason, "signature": f.signed(t, kind, id, action, revision, reason)})
}

func (f *listingFixture) decideAs(t *testing.T, as http.Handler, kind string, id int64, action, revision, reason string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, as, "POST", fmt.Sprintf("/api/admin/listing-reviews/%s/%d/%s", kind, id, action),
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

func code(w *httptest.ResponseRecorder) string {
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

// An unverified organisation's listing is a draft until its creator
// submits it, confirming its content; it goes public once approved. An
// edit waits for review behind what was approved, and a rejection leaves
// that up.
func TestListingReview_Event(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Find it"})
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after first save = %q, want draft", got)
	}
	if q := f.queue(t); len(q) != 0 {
		t.Fatalf("queue holds an unsubmitted draft: %+v", q)
	}
	w := call(t, f.console, "POST", fmt.Sprintf("/api/admin/events/%d/listing/submit", f.eventID), map[string]bool{"attest": false})
	if w.Code != http.StatusBadRequest || code(w) != "attest_required" {
		t.Fatalf("submit without confirming: %d %s", w.Code, w.Body)
	}
	if w := f.submitEvent(t); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"pending"`) {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	if items := f.storeItems(t); len(items) != 0 {
		t.Fatalf("store lists an unreviewed listing: %+v", items)
	}
	if got := f.mailTo(f.adminEmail); len(got) != 1 || !strings.Contains(got[0], "waiting for review") {
		t.Fatalf("platform admin mail = %v, want one review notice", got)
	}

	q := f.queue(t)
	if len(q) != 1 || q[0].Kind != "event" || q[0].ID != f.eventID || q[0].Draft.PriceCents != 9900 || q[0].Live != nil || q[0].AttestedBy != "boss" {
		t.Fatalf("queue = %+v", q)
	}
	if w := f.decide(t, "event", f.eventID, "approve", "stale", ""); w.Code != http.StatusConflict || code(w) != "changed" {
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
	if w := f.submitEvent(t); w.Code != http.StatusConflict || code(w) != "unchanged" {
		t.Fatalf("submitting what is approved: %d %s", w.Code, w.Body)
	}

	// An edit waits; the approved price stays up.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 12900})
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after edit = %q, want draft", got)
	}
	if w := f.submitEvent(t); w.Code != http.StatusOK {
		t.Fatalf("submit edit: %d %s", w.Code, w.Body)
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
	if w := f.submitEvent(t); w.Code != http.StatusConflict || code(w) != "unchanged_since_rejected" {
		t.Fatalf("resubmitting what was rejected: %d %s", w.Code, w.Body)
	}

	// Undoing the edit needs no review.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	if got := f.eventState(t); got != "approved" {
		t.Fatalf("state after undoing the edit = %q, want approved", got)
	}

	// An edit made after submitting withdraws the submission: what the
	// reviewer saw is not what it would approve.
	f.patchEvent(t, map[string]any{"public_blurb": "Seen"})
	f.submitEvent(t)
	seen := f.queue(t)[0].Revision
	f.patchEvent(t, map[string]any{"public_blurb": "Not seen"})
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after editing a submitted draft = %q, want draft", got)
	}
	if w := f.decide(t, "event", f.eventID, "approve", seen, ""); w.Code != http.StatusConflict {
		t.Fatalf("approve after a later edit: %d %s", w.Code, w.Body)
	}
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", seen, ""); w.Code != http.StatusConflict || code(w) != "changed" {
		t.Fatalf("approve an older revision: %d %s", w.Code, w.Body)
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

// A verified creator's submissions are approved at once, and verifying
// an organisation approves what of theirs waits. Both are logged.
func TestListingReview_VerifiedCreator(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	f.submitEvent(t)
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
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("verified edit before submitting = %q, want draft", got)
	}
	if w := f.submitEvent(t); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"approved"`) {
		t.Fatalf("verified submit: %d %s", w.Code, w.Body)
	}
	if items := f.storeItems(t); items[0].PriceCents != 14900 {
		t.Fatalf("verified edit price = %d, want 14900", items[0].PriceCents)
	}
	if got := f.mailTo(f.adminEmail); len(got) != 1 {
		t.Fatalf("platform admin mail = %v, want only the first submission's", got)
	}
	log := f.log(t, "auto")
	if len(log) != 2 || log[0].Action != "approved" || log[1].Action != "approved" {
		t.Fatalf("automatic approvals logged = %+v", log)
	}
}

type logEntry struct {
	Action string `json:"action"`
	Via    string `json:"via"`
	By     string `json:"by"`
	Reason string `json:"reason"`
}

func (f *listingFixture) log(t *testing.T, via string) []logEntry {
	t.Helper()
	w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log?via="+via, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("log: %d %s", w.Code, w.Body)
	}
	var resp struct {
		Log []logEntry `json:"log"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Log
}

// A reviewer sees every text of the adventure version a listing plays,
// and decisions made through an API token are logged as an assistant's.
func TestListingReview_ContentAndAssistant(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Find it"})
	f.submitEvent(t)
	w := call(t, f.reviewer, "GET", fmt.Sprintf("/api/admin/listing-reviews/event/%d", f.eventID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("content: %d %s", w.Code, w.Body)
	}
	var c admin.ReviewContent
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	all := map[string]string{}
	for _, x := range c.Texts {
		all[x.Text] = x.Where
	}
	for _, want := range []string{"What grows on the oak?", "acorns", "The old oak", "Find it", "Acme", "Treasure Hunt"} {
		if _, ok := all[want]; !ok {
			t.Errorf("texts lack %q: %+v", want, c.Texts)
		}
	}
	if c.State != "pending" || c.AttestedBy != "boss" || c.GraphID == 0 || c.Version != 1 {
		t.Errorf("content header = state %q attested by %q graph %d v%d", c.State, c.AttestedBy, c.GraphID, c.Version)
	}

	if w := f.decideAs(t, f.assistant, "event", f.eventID, "reject", c.Revision, "Mentions a slur in step 2"); w.Code != http.StatusOK {
		t.Fatalf("assistant reject: %d %s", w.Code, w.Body)
	}
	log := f.log(t, "mcp")
	if len(log) != 1 || log[0].Action != "rejected" || log[0].By != "root" || log[0].Reason != "Mentions a slur in step 2" {
		t.Fatalf("assistant decisions logged = %+v", log)
	}
}

// A new cover waits for review behind the approved one: the public gets
// the approved cover, the organisation and the reviewer the new one.
func TestListingReview_Cover(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	f.submitEvent(t)
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
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after a new cover = %q, want draft", got)
	}
	f.submitEvent(t)
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
	if w := call(t, f.reviewer, "GET", q[0].Draft.CoverURL, nil); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("reviewer's cover: %d %s", w.Code, w.Header().Get("Content-Type"))
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

func (f *listingFixture) parties(t *testing.T) []admin.PublicPartyOfferDTO {
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

func (f *listingFixture) submitOffer(t *testing.T) {
	t.Helper()
	if w := call(t, f.console, "POST", "/api/admin/party-offers/treasure/submit", map[string]bool{"attest": true}); w.Code != http.StatusOK {
		t.Fatalf("submit offer: %d %s", w.Code, w.Body)
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"listing_review":"draft"`) {
		t.Fatalf("offer: %d %s", w.Code, w.Body)
	}
	f.submitOffer(t)
	if p := f.parties(t); len(p) != 0 {
		t.Fatalf("page lists an unreviewed offer: %+v", p)
	}
	q := f.queue(t)
	if len(q) != 1 || q[0].Kind != "party" {
		t.Fatalf("queue = %+v", q)
	}
	if w := f.decide(t, "party", q[0].ID, "approve", q[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if p := f.parties(t); len(p) != 1 || p[0].BaseCents != 19900 {
		t.Fatalf("page after approval = %+v", p)
	}

	offer["base_cents"] = 29900
	if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer); w.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	f.submitOffer(t)
	if p := f.parties(t); len(p) != 1 || p[0].BaseCents != 19900 {
		t.Fatalf("page during review = %+v, want the approved 19900", p)
	}

	// Turning it off takes it down at once, with nothing to review.
	offer["enabled"] = false
	if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer); w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if p := f.parties(t); len(p) != 0 {
		t.Fatalf("page after disabling = %+v", p)
	}
	if q := f.queue(t); len(q) != 0 {
		t.Fatalf("queue after disabling = %+v", q)
	}
}

// A booked party plays the adventure version its offer was approved
// with, not one published since that nobody reviewed.
func TestListingReview_PartyPlaysApprovedVersion(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()

	offer := map[string]any{"enabled": true, "base_cents": 19900, "per_kid_cents": 2900, "currency": "sek", "max_kids": 12, "blurb": "Find the treasure"}
	call(t, f.console, "PUT", "/api/admin/party-offers/treasure", offer)
	f.submitOffer(t)
	q := f.queue(t)
	if w := f.decide(t, "party", q[0].ID, "approve", q[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	var advID, approved int64
	if err := f.h.DB.QueryRow(`SELECT id, published_graph_id FROM adventures WHERE slug = 'treasure'`).Scan(&advID, &approved); err != nil {
		t.Fatal(err)
	}

	g := partyGraph()
	g.Nodes[1].PuzzleCfg.Blocks = append(graph.LegacyBlocks("Something nobody reviewed", nil), g.Nodes[1].PuzzleCfg.Blocks[1:]...)
	if _, _, err := graphstore.Publish(context.Background(), f.h.DB, advID, 0, g, ""); err != nil {
		t.Fatal(err)
	}
	if w := call(t, f.console, "GET", "/api/admin/party-offers", nil); !strings.Contains(w.Body.String(), `"newer_version":true`) {
		t.Errorf("offers list does not tell of the newer version: %s", w.Body)
	}

	f.book(t, 3)
	var base int64
	if err := f.h.DB.QueryRow(`SELECT base_graph_id FROM events WHERE party ORDER BY id DESC LIMIT 1`).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if base != approved {
		t.Fatalf("booked party plays graph %d, want the approved %d", base, approved)
	}
}

// Only users a platform admin made listing reviewers review, and no one
// reviews their own organisation's listings.
func TestListingReview_Reviewers(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	f.submitEvent(t)
	rev := f.queue(t)[0].Revision

	var bossID, rootID int64
	if err := f.h.DB.QueryRow(`SELECT id FROM users WHERE username = 'boss'`).Scan(&bossID); err != nil {
		t.Fatal(err)
	}
	if err := f.h.DB.QueryRow(`SELECT id FROM users WHERE username = 'root'`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	as := func(u auth.User) chi.Router {
		r := chi.NewRouter()
		r.Use(injectUser(u))
		r.Get("/api/admin/listing-reviews", f.h.ListListingReviews)
		r.Get("/api/admin/listing-reviews/{kind}/{id}", f.h.GetListingReview)
		r.Post("/api/admin/listing-reviews/{kind}/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
			if chi.URLParam(r, "action") == "approve" {
				f.h.ApproveListing(w, r)
			} else {
				f.h.RejectListing(w, r)
			}
		})
		return r
	}

	// A platform admin who isn't a reviewer can't.
	notReviewer := as(auth.User{ID: rootID, Username: "root", Role: auth.RoleAdmin, IsAdmin: true})
	if w := call(t, notReviewer, "GET", "/api/admin/listing-reviews", nil); w.Code != http.StatusForbidden || code(w) != "not_reviewer" {
		t.Fatalf("admin without the flag lists: %d %s", w.Code, w.Body)
	}
	if w := f.decideAs(t, notReviewer, "event", f.eventID, "approve", rev, ""); w.Code != http.StatusForbidden {
		t.Fatalf("admin without the flag approves: %d %s", w.Code, w.Body)
	}

	// A reviewer in the listing's organisation neither sees nor decides it.
	orgID := f.orgID
	insider := as(auth.User{ID: bossID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID, ListingReviewer: true})
	if w := call(t, insider, "GET", "/api/admin/listing-reviews", nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"kind":"event"`) {
		t.Fatalf("insider's queue: %d %s", w.Code, w.Body)
	}
	if w := call(t, insider, "GET", fmt.Sprintf("/api/admin/listing-reviews/event/%d", f.eventID), nil); w.Code != http.StatusForbidden || code(w) != "own_org" {
		t.Fatalf("insider reads its own listing: %d %s", w.Code, w.Body)
	}
	if w := f.decideAs(t, insider, "event", f.eventID, "approve", rev, ""); w.Code != http.StatusForbidden || code(w) != "own_org" {
		t.Fatalf("insider approves its own listing: %d %s", w.Code, w.Body)
	}
	if got := f.eventState(t); got != "pending" {
		t.Fatalf("state = %q, want still pending", got)
	}

	// A platform admin grants the flag.
	grant := chi.NewRouter()
	grant.Use(injectUser(auth.User{ID: rootID, Role: auth.RoleAdmin, IsAdmin: true}))
	grant.Post("/api/admin/listing-reviewers", f.h.SetListingReviewer)
	grant.Get("/api/admin/listing-reviewers", f.h.ListListingReviewers)
	if w := call(t, grant, "POST", "/api/admin/listing-reviewers", map[string]any{"username": "root", "reviewer": true}); w.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	if w := call(t, grant, "GET", "/api/admin/listing-reviewers", nil); !strings.Contains(w.Body.String(), `"username":"root"`) {
		t.Fatalf("reviewers: %s", w.Body)
	}
}

// The review log is a hash chain: each entry names the one before it and
// fingerprints what was reviewed. The database refuses edits, and an
// edit made anyway (with the guard turned off) shows when the chain is
// recomputed. The head is mailed out once until it moves.
func TestListingReview_HashChain(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	db := f.h.DB

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Find it"})
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "reject", f.queue(t)[0].Revision, "Too vague"); w.Code != http.StatusOK {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	f.patchEvent(t, map[string]any{"public_blurb": "Find the acorns"})
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", f.queue(t)[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}

	verify := func() (ok bool, entries int, head string, problems []string) {
		t.Helper()
		w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("verify: %d %s", w.Code, w.Body)
		}
		var rep struct {
			OK       bool   `json:"ok"`
			Entries  int    `json:"entries"`
			HeadHash string `json:"head_hash"`
			Problems []struct {
				Problem string `json:"problem"`
			} `json:"problems"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
			t.Fatal(err)
		}
		for _, p := range rep.Problems {
			problems = append(problems, p.Problem)
		}
		return rep.OK, rep.Entries, rep.HeadHash, problems
	}
	ok, entries, head, problems := verify()
	if !ok || entries != 4 || len(head) != 64 {
		t.Fatalf("verify = ok %v, %d entries, head %q, %v", ok, entries, head, problems)
	}

	// The approval fingerprints the blurb it approved.
	var snapshot string
	if err := db.QueryRow(`
		SELECT s.content::text FROM listing_review_log l JOIN listing_review_snapshots s ON s.content_hash = l.content_hash
		WHERE l.action = 'approved'`).Scan(&snapshot); err != nil || !strings.Contains(snapshot, "Find the acorns") || !strings.Contains(snapshot, "What grows on the oak?") {
		t.Fatalf("approved snapshot = %q (%v)", snapshot, err)
	}

	// Mailed once, and not again until it moves.
	f.h.AnchorListingLog(context.Background())
	f.h.AnchorListingLog(context.Background())
	if got := f.mailTo(f.adminEmail); len(got) != 3 || !strings.Contains(got[2], "fingerprint") {
		t.Fatalf("platform admin mail = %v, want two review notices and one fingerprint", got)
	}

	if _, err := db.Exec(`UPDATE listing_review_log SET reason = 'Fine' WHERE action = 'rejected'`); err == nil {
		t.Fatal("the log took an edit")
	}
	if _, err := db.Exec(`DELETE FROM listing_review_log WHERE action = 'rejected'`); err == nil {
		t.Fatal("the log took a delete")
	}

	// Someone with the database turns the guard off and rewrites history.
	if _, err := db.Exec(`
		ALTER TABLE listing_review_log DISABLE TRIGGER listing_review_log_append_only;
		UPDATE listing_review_log SET reason = 'Fine' WHERE action = 'rejected';
		UPDATE listing_review_snapshots SET content = jsonb_set(content, '{draft,blurb}', '"Something else"')
		WHERE content_hash = (SELECT content_hash FROM listing_review_log WHERE action = 'approved');
		ALTER TABLE listing_review_log ENABLE TRIGGER listing_review_log_append_only;`); err != nil {
		t.Fatal(err)
	}
	// The rejection's fields, and its signature, which was over the
	// reason as written; and the snapshot the submission and the
	// approval of that content share.
	ok, _, _, problems = verify()
	all := strings.Join(problems, "; ")
	if ok || len(problems) != 4 || strings.Count(all, "fields were changed") != 1 || strings.Count(all, "signature is over a different decision") != 1 ||
		strings.Count(all, "content it reviewed was changed") != 2 {
		t.Fatalf("verify after tampering = ok %v, %v", ok, problems)
	}

	// Removing an entry breaks the link to it.
	if _, err := db.Exec(`
		ALTER TABLE listing_review_log DISABLE TRIGGER listing_review_log_append_only;
		DELETE FROM listing_review_log WHERE id = (SELECT min(id) FROM listing_review_log);
		ALTER TABLE listing_review_log ENABLE TRIGGER listing_review_log_append_only;`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, problems = verify(); !strings.Contains(strings.Join(problems, "; "), "does not follow the entry before it") {
		t.Fatalf("verify after a removal = %v", problems)
	}
}

// A decision in the console is signed with the reviewer's passkey over
// exactly that decision; one unsigned, signed for another decision, or
// signed once already is refused. The log checks every signature again.
func TestListingReview_PasskeySigning(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	f.submitEvent(t)
	rev := f.queue(t)[0].Revision
	path := fmt.Sprintf("/api/admin/listing-reviews/event/%d/", f.eventID)

	if w := call(t, f.reviewer, "POST", path+"approve", map[string]any{"revision": rev}); w.Code != http.StatusBadRequest || code(w) != "signature_required" {
		t.Fatalf("unsigned: %d %s", w.Code, w.Body)
	}
	forReject := f.signed(t, "event", f.eventID, "reject", rev, "Nope")
	if w := call(t, f.reviewer, "POST", path+"approve", map[string]any{"revision": rev, "signature": forReject}); w.Code != http.StatusForbidden || code(w) != "bad_signature" {
		t.Fatalf("signed for another decision: %d %s", w.Code, w.Body)
	}
	sig := f.signed(t, "event", f.eventID, "approve", rev, "")
	if w := call(t, f.reviewer, "POST", path+"approve", map[string]any{"revision": rev, "signature": sig}); w.Code != http.StatusOK {
		t.Fatalf("signed: %d %s", w.Code, w.Body)
	}
	if got := f.eventState(t); got != "approved" {
		t.Fatalf("state = %q", got)
	}

	// Replaying the signature for the next decision fails.
	f.patchEvent(t, map[string]any{"ticket_price_cents": 12900})
	f.submitEvent(t)
	if w := call(t, f.reviewer, "POST", path+"approve", map[string]any{"revision": f.queue(t)[0].Revision, "signature": sig}); w.Code != http.StatusForbidden {
		t.Fatalf("replayed signature: %d %s", w.Code, w.Body)
	}

	w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
	var rep struct {
		OK       bool `json:"ok"`
		Signed   int  `json:"signed"`
		Unsigned int  `json:"unsigned"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil || !rep.OK || rep.Signed != 1 || rep.Unsigned != 0 {
		t.Fatalf("verify = %+v (%v) %s", rep, err, w.Body)
	}
	if w := call(t, f.reviewer, "GET", "/api/admin/me/reviewer-passkeys", nil); !strings.Contains(w.Body.String(), `"name":"Test key"`) || !strings.Contains(w.Body.String(), "last_used_at") {
		t.Fatalf("passkeys: %s", w.Body)
	}
}

// A saved adventure version and uploaded media can't be changed in the
// database; and when someone goes around that, the check compares what
// approved listings serve with what was approved and says what moved.
func TestListingReview_ServedMatchesApproved(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	db := f.h.DB

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Find it"})
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", f.queue(t)[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	var graphID int64
	if err := db.QueryRow(`SELECT graph_id FROM events WHERE id = $1`, f.eventID).Scan(&graphID); err != nil {
		t.Fatal(err)
	}

	// The database holds saved versions and media to append-only.
	if _, err := db.Exec(`UPDATE adventure_graphs SET graph = jsonb_set(graph, '{zones,0,name}', '"Somewhere else"') WHERE id = $1`, graphID); err == nil {
		t.Fatal("a saved version took an edit")
	}
	if _, err := db.Exec(`UPDATE adventure_graphs SET label = 'Launch' WHERE id = $1`, graphID); err != nil {
		t.Fatalf("labelling a version: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO adventure_media (adventure_id, kind, mime_type, size_bytes, sha256, content)
		SELECT id, 'image', 'image/png', 1, 'abc', '\x89'::bytea FROM adventures WHERE slug = 'treasure'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE adventure_media SET content = '\x00'::bytea WHERE sha256 = 'abc'`); err == nil {
		t.Fatal("uploaded media took an edit")
	}

	type report struct {
		OK     bool `json:"ok"`
		Served struct {
			Checked  int `json:"checked"`
			Matching int `json:"matching"`
			Findings []struct {
				ID       int64    `json:"id"`
				Severity string   `json:"severity"`
				Parts    []string `json:"parts"`
			} `json:"findings"`
		} `json:"served"`
	}
	check := func() report {
		t.Helper()
		w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
		var r report
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatalf("verify: %v %s", err, w.Body)
		}
		return r
	}
	if r := check(); !r.OK || r.Served.Checked != 1 || r.Served.Matching != 1 {
		t.Fatalf("check after approval = %+v", r)
	}

	// A platform admin renames the organisation: shown, not tampering.
	if _, err := db.Exec(`UPDATE organizations SET name = 'Acme AB' WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	r := check()
	if !r.OK || len(r.Served.Findings) != 1 || r.Served.Findings[0].Severity != "unreviewed" ||
		strings.Join(r.Served.Findings[0].Parts, ",") != "organisation name" {
		t.Fatalf("check after a rename = %+v", r)
	}
	// The page's tagline is held back for review: one written straight
	// to the database is tampering.
	if _, err := db.Exec(`UPDATE organizations SET tagline = 'New words' WHERE id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}

	// Someone with the database edits the approved version and price.
	for _, q := range []string{
		`ALTER TABLE adventure_graphs DISABLE TRIGGER adventure_graphs_frozen`,
		`UPDATE adventure_graphs SET graph = jsonb_set(graph, '{zones,0,name}', '"Somewhere else"') WHERE id = ` + fmt.Sprint(graphID),
		`ALTER TABLE adventure_graphs ENABLE TRIGGER adventure_graphs_frozen`,
		`UPDATE event_listings SET price_cents = 100 WHERE event_id = ` + fmt.Sprint(f.eventID),
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	r = check()
	if r.OK || len(r.Served.Findings) != 1 || r.Served.Findings[0].Severity != "changed" ||
		strings.Join(r.Served.Findings[0].Parts, ",") != "adventure content,listing,organisation page,organisation name" {
		t.Fatalf("check after tampering = %+v", r)
	}

	// The day's mail says so.
	f.h.AnchorListingLog(context.Background())
	if got := f.mailTo(f.adminEmail); !strings.Contains(got[len(got)-1], "1 problems") {
		t.Fatalf("platform admin mail = %v", got)
	}
}
