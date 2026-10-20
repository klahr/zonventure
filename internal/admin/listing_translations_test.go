package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/public"
)

// A listing in the adventure's other languages: its own name and blurb
// go through review with the rest of it, and the store shows them, with
// the adventure's own words translated in the version it plays, in the
// language the page asks for.
func TestListingReview_Translations(t *testing.T) {
	t.Parallel()
	f, cleanup := newListingFixture(t)
	defer cleanup()
	db := f.h.DB
	if _, err := db.Exec(`UPDATE adventures SET language = 'sv', area = 'Gamla stan', start_label = 'Vid eken' WHERE slug = 'treasure' AND org_id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	// The adventure, translated into English in a version of its own,
	// which the event plays.
	var advID int64
	if err := db.QueryRow(`SELECT id FROM adventures WHERE slug = 'treasure' AND org_id = $1`, f.orgID).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	was, err := graphstore.Published(ctx, db, advID)
	if err != nil {
		t.Fatal(err)
	}
	g := *was.Graph
	g.Translations = map[string]map[string]string{"en": {
		graph.MetaKey("title"): "The Treasure", graph.MetaKey("area"): "Old town", graph.MetaKey("start_label"): "By the oak",
	}}
	v, _, err := graphstore.Publish(ctx, db, advID, 0, &g, "English")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET graph_id = $1 WHERE id = $2`, v.ID, f.eventID); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []map[string]any{
		{"e n": map[string]string{"name": "x"}},
		{"en": map[string]string{"title": "Only the adventure translates its title"}},
	} {
		w := call(t, f.console, "PATCH", fmt.Sprintf("/api/admin/events/%d", f.eventID), map[string]any{"translations": bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("translations %v: %d %s, want 400", bad, w.Code, w.Body)
		}
	}

	f.patchEvent(t, map[string]any{"ticket_price_cents": 9900, "public_blurb": "Hitta den",
		"translations": map[string]any{"en": map[string]string{"name": " Treasure hunt ", "blurb": "Find it"}, "de": map[string]string{}}})
	f.submitEvent(t)
	c := f.content(t)
	if !slices.ContainsFunc(c.Texts, func(x admin.ReviewText) bool { return x.Where == "Listing (en): blurb" && x.Text == "Find it" }) {
		t.Errorf("the reviewer is not shown the English blurb: %+v", c.Texts)
	}
	if w := f.decide(t, "event", f.eventID, "approve", c.Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}

	in := func(lang string) public.StoreItemDTO {
		t.Helper()
		items := f.storeItemsIn(t, lang)
		if len(items) != 1 {
			t.Fatalf("store in %q = %+v", lang, items)
		}
		return items[0]
	}
	en := in("en")
	if en.EventName != "Treasure hunt" || en.Blurb != "Find it" || en.AdventureTitle != "The Treasure" || en.Area != "Old town" || en.StartLabel != "By the oak" {
		t.Errorf("store in English = %+v", en)
	}
	if !slices.Equal(en.Languages, []string{"sv", "en"}) {
		t.Errorf("languages = %v, want sv then en", en.Languages)
	}
	for _, lang := range []string{"", "sv", "de"} {
		if it := in(lang); it.EventName != "Treasure" || it.Blurb != "Hitta den" || it.Area != "Gamla stan" || it.StartLabel != "Vid eken" {
			t.Errorf("store in %q = %+v, want the main language", lang, it)
		}
	}

	// A changed translation waits for review; the approved one stays up.
	f.patchEvent(t, map[string]any{"translations": map[string]any{"en": map[string]string{"blurb": "Find it now"}}})
	if got := f.eventState(t); got != "draft" {
		t.Fatalf("state after editing a translation = %q, want draft", got)
	}
	if it := in("en"); it.Blurb != "Find it" || it.EventName != "Treasure hunt" {
		t.Errorf("store during the edit = %+v, want the approved translation", it)
	}
	f.submitEvent(t)
	if w := f.decide(t, "event", f.eventID, "approve", f.content(t).Revision, ""); w.Code != http.StatusOK {
		t.Fatalf("approve the edit: %d %s", w.Code, w.Body)
	}
	if it := in("en"); it.Blurb != "Find it now" || it.EventName != "Treasure" {
		t.Errorf("store after the edit = %+v: the English blurb, and no English name left", it)
	}
}

func (f *listingFixture) storeItemsIn(t *testing.T, lang string) []public.StoreItemDTO {
	t.Helper()
	w := call(t, f.store, "GET", "/api/o/acme/store/items?lang="+lang, nil)
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

func (f *listingFixture) content(t *testing.T) admin.ReviewContent {
	t.Helper()
	w := call(t, f.reviewer, "GET", fmt.Sprintf("/api/admin/listing-reviews/event/%d", f.eventID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("content: %d %s", w.Code, w.Body)
	}
	var c admin.ReviewContent
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}
