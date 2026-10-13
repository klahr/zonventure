package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestParty_OfferName: an offer sells under its own name once approved,
// and under the adventure's title without one. The console's list
// counts the adventure's fixed places.
func TestParty_OfferName(t *testing.T) {
	t.Parallel()
	f, cleanup := newPartyFixture(t)
	defer cleanup()
	body := map[string]any{"enabled": true, "base_cents": 19900, "per_kid_cents": 2900, "currency": "sek", "max_kids": 12}
	f.offer(t, body)
	if w := call(t, f.public, "GET", "/api/o/acme/parties", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"title":"Treasure Hunt"`) {
		t.Fatalf("without a name: %d %s", w.Code, w.Body)
	}
	body["name"] = "  Pirate birthday  "
	f.offer(t, body)
	if w := call(t, f.public, "GET", "/api/o/acme/parties", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"title":"Pirate birthday"`) {
		t.Fatalf("with a name: %d %s", w.Code, w.Body)
	}
	// The oak is a fixed place on the map: the offer dialog warns of it.
	if w := call(t, f.console, "GET", "/api/admin/party-offers", nil); !strings.Contains(w.Body.String(), `"name":"Pirate birthday"`) || !strings.Contains(w.Body.String(), `"places":1`) {
		t.Errorf("console list: %s", w.Body)
	}
	body["name"] = strings.Repeat("x", 81)
	if w := call(t, f.console, "PUT", "/api/admin/party-offers/treasure", body); w.Code != http.StatusBadRequest {
		t.Errorf("too long a name: %d %s", w.Code, w.Body)
	}
}
