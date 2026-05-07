package admin_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/dbtest"
	"git.sr.ht/~klahr/zv/internal/ws"
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

// TestOrgBundle_RoundTrip exercises the full migration loop: export
// the seeded org, wipe the source DB, replay the bundle into a fresh
// org, and verify every preserved-token column round-trips
// byte-for-byte. Skips without ZV_TEST_DSN per the existing dbtest
// convention.
func TestOrgBundle_RoundTrip(t *testing.T) {
	_, orgID, eventID, teamID, h := orgFixture(t)
	ctx := context.Background()

	// 1. Export the bundle to memory.
	env, files, err := h.ExportOrgEnvelope(ctx, orgID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if env.Format != "zonventure-org-v1" {
		t.Errorf("manifest format: got %q, want zonventure-org-v1", env.Format)
	}
	if env.Org.Name != "Acme" {
		t.Errorf("org name: got %q, want Acme", env.Org.Name)
	}
	if len(env.Adventures) != 1 || env.Adventures[0].Slug != "treasure" {
		t.Errorf("adventures: got %+v, want one with slug 'treasure'", env.Adventures)
	}
	if len(env.Events) != 1 || env.Events[0].LiveToken != "live-tok-fixture" {
		t.Errorf("events: got %+v, want one with live_token 'live-tok-fixture'", env.Events)
	}
	if len(env.Events[0].Teams) != 1 || env.Events[0].Teams[0].Token != "team-tok-fixture" {
		t.Errorf("teams: got %+v, want token 'team-tok-fixture'", env.Events[0].Teams)
	}
	if len(env.Events[0].Teams[0].Slots) != 2 {
		t.Errorf("slots: got %d, want 2", len(env.Events[0].Teams[0].Slots))
	}
	if len(env.Events[0].TicketPurchases) != 1 {
		t.Errorf("purchases: got %d, want 1", len(env.Events[0].TicketPurchases))
	}
	if got := env.Events[0].TicketPurchases[0].StripeChargeID; got != "ch_test_fixture" {
		t.Errorf("stripe_charge_id: got %q, want ch_test_fixture (audit trail must survive)", got)
	}

	// 2. Stream into a zip blob, then split it back to confirm the
	// on-wire format round-trips through admin.WriteOrgBundle.
	var zipBuf bytes.Buffer
	if err := admin.WriteOrgBundle(&zipBuf, env, files); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	if err != nil {
		t.Fatalf("re-read zip: %v", err)
	}
	hasManifest := false
	for _, zf := range zr.File {
		if zf.Name == "org.json" {
			hasManifest = true
		}
	}
	if !hasManifest {
		t.Errorf("zip missing org.json")
	}

	// 3. Wipe the source-side IDs by deleting the org and dependent
	// rows. CASCADE handles events / teams / slots / purchases.
	// Adventures cascade via `org_id` too; the assignment row goes
	// the same way. We keep `users` global per the schema design,
	// but drop the membership row so the new org doesn't pre-exist.
	if _, err := h.DB.Exec(`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		t.Fatalf("wipe source org: %v", err)
	}
	// Sanity: the seeded event id no longer resolves.
	var stillThere int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE id = $1`, eventID).Scan(&stillThere); err != nil {
		t.Fatalf("post-wipe count: %v", err)
	}
	if stillThere != 0 {
		t.Fatalf("post-wipe events count: got %d, want 0 (cascade should drop the row)", stillThere)
	}

	// 4. Import the bundle into the now-empty DB.
	summary, err := admin.ImportOrgBundle(ctx, h.DB, zipBuf.Bytes())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.OrgID == orgID {
		t.Errorf("imported org id collides with the wiped one (got %d) — id-rebase isn't doing its job", summary.OrgID)
	}
	if summary.Adventures != 1 {
		t.Errorf("import adventures: got %d, want 1", summary.Adventures)
	}
	if summary.Events != 1 {
		t.Errorf("import events: got %d, want 1", summary.Events)
	}
	if summary.Teams != 1 {
		t.Errorf("import teams: got %d, want 1", summary.Teams)
	}
	if summary.Slots != 2 {
		t.Errorf("import slots: got %d, want 2", summary.Slots)
	}
	if summary.Purchases != 1 {
		t.Errorf("import purchases: got %d, want 1", summary.Purchases)
	}

	// 5. Verify the preserved tokens — same column values on the
	// destination, even though every primary key is fresh. These
	// are the columns the migration MUST round-trip verbatim
	// because they're embedded in printed QR codes / emailed magic
	// links / bookmarks in the wild.
	var liveTok, teamTok, recapTok string
	if err := h.DB.QueryRow(`SELECT live_token FROM events WHERE org_id = $1`, summary.OrgID).Scan(&liveTok); err != nil {
		t.Fatalf("live_token query: %v", err)
	}
	if liveTok != "live-tok-fixture" {
		t.Errorf("live_token: got %q, want live-tok-fixture", liveTok)
	}
	if err := h.DB.QueryRow(`
		SELECT t.token, t.recap_token
		FROM teams t JOIN events e ON e.id = t.event_id
		WHERE e.org_id = $1
	`, summary.OrgID).Scan(&teamTok, &recapTok); err != nil {
		t.Fatalf("team_token query: %v", err)
	}
	if teamTok != "team-tok-fixture" {
		t.Errorf("team token: got %q, want team-tok-fixture", teamTok)
	}
	if recapTok != "recap-tok-fixture" {
		t.Errorf("recap token: got %q, want recap-tok-fixture", recapTok)
	}
	rows, err := h.DB.Query(`
		SELECT s.token FROM player_slots s
		JOIN teams t ON t.id = s.team_id
		JOIN events e ON e.id = t.event_id
		WHERE e.org_id = $1
		ORDER BY s.token
	`, summary.OrgID)
	if err != nil {
		t.Fatalf("slot tokens query: %v", err)
	}
	defer rows.Close()
	var slotToks []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		slotToks = append(slotToks, s)
	}
	if len(slotToks) != 2 || slotToks[0] != "slot-tok-1" || slotToks[1] != "slot-tok-2" {
		t.Errorf("slot tokens: got %v, want [slot-tok-1 slot-tok-2]", slotToks)
	}

	// 6. Stripe charge id survives as audit data. The refund-button
	// gating against pre-migration charges is a UI concern the bundle
	// doesn't see; the audit trail is what's tested here.
	var chargeID string
	if err := h.DB.QueryRow(`
		SELECT stripe_charge_id FROM ticket_purchases p
		JOIN events e ON e.id = p.event_id
		WHERE e.org_id = $1
	`, summary.OrgID).Scan(&chargeID); err != nil {
		t.Fatalf("stripe_charge_id query: %v", err)
	}
	if chargeID != "ch_test_fixture" {
		t.Errorf("stripe_charge_id: got %q, want ch_test_fixture (audit must survive)", chargeID)
	}

	// 7. The destination org's Stripe Connect identifiers MUST be
	// cleared — the org has to re-onboard against the destination
	// platform.
	var connectAccount sql.NullString
	if err := h.DB.QueryRow(
		`SELECT stripe_connect_account_id FROM organizations WHERE id = $1`,
		summary.OrgID).Scan(&connectAccount); err != nil {
		t.Fatalf("connect query: %v", err)
	}
	if connectAccount.Valid && connectAccount.String != "" {
		t.Errorf("stripe_connect_account_id: got %q, want NULL (must re-onboard)", connectAccount.String)
	}

	// 8. Adventure avatar bytes survive the round-trip byte-for-byte.
	// Catches regressions where the bundle drops the asset prefix
	// or the per-adventure importer fails to re-attach the bytes.
	var avBytes []byte
	if err := h.DB.QueryRow(
		`SELECT avatar_content FROM adventures WHERE org_id = $1 AND slug = 'treasure'`,
		summary.OrgID).Scan(&avBytes); err != nil {
		t.Fatalf("adventure avatar query: %v", err)
	}
	if !bytes.Equal(avBytes, fixtureAvatarPNG) {
		t.Errorf("adventure avatar: got %d bytes, want %d (byte-for-byte equality required)",
			len(avBytes), len(fixtureAvatarPNG))
	}

	// 9. Event cover bytes survive the round-trip.
	var coverBytes []byte
	if err := h.DB.QueryRow(
		`SELECT cover_image_content FROM events WHERE org_id = $1`,
		summary.OrgID).Scan(&coverBytes); err != nil {
		t.Fatalf("event cover query: %v", err)
	}
	if !bytes.Equal(coverBytes, fixtureAvatarPNG) {
		t.Errorf("event cover: got %d bytes, want %d (byte-for-byte equality required)",
			len(coverBytes), len(fixtureAvatarPNG))
	}

	_ = teamID // silence unused if cascade test ever drops the var
}

// TestOrgBundle_SlugCollision proves the importer rejects with the
// per-adventure importer's 409 shape when the destination DB
// already has an adventure with the same slug. The realistic
// scenario: an operator imports the bundle, then accidentally re-
// runs the import — adventure slugs are globally unique so the
// second pass must fail cleanly rather than silently producing a
// half-imported org.
func TestOrgBundle_SlugCollision(t *testing.T) {
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

// TestOrgBundle_ManifestFormat checks the format string on the
// manifest. Cheap but locks the contract — bumping the format
// without bumping the constant would otherwise silently break
// imports.
func TestOrgBundle_ManifestFormat(t *testing.T) {
	_, orgID, _, _, h := orgFixture(t)
	env, _, err := h.ExportOrgEnvelope(context.Background(), orgID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if env.Format != "zonventure-org-v1" {
		t.Errorf("format: got %q, want zonventure-org-v1", env.Format)
	}
}

// TestOrgBundle_RejectsBadManifest covers the importer's input-
// validation posture: a non-zip body, a zip without org.json, and
// a manifest with the wrong format string all reject as 400-class
// errors rather than half-importing or panicking.
func TestOrgBundle_RejectsBadManifest(t *testing.T) {
	db, _, _, _, _ := orgFixture(t)
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
