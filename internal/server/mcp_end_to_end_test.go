package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	imgpng "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/server"
)

// TestMCPFromAdventureToStartedEvents: everything from a new adventure
// to a self-guided and a scheduled event running, through the MCP alone
// — as an author does from the Claude app on a phone, without the
// website: pictures go through upload links the user taps.
func TestMCPFromAdventureToStartedEvents(t *testing.T) {
	t.Setenv("ZV_MCP_ENABLED", "1")
	// Simulated payments, where the MCP may buy an event pass itself.
	ts, _, cleanup := newTestServerWith(t, server.Config{PublicURL: "http://zv.test", PaymentsProvider: "simulated", Env: "dev"})
	defer cleanup()
	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	call := mcpTool(t, ts, pub)
	must := func(name string, args any) string {
		t.Helper()
		text, isErr := call(name, args)
		if isErr {
			t.Fatalf("%s: %s", name, text)
		}
		return text
	}
	// The user taps the link and picks a picture on the page it opens.
	pick := func(text string) {
		t.Helper()
		m := regexp.MustCompile(`http://zv\.test(/api/media-upload/[A-Za-z0-9_-]+)`).FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("no upload link in %q", text)
		}
		resp, err := http.Get(ts.URL + m[1])
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("upload page: %v %v", err, resp)
		}
		resp.Body.Close()
		var png, body bytes.Buffer
		_ = imgpng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 8, 8)))
		mw := multipart.NewWriter(&body)
		// As a browser sends it: with the picture's type.
		fw, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="photo.png"`}, "Content-Type": {"image/png"}})
		_, _ = fw.Write(png.Bytes())
		_ = mw.Close()
		req, _ := http.NewRequest("POST", ts.URL+m[1], &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Accept", "text/html")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		page, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(page), "Uploaded.") {
			t.Fatalf("picking the file: %d %s", resp.StatusCode, page)
		}
	}

	// The adventure: made, a step added, a look and an icon, published.
	must("create_adventure", map[string]any{"slug": "harbour", "title": "The Harbour", "language": "en", "brief": map[string]any{"audience": "families"}})
	must("edit_adventure_graph", map[string]any{"slug": "harbour", "ops": []any{
		map[string]any{"op": "put_node", "node": map[string]any{"slug": "hello", "type": "story", "title": "Hello", "story": map[string]any{"blocks": []any{map[string]any{"id": "b1", "kind": "text", "text": "Welcome aboard."}}}}},
		map[string]any{"op": "connect", "from": "start", "from_port": "out", "to": "hello"},
		map[string]any{"op": "connect", "from": "hello", "from_port": "out", "to": "end"},
	}})
	must("update_adventure_meta", map[string]any{"slug": "harbour", "patch": map[string]any{"accent_color": "#00aa77"}})
	pick(must("get_upload_link", map[string]any{"slug": "harbour", "for": "icon"}))
	pick(must("get_upload_link", map[string]any{"slug": "harbour"}))
	if media := must("list_adventure_media", map[string]any{"slug": "harbour"}); !strings.Contains(media, `"kind":"image"`) {
		t.Errorf("the picked picture is not listed: %s", media)
	}
	if adv := must("get_adventure", map[string]any{"slug": "harbour"}); !strings.Contains(adv, `"icon_url":"http://zv.test/`) || !strings.Contains(adv, `"audience":"families"`) || !strings.Contains(adv, `"accent_color":"#00aa77"`) {
		t.Errorf("get_adventure: %s", adv)
	}
	must("publish_adventure_graph", map[string]any{"slug": "harbour"})
	if list := must("list_adventures", map[string]any{}); !strings.Contains(list, `{"slug":"harbour","title":"The Harbour","language":"en","can_edit":true,"playable":true}`) {
		t.Errorf("list_adventures: %s", list)
	}

	eventID := func(text string) int64 {
		t.Helper()
		var ev struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(text), &ev); err != nil || ev.ID == 0 {
			t.Fatalf("no event id in %s", text)
		}
		return ev.ID
	}
	state := func(id int64) string {
		t.Helper()
		var ev struct {
			State string `json:"state"`
		}
		text := must("get_event_state_summary", map[string]any{"event_id": id})
		_ = json.Unmarshal([]byte(text), &ev)
		return ev.State
	}

	// Self-guided: listed in the store with a price and a cover, open.
	quest := eventID(must("create_event", map[string]any{"adventure_slug": "harbour", "public_for_sale": true, "attest_no_minors": true, "name": "Harbour walk"}))
	// Tickets are sold in SEK only: another currency is refused before anything is saved.
	if text, isErr := call("update_event", map[string]any{"event_id": quest, "ticket_price_cents": 1500, "ticket_currency": "EUR"}); !isErr || !strings.Contains(text, "SEK") {
		t.Errorf("a ticket in EUR: %v %s", isErr, text)
	}
	must("update_event", map[string]any{"event_id": quest, "ticket_price_cents": 19900, "ticket_currency": "SEK", "ticket_team_size": 4, "public_blurb": "A walk by the water."})
	pick(must("get_upload_link", map[string]any{"for": "event_cover", "event_id": quest}))
	must("start_event", map[string]any{"event_id": quest})
	if s := state(quest); s != "running" {
		t.Errorf("self-guided event is %q", s)
	}
	must("give_free_ticket", map[string]any{"event_id": quest, "email": "tester@example.com"})

	// Scheduled: no pass needed (each team is paid for as it starts);
	// teams invited and the event started.
	scheduled := eventID(must("create_event", map[string]any{"adventure_slug": "harbour", "attest_no_minors": true, "name": "Team day", "scheduled_at": "2030-06-01T10:00:00Z"}))
	must("create_team", map[string]any{"event_id": scheduled, "name": "Gulls", "member_names": []string{"Ada", "Bo"}, "email": "gulls@example.com"})
	must("send_event_invitations", map[string]any{"event_id": scheduled})
	must("start_event", map[string]any{"event_id": scheduled})
	if s := state(scheduled); s != "running" {
		t.Errorf("scheduled event is %q", s)
	}
}
