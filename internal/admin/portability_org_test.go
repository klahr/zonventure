package admin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/ws"
)

// fixtureAvatarPNG is a synthetic 1x1 PNG byte blob — realistic
// enough that imageutil's MIME sniff would accept it, small enough
// to assert byte-for-byte equality without bloating the test
// output. Used as the adventure avatar AND the event cover so the
// round-trip exercises both branches.
var fixtureAvatarPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

// orgFixture seeds a synthetic org with the structure the org-bundle
// round-trip test needs: an org row, an adventure with an avatar
// blob (so the per-adventure exporter's asset-writer branch is
// exercised), an unused assignment, an event with a cover image,
// a team + slots, and a paid ticket-purchase row carrying Stripe
// identifiers. Returns the source orgID and the handlers wired
// against the same DB so tests can drive both export and import
// on a single fixture.
func orgFixture(t *testing.T) (db *sql.DB, orgID int64, eventID, teamID int64, h *admin.Handlers) {
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
		INSERT INTO adventures(slug, title, author, org_id, published,
			avatar_mime_type, avatar_size_bytes, avatar_content)
		VALUES ('treasure', 'Treasure Hunt', 'Acme', $1, 1,
			'image/png', $2, $3)
	`, orgID, int64(len(fixtureAvatarPNG)), fixtureAvatarPNG); err != nil {
		t.Fatal(err)
	}
	var assID int64
	if err := db.QueryRow(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, 'treasure', 50)
		RETURNING id
	`, orgID).Scan(&assID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, public_for_sale, assignment_id,
			cover_image_mime_type, cover_image_size_bytes, cover_image_content)
		VALUES ('treasure', $1, $2, 'running', 'live-tok-fixture',
			'My Run', now_text(), FALSE, $3,
			'image/png', $4, $5)
		RETURNING id
	`, orgID, userID, assID, int64(len(fixtureAvatarPNG)), fixtureAvatarPNG).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO teams(event_id, name, token, recap_token, email)
		VALUES ($1, 'Team Alpha', 'team-tok-fixture', 'recap-tok-fixture', 'alpha@acme.com')
		RETURNING id
	`, eventID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO player_slots(team_id, name, token)
		VALUES ($1, 'Player 1', 'slot-tok-1'),
		       ($1, 'Player 2', 'slot-tok-2')
	`, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size, amount_cents, currency, state, paid_at,
			stripe_session_id, stripe_charge_id, team_id)
		VALUES ($1, 'alpha@acme.com', 2, 2500, 'eur', 'paid', now_text(),
			'cs_test_fixture', 'ch_test_fixture', $2)
	`, eventID, teamID); err != nil {
		t.Fatal(err)
	}

	h = &admin.Handlers{DB: db, Lib: adventure.New(db), Hub: ws.NewHub()}
	return db, orgID, eventID, teamID, h
}

