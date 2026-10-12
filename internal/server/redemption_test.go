package server_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/server"
)

type redemptionView struct {
	ID           int64  `json:"id"`
	State        string `json:"state"`
	PartnerKind  string `json:"partner_kind"`
	BoxNumber    string `json:"box_number"`
	SecurityCode string `json:"security_code"`
	Email        string `json:"email"`
	PostAt       string `json:"post_at"`
	PostedAt     string `json:"posted_at"`
	Note         string `json:"note"`
	TeamID       int64  `json:"team_id"`
}

type errView struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

// doErr is c.do for a request expected to fail: it returns the status
// and the error body, which c.do doesn't decode.
func doErr(c *client, method, path string, body any) (int, errView) {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, c.ts.URL+path, bytes.NewReader(b))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := c.csrfCookie(); tok != "" {
		req.Header.Set(auth.CSRFHeaderName, tok)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var e errView
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return resp.StatusCode, e
}

// redemptionStack is the purchase stack plus a running self-guided event
// and an email for boss, the org admin who is told about new redemptions.
func redemptionStack(t *testing.T) (*client, *sql.DB, *http.Client, int64, int64, func()) {
	t.Helper()
	ts, db, orgID, cleanup := seedPurchaseStack(t, server.Config{PublicURL: "https://zv.test"})
	eventID := seedEventInOrg(t, db, orgID)
	if _, err := db.Exec(`
		UPDATE events SET public_for_sale = TRUE, state = 'running', started_at = now_text(), ticket_team_size = 3
		WHERE id = $1`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET email = 'boss@org.test' WHERE username = 'boss'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE organizations SET slug = 'testorg' WHERE id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	boss := newClient(t, ts)
	if s := boss.do("POST", "/api/auth/login", map[string]string{"username": "boss", "password": "s3cret"}, nil); s != 200 {
		t.Fatalf("boss login = %d", s)
	}
	return boss, db, ts.Client(), orgID, eventID, cleanup
}

func outboxCount(t *testing.T, db *sql.DB, kind, to string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mail_outbox WHERE kind = $1 AND to_addr = $2`, kind, to).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A buyer's TrueStory code goes from the public form, through the
// organiser's approval, to a team with a year-long link; cancelling
// stops the link and frees the code.
func TestRedemption_TrueStoryLifecycle(t *testing.T) {
	boss, db, _, _, eventID, cleanup := redemptionStack(t)
	defer cleanup()

	var partner struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/partners", map[string]string{"kind": "truestory"}, &partner); s != 201 {
		t.Fatalf("create partner = %d", s)
	}
	if s := boss.do("POST", "/api/admin/partners", map[string]string{"kind": "truestory"}, nil); s != 409 {
		t.Fatalf("second truestory partner = %d, want 409", s)
	}
	var product struct {
		ID       int64 `json:"id"`
		TeamSize int   `json:"team_size"`
	}
	if s := boss.do("POST", "/api/admin/partners/"+itoa(partner.ID)+"/products",
		map[string]any{"name": "Mystery walk", "event_id": eventID}, &product); s != 201 {
		t.Fatalf("create product = %d", s)
	}
	if product.TeamSize != 3 {
		t.Errorf("product team_size = %d, want the event's 3", product.TeamSize)
	}

	buyer := newClient(t, boss.ts)
	var opts struct {
		Partners []struct {
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			Products []struct {
				ID int64 `json:"id"`
			} `json:"products"`
		} `json:"partners"`
	}
	if s := buyer.do("GET", "/api/o/testorg/redeem/options", nil, &opts); s != 200 {
		t.Fatalf("options = %d", s)
	}
	if len(opts.Partners) != 1 || opts.Partners[0].Name != "TrueStory" || len(opts.Partners[0].Products) != 1 {
		t.Fatalf("options = %+v", opts)
	}

	form := func(code string) map[string]any {
		return map[string]any{
			"product_id": product.ID, "security_code": code, "name": "Ada Buyer", "email": "Ada@Buyer.test",
			"street_address": "Storgatan 1", "postal_code": "111 22", "city": "Stockholm", "accepted_terms": true,
		}
	}
	if s, e := doErr(buyer, "POST", "/api/redeem", form("ABCDEF12")); s != 400 || e.Reason != "invalid_code" {
		t.Fatalf("8-character code = %d %+v, want 400 invalid_code", s, e)
	}
	if s := buyer.do("POST", "/api/redeem", form("abc-def-123"), nil); s != 201 {
		t.Fatalf("redeem = %d", s)
	}
	if s, e := doErr(buyer, "POST", "/api/redeem", form("ABCDEF123")); s != 409 || e.Reason != "code_used" {
		t.Fatalf("same code again = %d %+v, want 409 code_used", s, e)
	}
	if n := outboxCount(t, db, "redemption_new", "boss@org.test"); n != 1 {
		t.Errorf("org admin notified %d times, want 1", n)
	}

	var list struct {
		Redemptions []redemptionView `json:"redemptions"`
		Counts      map[string]int   `json:"counts"`
	}
	if s := boss.do("GET", "/api/admin/redemptions", nil, &list); s != 200 {
		t.Fatalf("list = %d", s)
	}
	if len(list.Redemptions) != 1 || list.Counts["pending"] != 1 {
		t.Fatalf("list = %+v", list)
	}
	r := list.Redemptions[0]
	if r.SecurityCode != "ABCDEF123" || r.Email != "ada@buyer.test" || r.State != "pending" {
		t.Errorf("stored redemption = %+v", r)
	}

	var got redemptionView
	if s := boss.do("POST", "/api/admin/redemptions/"+itoa(r.ID)+"/approve", nil, &got); s != 200 {
		t.Fatalf("approve = %d", s)
	}
	if got.State != "approved" || got.TeamID == 0 {
		t.Fatalf("approved = %+v", got)
	}
	var slots int
	var minExpiry string
	if err := db.QueryRow(`SELECT COUNT(*), MIN(expires_at) FROM player_slots WHERE team_id = $1`, got.TeamID).Scan(&slots, &minExpiry); err != nil {
		t.Fatal(err)
	}
	if slots != 3 {
		t.Errorf("team has %d player slots, want 3", slots)
	}
	if exp, err := time.Parse("2006-01-02 15:04:05", minExpiry); err != nil || time.Until(exp) < 360*24*time.Hour {
		t.Errorf("slots expire %q, want about a year out", minExpiry)
	}
	if n := outboxCount(t, db, "redemption_team_link", "ada@buyer.test"); n != 1 {
		t.Errorf("team link mailed %d times, want 1", n)
	}
	if s, e := doErr(boss, "POST", "/api/admin/redemptions/"+itoa(r.ID)+"/approve", nil); s != 409 || e.Reason != "not_pending" {
		t.Errorf("second approve = %d %+v, want 409 not_pending", s, e)
	}

	// Posting is tracked by hand.
	if s := boss.do("PATCH", "/api/admin/redemptions/"+itoa(r.ID),
		map[string]any{"post_at": "2026-10-01", "note": "gift wrap", "posted": true}, &got); s != 200 {
		t.Fatalf("update = %d", s)
	}
	if got.PostAt != "2026-10-01" || got.Note != "gift wrap" || got.PostedAt == "" {
		t.Errorf("updated = %+v", got)
	}

	if s := boss.do("POST", "/api/admin/redemptions/"+itoa(r.ID)+"/cancel", nil, &got); s != 200 || got.State != "cancelled" {
		t.Fatalf("cancel = %d %+v", s, got)
	}
	var withdrawn sql.NullString
	var live int
	if err := db.QueryRow(`
		SELECT t.withdrawn_at, (SELECT COUNT(*) FROM player_slots WHERE team_id = t.id AND expires_at > now_text())
		FROM teams t WHERE t.id = $1`, got.TeamID).Scan(&withdrawn, &live); err != nil {
		t.Fatal(err)
	}
	if !withdrawn.Valid || live != 0 {
		t.Errorf("cancelled team withdrawn=%v, live slots=%d; want withdrawn with no live slots", withdrawn.Valid, live)
	}

	// The code is free again, and a rejection frees it too.
	if s := buyer.do("POST", "/api/redeem", form("ABCDEF123"), nil); s != 201 {
		t.Fatalf("redeem after cancel = %d", s)
	}
	if s := boss.do("GET", "/api/admin/redemptions", nil, &list); s != 200 || len(list.Redemptions) != 1 {
		t.Fatalf("pending after re-redeem = %d %+v", s, list)
	}
	if s := boss.do("POST", "/api/admin/redemptions/"+itoa(list.Redemptions[0].ID)+"/reject", nil, &got); s != 200 || got.State != "rejected" {
		t.Fatalf("reject = %d %+v", s, got)
	}
	if n := outboxCount(t, db, "redemption_rejected", "ada@buyer.test"); n != 1 {
		t.Errorf("rejection mailed %d times, want 1", n)
	}
}

// Partner setup is the org admin's, the queue belongs to the org, and a
// Live it code needs its box number.
func TestRedemption_ScopingAndLiveit(t *testing.T) {
	boss, db, httpc, orgID, eventID, cleanup := redemptionStack(t)
	defer cleanup()

	// The redeem page renders in the visitor's language, per org or for all.
	t.Run("page", func(t *testing.T) {
		get := func(path string) (int, string) {
			t.Helper()
			resp, err := httpc.Get(boss.ts.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(b)
		}
		if s, body := get("/redeem?lang=sv"); s != 200 || !strings.Contains(body, "Lös in ett presentkort") ||
			!strings.Contains(body, "'/api/redeem'") {
			t.Errorf("/redeem = %d, Swedish title or API base missing", s)
		}
		if s, body := get("/o/testorg/redeem"); s != 200 || !strings.Contains(body, "'/api/o/testorg/redeem'") ||
			strings.Contains(body, "{{") {
			t.Errorf("/o/testorg/redeem = %d, org API base missing or placeholder left", s)
		}
		if s, _ := get("/o/nosuchorg/redeem"); s != 404 {
			t.Errorf("unknown org = %d, want 404", s)
		}
	})

	var partner struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/partners", map[string]string{"kind": "liveit"}, &partner); s != 201 {
		t.Fatalf("create partner = %d", s)
	}
	var product struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/partners/"+itoa(partner.ID)+"/products",
		map[string]any{"name": "Box", "event_id": eventID, "team_size": 2}, &product); s != 201 {
		t.Fatalf("create product = %d", s)
	}

	manual := map[string]any{
		"product_id": product.ID, "security_code": "x9y8", "name": "Phone Order", "email": "phone@order.test",
		"street_address": "Gatan 2", "postal_code": "222 33", "city": "Lund", "note": "called in",
	}
	if s, e := doErr(boss, "POST", "/api/admin/redemptions", manual); s != 400 || e.Reason != "invalid_code" {
		t.Fatalf("Live it code without box number = %d %+v, want 400 invalid_code", s, e)
	}
	manual["box_number"] = "123 456"
	var r redemptionView
	if s := boss.do("POST", "/api/admin/redemptions", manual, &r); s != 201 {
		t.Fatalf("manual redemption = %d", s)
	}
	if r.BoxNumber != "123456" || r.SecurityCode != "X9Y8" || r.Note != "called in" || r.State != "pending" {
		t.Errorf("manual redemption = %+v", r)
	}

	hash := fixtureHash(t, "pw")
	var janeID, eveID, otherOrg int64
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('jane', $1, 1) RETURNING id`, hash).Scan(&janeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`, janeID, orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ('Other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO users(username, password_hash, email_verified) VALUES ('eve', $1, 1) RETURNING id`, hash).Scan(&eveID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`, eveID, otherOrg); err != nil {
		t.Fatal(err)
	}
	login := func(name string) *client {
		c := newClient(t, boss.ts)
		if s := c.do("POST", "/api/auth/login", map[string]string{"username": name, "password": "pw"}, nil); s != 200 {
			t.Fatalf("%s login = %d", name, s)
		}
		return c
	}

	jane := login("jane")
	if s := jane.do("POST", "/api/admin/partners", map[string]string{"kind": "truestory"}, nil); s != 403 {
		t.Errorf("member creating a partner = %d, want 403", s)
	}
	if s := jane.do("GET", "/api/admin/redemptions", nil, nil); s != 200 {
		t.Errorf("member listing redemptions = %d, want 200", s)
	}

	eve := login("eve")
	if s := eve.do("POST", "/api/admin/redemptions/"+itoa(r.ID)+"/approve", nil, nil); s != 404 {
		t.Errorf("another org approving = %d, want 404", s)
	}
	if s := eve.do("POST", "/api/admin/partners/"+itoa(partner.ID)+"/products",
		map[string]any{"name": "Steal", "event_id": eventID}, nil); s != 404 {
		t.Errorf("another org adding a product = %d, want 404", s)
	}
	manual["security_code"] = "OTHER1"
	if s, e := doErr(eve, "POST", "/api/admin/redemptions", manual); s != 400 || e.Reason != "unknown_product" {
		t.Errorf("another org's product = %d %+v, want 400 unknown_product", s, e)
	}

	if s, e := doErr(boss, "DELETE", "/api/admin/partners/"+itoa(partner.ID), nil); s != 409 || e.Reason != "in_use" {
		t.Errorf("deleting a partner with redemptions = %d %+v, want 409 in_use", s, e)
	}
}

// A buyer picks the language their envelope is printed in among those
// the product's adventure version is played in. Their emails and their
// team's starting language follow it, and the organiser can change it.
func TestRedemption_LetterLanguage(t *testing.T) {
	boss, db, _, _, eventID, cleanup := redemptionStack(t)
	defer cleanup()

	var graphID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_graphs(adventure_id, version, graph)
		VALUES ((SELECT id FROM adventures WHERE slug = 'demo'), 1, '{"translations": {"sv": {"title": "Demo"}}}')
		RETURNING id`).Scan(&graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET graph_id = $1 WHERE id = $2`, graphID, eventID); err != nil {
		t.Fatal(err)
	}
	var partner, product struct {
		ID int64 `json:"id"`
	}
	if s := boss.do("POST", "/api/admin/partners", map[string]string{"kind": "truestory"}, &partner); s != 201 {
		t.Fatalf("create partner = %d", s)
	}
	if s := boss.do("POST", "/api/admin/partners/"+itoa(partner.ID)+"/products",
		map[string]any{"name": "Mystery walk", "event_id": eventID}, &product); s != 201 {
		t.Fatalf("create product = %d", s)
	}

	buyer := newClient(t, boss.ts)
	var opts struct {
		Partners []struct {
			Products []struct {
				Letter *struct {
					Languages []string `json:"languages"`
				} `json:"letter"`
			} `json:"products"`
		} `json:"partners"`
	}
	if s := buyer.do("GET", "/api/redeem/options", nil, &opts); s != 200 {
		t.Fatalf("options = %d", s)
	}
	if l := opts.Partners[0].Products[0].Letter; l == nil || strings.Join(l.Languages, ",") != "en,sv" {
		t.Fatalf("letter = %+v, want languages en,sv", l)
	}

	form := func(code, lang string) map[string]any {
		return map[string]any{
			"product_id": product.ID, "security_code": code, "name": "Ada Buyer", "email": "ada@buyer.test",
			"street_address": "Storgatan 1", "postal_code": "111 22", "city": "Stockholm",
			"letter_lang": lang, "accepted_terms": true,
		}
	}
	if s, e := doErr(buyer, "POST", "/api/redeem", form("ABCDEF123", "de")); s != 400 || e.Reason != "invalid_language" {
		t.Fatalf("German letter = %d %+v, want 400 invalid_language", s, e)
	}
	if s := buyer.do("POST", "/api/redeem", form("ABCDEF123", "sv"), nil); s != 201 {
		t.Fatalf("redeem in Swedish = %d", s)
	}
	// A form from before the choice gets the main language.
	if s := buyer.do("POST", "/api/redeem", form("ABCDEF124", ""), nil); s != 201 {
		t.Fatalf("redeem without a language = %d", s)
	}

	type langView struct {
		ID              int64    `json:"id"`
		SecurityCode    string   `json:"security_code"`
		LetterLang      string   `json:"letter_lang"`
		LetterLanguages []string `json:"letter_languages"`
		TeamID          int64    `json:"team_id"`
	}
	var list struct {
		Redemptions []langView `json:"redemptions"`
	}
	if s := boss.do("GET", "/api/admin/redemptions", nil, &list); s != 200 || len(list.Redemptions) != 2 {
		t.Fatalf("list = %d %+v", s, list)
	}
	byCode := map[string]langView{}
	for _, r := range list.Redemptions {
		byCode[r.SecurityCode] = r
	}
	sv, en := byCode["ABCDEF123"], byCode["ABCDEF124"]
	if sv.LetterLang != "sv" || en.LetterLang != "en" || strings.Join(sv.LetterLanguages, ",") != "en,sv" {
		t.Fatalf("stored = %+v / %+v", sv, en)
	}

	if s, e := doErr(boss, "PATCH", "/api/admin/redemptions/"+itoa(en.ID), map[string]any{"letter_lang": "xx"}); s != 400 || e.Reason != "invalid_language" {
		t.Errorf("unknown language = %d %+v, want 400 invalid_language", s, e)
	}
	var got langView
	if s := boss.do("PATCH", "/api/admin/redemptions/"+itoa(en.ID), map[string]any{"letter_lang": "sv"}, &got); s != 200 || got.LetterLang != "sv" {
		t.Errorf("change language = %d %+v", s, got)
	}

	if s := boss.do("POST", "/api/admin/redemptions/"+itoa(sv.ID)+"/approve", nil, &got); s != 200 || got.TeamID == 0 {
		t.Fatalf("approve = %d %+v", s, got)
	}
	var teamLang, body string
	if err := db.QueryRow(`SELECT lang FROM teams WHERE id = $1`, got.TeamID).Scan(&teamLang); err != nil {
		t.Fatal(err)
	}
	if teamLang != "sv" {
		t.Errorf("team starts in %q, want sv", teamLang)
	}
	if err := db.QueryRow(`SELECT body_text FROM mail_outbox WHERE kind = 'redemption_team_link' AND to_addr = 'ada@buyer.test'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "bekräftad") {
		t.Errorf("team link email isn't in Swedish: %q", body)
	}
}
