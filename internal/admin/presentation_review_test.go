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

	"github.com/klahr/zonventure/internal/eventfile"
)

// presentationFixture is a listing fixture whose event is approved for
// sale, so the organisation's page and the adventure's presentation are
// held back for review.
func newPresentationFixture(t *testing.T) (*listingFixture, func()) {
	t.Helper()
	f, cleanup := newListingFixture(t)
	f.console.Put("/api/admin/orgs/me/profile", f.h.SetMyOrgProfile)
	f.console.Get("/api/admin/orgs/me/proposal", f.h.GetMyOrgProposal)
	f.console.Post("/api/admin/orgs/me/proposal/submit", f.h.SubmitMyOrgProposal)
	f.console.Patch("/api/admin/adventures/{slug}", f.h.UpdateAdventure)
	f.console.Post("/api/admin/adventures/{slug}/avatar", f.h.SetAdventureAvatar)
	f.console.Get("/api/admin/adventures/{slug}/proposal", f.h.GetAdventureProposal)
	f.console.Post("/api/admin/adventures/{slug}/proposal/submit", f.h.SubmitAdventureProposal)
	f.console.Post("/api/admin/events/{eventID}/files", f.h.AddEventFile)

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900})
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", f.queue(t)[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve listing: %d %s", w.Code, w.Body)
	}
	return f, cleanup
}

func (f *listingFixture) scalar(t *testing.T, q string, args ...any) string {
	t.Helper()
	var s string
	if err := f.h.DB.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s
}

func upload(t *testing.T, h http.Handler, path, field, filename, mime string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	hdr.Set("Content-Type", mime)
	part, _ := mw.CreatePart(hdr)
	_, _ = part.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return b.Bytes()
}

// The organisation's page, once something of theirs is for sale: an edit
// is held back as a proposal, submitted with the creator's word, and
// only an approval writes it; the check then agrees with what is served.
func TestPresentationReview_OrgPage(t *testing.T) {
	t.Parallel()
	f, cleanup := newPresentationFixture(t)
	defer cleanup()

	w := call(t, f.console, "PUT", "/api/admin/orgs/me/profile", map[string]any{"tagline": "Puzzles for everyone", "accent_color": "#123456"})
	if w.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT COALESCE(tagline, '') || '|' || COALESCE(accent_color, '') FROM organizations WHERE id = $1`, f.orgID); got != "|#123456" {
		t.Fatalf("live page = %q, want the tagline held back and the colour applied", got)
	}
	w = call(t, f.console, "GET", "/api/admin/orgs/me/proposal", nil)
	if !strings.Contains(w.Body.String(), `"tagline":"Puzzles for everyone"`) || !strings.Contains(w.Body.String(), `"gated":true`) {
		t.Fatalf("proposal = %s", w.Body)
	}
	if w := call(t, f.console, "POST", "/api/admin/orgs/me/proposal/submit", map[string]bool{"attest": true}); w.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}

	var q pendingListing
	for _, x := range f.queue(t) {
		if x.Kind == "org" {
			q = x
		}
	}
	if q.ID == 0 {
		t.Fatalf("no org proposal in the queue: %+v", f.queue(t))
	}
	w = call(t, f.reviewer, "GET", fmt.Sprintf("/api/admin/listing-reviews/org/%d", q.ID), nil)
	if !strings.Contains(w.Body.String(), `"proposed":"Puzzles for everyone"`) {
		t.Fatalf("content: %s", w.Body)
	}
	if w := f.decide(t, "org", q.ID, "approve", q.Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT tagline FROM organizations WHERE id = $1`, f.orgID); got != "Puzzles for everyone" {
		t.Fatalf("live tagline = %q", got)
	}
	w = call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
	if !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"matching":1`) {
		t.Fatalf("check after the page's approval: %s", w.Body)
	}
}

// An adventure's presentation, once it is for sale: a title and an icon
// are held back, a value set back to the live one drops out, and an
// approval writes them.
func TestPresentationReview_Adventure(t *testing.T) {
	t.Parallel()
	f, cleanup := newPresentationFixture(t)
	defer cleanup()

	w := call(t, f.console, "PATCH", "/api/admin/adventures/treasure", map[string]any{"title": "The Big Treasure", "description": "Dig"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "held_for_review") {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT title FROM adventures WHERE slug = 'treasure'`); got != "Treasure Hunt" {
		t.Fatalf("live title = %q, want it held back", got)
	}
	// Setting the description back to what is live drops it out.
	call(t, f.console, "PATCH", "/api/admin/adventures/treasure", map[string]any{"description": f.scalar(t, `SELECT COALESCE(description, '') FROM adventures WHERE slug = 'treasure'`)})
	if w := upload(t, f.console, "/api/admin/adventures/treasure/avatar", "file", "icon.png", "image/png", testPNG(t)); w.Code != http.StatusAccepted {
		t.Fatalf("icon: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT (avatar_content IS NULL)::text FROM adventures WHERE slug = 'treasure'`); got != "true" {
		t.Fatal("the icon was not held back")
	}
	w = call(t, f.console, "GET", "/api/admin/adventures/treasure/proposal", nil)
	if !strings.Contains(w.Body.String(), `"title":"The Big Treasure"`) || strings.Contains(w.Body.String(), "description") || !strings.Contains(w.Body.String(), `"has_image":true`) {
		t.Fatalf("proposal = %s", w.Body)
	}
	if w := call(t, f.console, "POST", "/api/admin/adventures/treasure/proposal/submit", map[string]bool{"attest": true}); w.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	var q pendingListing
	for _, x := range f.queue(t) {
		if x.Kind == "adventure" {
			q = x
		}
	}
	if w := f.decide(t, "adventure", q.ID, "approve", q.Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT title || '|' || (avatar_content IS NOT NULL)::text FROM adventures WHERE slug = 'treasure'`); got != "The Big Treasure|true" {
		t.Fatalf("live adventure = %q", got)
	}
	if got := f.scalar(t, `SELECT count(*)::text FROM presentation_proposals WHERE listing_review <> 'approved'`); got != "0" {
		t.Fatalf("open proposals = %s", got)
	}
	w = call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
	if !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"matching":1`) {
		t.Fatalf("check after the presentation's approval: %s", w.Body)
	}
}

