package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	imgpng "image/png"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/match"
	"github.com/klahr/zonventure/internal/graphstore"
)

// An organisation's assistant submits a listing, with the user's word;
// a platform admin's assistant reads all of it — texts and pictures —
// and approves it, logged as an assistant's decision.
func TestListingReviewTools(t *testing.T) {
	t.Parallel()
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	var orgID, bossID, rootID, advID, eventID int64
	if err := db.QueryRow(`INSERT INTO organizations(name, slug) VALUES ('Acme', 'acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified) VALUES ('boss', 'boss@acme.com', 1) RETURNING id`).Scan(&bossID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, email, email_verified, is_admin) VALUES ('root', 'root@zv.example', 1, TRUE) RETURNING id`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	var icon bytes.Buffer
	_ = imgpng.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	if err := db.QueryRow(`
		INSERT INTO adventures(slug, title, description, author, org_id, avatar_mime_type, avatar_size_bytes, avatar_content)
		VALUES ('oak', 'Oak Hunt', '', '', $1, 'image/png', $2, $3) RETURNING id`, orgID, icon.Len(), icon.Bytes()).Scan(&advID); err != nil {
		t.Fatal(err)
	}
	g := &graph.Graph{
		Zones: []graph.Zone{{Slug: "oak", Name: "The old oak", Lat: 59.33, Lng: 18.06, RadiusM: 30}},
		Nodes: []graph.Node{
			{Slug: "start", Type: graph.Start},
			{Slug: "q", Type: graph.Puzzle, PuzzleCfg: &graph.PuzzleConfig{
				Zone:    "oak",
				Blocks:  graph.LegacyBlocks("What grows on the oak?", nil),
				Outputs: []graph.Output{{Port: "solved", Trigger: graph.OnAnswer, Matchers: []match.Matcher{{Kind: match.Exact, Value: "acorns"}}}},
			}},
			{Slug: "end", Type: graph.End},
		},
		Edges: []graph.Edge{{From: "start", FromPort: graph.PortOut, To: "q"}, {From: "q", FromPort: "solved", To: "end"}},
	}
	if _, _, err := graphstore.Publish(context.Background(), db, advID, bossID, g, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, live_token, name, public_for_sale, graph_id, ticket_price_cents, ticket_currency, listing_review)
		SELECT 'oak', $1, 'running', 'lt-review', 'Oak', TRUE, published_graph_id, 9900, 'sek', 'draft'
		FROM adventures WHERE id = $2 RETURNING id`, orgID, advID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: db, Admin: &admin.Handlers{DB: db}, tools: newRegistry()}
	boss := auth.WithAPIToken(auth.WithUser(context.Background(),
		auth.User{ID: bossID, Username: "boss", Role: auth.RoleOrgAdmin, OrgID: &orgID}))
	root := auth.WithAPIToken(auth.WithUser(context.Background(),
		auth.User{ID: rootID, Username: "root", Role: auth.RoleAdmin, IsAdmin: true, ListingReviewer: true}))

	if res, _ := submitListingForReview(boss, s, json.RawMessage(fmt.Sprintf(`{"event_id":%d,"attest":false}`, eventID))); !res.IsError {
		t.Fatalf("submit without the user's word: %s", textOf(res))
	}
	if res, _ := submitListingForReview(boss, s, json.RawMessage(fmt.Sprintf(`{"event_id":%d,"attest":true}`, eventID))); res.IsError || !strings.Contains(textOf(res), "pending") {
		t.Fatalf("submit: %s", textOf(res))
	}

	res, _ := listListingReviews(root, s, nil)
	var queue struct {
		Reviews []struct {
			Revision string `json:"revision"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &queue); err != nil || len(queue.Reviews) != 1 {
		t.Fatalf("queue: %v %s", err, textOf(res))
	}

	res, _ = getListingReview(root, s, json.RawMessage(fmt.Sprintf(`{"kind":"event","id":%d}`, eventID)))
	if res.IsError {
		t.Fatalf("get: %s", textOf(res))
	}
	text := res.Content[0].Text
	for _, want := range []string{"What grows on the oak?", "acorns", "Oak Hunt", "Submitted by boss", queue.Reviews[0].Revision} {
		if !strings.Contains(text, want) {
			t.Errorf("review text lacks %q:\n%s", want, text)
		}
	}
	var images int
	for _, c := range res.Content {
		if c.Type == "image" && c.MimeType != "" && c.Data != "" {
			images++
		}
	}
	if images != 1 {
		t.Errorf("images = %d, want the adventure's icon", images)
	}

	res, _ = decideListing("approve")(root, s, json.RawMessage(fmt.Sprintf(`{"kind":"event","id":%d,"revision":%q}`, eventID, queue.Reviews[0].Revision)))
	if res.IsError {
		t.Fatalf("approve: %s", textOf(res))
	}
	var via string
	if err := db.QueryRow(`SELECT via FROM listing_review_log WHERE action = 'approved' AND item_id = $1`, eventID).Scan(&via); err != nil || via != "mcp" {
		t.Fatalf("approval logged via %q (%v), want mcp", via, err)
	}
	var listed bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM event_listings WHERE event_id = $1)`, eventID).Scan(&listed); err != nil || !listed {
		t.Fatalf("listed = %v (%v)", listed, err)
	}
}