// TestOrgBundle_SlugCollision proves the importer rejects with the
// per-adventure importer's 409 shape when the destination DB
// already has an adventure with the same slug. The realistic
// scenario: an operator imports the bundle, then accidentally re-
// runs the import — adventure slugs are globally unique so the
// second pass must fail cleanly rather than silently producing a
// half-imported org.
func TestOrgBundle_SlugCollision(t *testing.T) {
	t.Parallel()
	db, orgID, _, _, h := orgFixture(t)
	ctx := context.Background()

	env, files, err := h.ExportOrgEnvelope(ctx, orgID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var zipBuf bytes.Buffer
	if err := admin.WriteOrgBundle(&zipBuf, env, files); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	// First import attempt: the source org still owns slug=treasure,
	// so the importer hits the slug clash on its first pass — same
	// shape as a re-run after a successful import would produce.
	_, err = admin.ImportOrgBundle(ctx, db, zipBuf.Bytes())
	if err == nil {
		t.Fatalf("import: expected slug-collision error, got nil")
	}
	if !strings.Contains(err.Error(), "treasure") {
		t.Errorf("import: error should mention the colliding slug 'treasure', got %v", err)
	}

	// The cleanupOrg path on the importer must have dropped the
	// half-imported org so we don't leak orphan rows. Source org
	// is still 'Acme'; a successful clash should leave exactly the
	// one 'Acme' org behind, not one + a half-baked 'Acme' clone.
	var orgCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM organizations WHERE name = 'Acme'`).Scan(&orgCount); err != nil {
		t.Fatalf("orgs count: %v", err)
	}
	if orgCount != 1 {
		t.Errorf("orgs after failed import: got %d 'Acme' rows, want 1 (importer should clean up on failure)", orgCount)
	}
}

// The org's flags and its unbilled invoice purchases move with it;
// billed and voided ones stay behind. The summary lists the flags.
func TestOrgBundle_CarriesFlagsAndUnbilledInvoices(t *testing.T) {
	t.Parallel()
	db, orgID, _, _, h := orgFixture(t)
	ctx := context.Background()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`UPDATE organizations SET trusted = 1, verified_creator = TRUE WHERE id = $1`, orgID)
	mustExec(`
		INSERT INTO purchases(org_id, adventure_slug, max_players, amount_cents, currency, provider, state, paid_at,
			assignment_id, top_up_assignment_id, billed_at, voided_at)
		SELECT $1, 'treasure', 10, 49000, 'SEK', 'invoice', 'paid', now_text(), a.id, a.id, b, v
		FROM adventure_assignments a,
		     (VALUES (NULL, NULL), (now_text(), NULL), (NULL, now_text())) AS x(b, v)
		WHERE a.org_id = $1`, orgID)

	env, files, err := h.ExportOrgEnvelope(ctx, orgID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var zipBuf bytes.Buffer
	if err := admin.WriteOrgBundle(&zipBuf, env, files); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	mustExec(`DELETE FROM organizations WHERE id = $1`, orgID)
	summary, err := admin.ImportOrgBundle(ctx, db, zipBuf.Bytes())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.InvoicePurchases != 1 || strings.Join(summary.Flags, ",") != "invoice_billing,verified_creator" {
		t.Fatalf("summary = %+v", summary)
	}
	var (
		trusted         int
		party, verified bool
		amount          int64
		topUpMatches    bool
	)
	if err := db.QueryRow(`SELECT trusted, quiz_trail_enabled, verified_creator FROM organizations WHERE id = $1`, summary.OrgID).
		Scan(&trusted, &party, &verified); err != nil {
		t.Fatal(err)
	}
	if trusted != 1 || party || !verified {
		t.Errorf("flags: trusted %d, party %v, verified %v", trusted, party, verified)
	}
	if err := db.QueryRow(`
		SELECT p.amount_cents, p.top_up_assignment_id = a.id
		FROM purchases p JOIN adventure_assignments a ON a.id = p.assignment_id
		WHERE p.org_id = $1 AND p.provider = 'invoice' AND p.billed_at IS NULL`, summary.OrgID).Scan(&amount, &topUpMatches); err != nil {
		t.Fatalf("imported invoice purchase: %v", err)
	}
	if amount != 49000 || !topUpMatches {
		t.Errorf("imported invoice purchase: amount %d, top-up kept %v", amount, topUpMatches)
	}
}

// An event whose adventure is neither in the bundle nor on the
// destination (another org's catalog adventure) is refused up front:
// imported, its players could never load the game.
func TestOrgBundle_RefusesEventWithoutAdventure(t *testing.T) {
	t.Parallel()
	db, orgID, _, _, h := orgFixture(t)
	ctx := context.Background()

	env, files, err := h.ExportOrgEnvelope(ctx, orgID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(env.Events) == 0 {
		t.Fatal("fixture has no event")
	}
	env.Events[0].AdventureSlug = "someone-elses-catalog-adventure"
	var zipBuf bytes.Buffer
	if err := admin.WriteOrgBundle(&zipBuf, env, files); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	_, err = admin.ImportOrgBundle(ctx, db, zipBuf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "someone-elses-catalog-adventure") {
		t.Fatalf("import: want a refusal naming the missing adventure, got %v", err)
	}
	var orgCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM organizations WHERE name LIKE 'Acme%'`).Scan(&orgCount); err != nil {
		t.Fatalf("orgs count: %v", err)
	}
	if orgCount != 1 {
		t.Errorf("orgs after refused import: got %d, want 1", orgCount)
	}
}

// TestOrgBundle_RejectsBadManifest covers the importer's input-
// validation posture: a non-zip body, a zip without org.json, and
// a manifest with the wrong format string all reject as 400-class
// errors rather than half-importing or panicking. Every case fails
// before the importer touches the database, so no DB is opened.
func TestOrgBundle_RejectsBadManifest(t *testing.T) {
	t.Parallel()
	var db *sql.DB
	ctx := context.Background()

	// Non-zip.
	if _, err := admin.ImportOrgBundle(ctx, db, []byte("not a zip")); err == nil {
		t.Errorf("expected error on non-zip body")
	}

	// Zip without org.json.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("garbage.txt")
	_, _ = w.Write([]byte("nothing useful"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := admin.ImportOrgBundle(ctx, db, buf.Bytes()); err == nil ||
		!strings.Contains(err.Error(), "missing org.json") {
		t.Errorf("expected 'missing org.json' error, got %v", err)
	}

	// Wrong format.
	buf.Reset()
	zw = zip.NewWriter(&buf)
	mw, _ := zw.Create("org.json")
	manifest := map[string]any{"format": "zonventure-org-v999", "org": map[string]string{"name": "x"}}
	mb, _ := json.Marshal(manifest)
	_, _ = mw.Write(mb)
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := admin.ImportOrgBundle(ctx, db, buf.Bytes()); err == nil ||
		!strings.Contains(err.Error(), "unsupported manifest format") {
		t.Errorf("expected 'unsupported manifest format' error, got %v", err)
	}
}