// Before anything is for sale, edits apply at once.
func TestPresentationReview_NotGatedBeforeSale(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	f.console.Patch("/api/admin/adventures/{slug}", f.h.UpdateAdventure)
	if w := call(t, f.console, "PATCH", "/api/admin/adventures/treasure", map[string]any{"title": "Renamed"}); w.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	if got := f.scalar(t, `SELECT title FROM adventures WHERE slug = 'treasure'`); got != "Renamed" {
		t.Fatalf("title = %q", got)
	}
}

// Files for players of an event sold in the store reach them once the
// listing is approved with them.
func TestPresentationReview_EventFiles(t *testing.T) {
	t.Parallel()
	f, cleanup := newPresentationFixture(t)
	defer cleanup()

	if w := upload(t, f.console, fmt.Sprintf("/api/admin/events/%d/files", f.eventID), "file", "rules.pdf", "application/pdf", []byte("%PDF-1.4 rules")); w.Code != http.StatusCreated {
		t.Fatalf("add file: %d %s", w.Code, w.Body)
	}
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after a new file = %q, want draft", got)
	}
	files, err := eventfile.ListForPlayers(context.Background(), f.h.DB, f.eventID)
	if err != nil || len(files) != 0 {
		t.Fatalf("players see %d files (%v) before review", len(files), err)
	}
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", f.queue(t)[0].Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if files, _ := eventfile.ListForPlayers(context.Background(), f.h.DB, f.eventID); len(files) != 1 {
		t.Fatalf("players see %d files after review", len(files))
	}
	w := call(t, f.reviewer, "GET", "/api/admin/listing-reviews/log/verify", nil)
	var rep struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil || !rep.OK {
		t.Fatalf("check: %s", w.Body)
	}
}
