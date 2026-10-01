package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/adventure"
	"github.com/klahr/zonventure/internal/dbtest"
	"github.com/klahr/zonventure/internal/ws"
)

// pngBytes is a deterministic 1x1 PNG used for every blob in the
// rich fixture. The same bytes for every column so an
// accidentally-swapped FK doesn't pass equality by coincidence;
// we add a per-asset salt where the schema has a label column
// (alt_text, description) that survives the round-trip too.
var pngBytes = []byte{
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

// richFixture seeds two orgs (Acme + Bystander) with a wide spread
// of data so the round-trip exercises everything the migration
// touches: adventures (metadata only); events in scheduled /
// running / completed / cancelled / quest states; teams in mid-
// game / completed / withdrawn states with and without avatars;
// slots in claimed / unclaimed / expiring states; ticket purchases
// in paid / pending / failed states with Stripe identifiers.
//
// Bystander exists alongside Acme so the round-trip can verify
// nothing leaks across org boundaries — neither at export time
// (Bystander rows must NOT appear in Acme's bundle) nor at import
// time (the destination must end up with the imported Acme org +
// the original Bystander, untouched).
type richFixtureIDs struct {
	acme             int64
	bystander        int64
	bossUserID       int64
	minionUserID     int64
	advLinear        string // slug
	advShuffled      string // slug
	advBystander     string // slug — must NOT appear in Acme's bundle
	eventScheduledID int64
	eventRunningID   int64
	eventCompletedID int64
	eventCancelledID int64
	eventQuestID     int64
	bystanderEventID int64 // must NOT appear in Acme's bundle
	teamMidGameID    int64
	teamCompletedID  int64
	teamWithdrawnID  int64
	teamQuestBuyerID int64
}

func richFixture(t *testing.T) (db *sql.DB, h *admin.Handlers, ids richFixtureIDs) {
	t.Helper()
	db, cleanup := dbtest.Open(t)
	t.Cleanup(cleanup)
	h = &admin.Handlers{DB: db, Lib: adventure.New(db), Hub: ws.NewHub()}

	// ---- Orgs --------------------------------------------------
	mustQuery(t, db, `INSERT INTO organizations(name) VALUES ('Acme') RETURNING id`, &ids.acme)
	mustQuery(t, db, `INSERT INTO organizations(name) VALUES ('Bystander') RETURNING id`, &ids.bystander)

	// ---- Users + memberships ----------------------------------
	mustQuery(t, db, `
		INSERT INTO users(username, email, email_verified)
		VALUES ('boss', 'boss@acme.com', 1) RETURNING id
	`, &ids.bossUserID)
	mustQuery(t, db, `
		INSERT INTO users(username, email, email_verified)
		VALUES ('minion', 'minion@acme.com', 0) RETURNING id
	`, &ids.minionUserID)
	mustExec(t, db, `INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		ids.bossUserID, ids.acme)
	mustExec(t, db, `INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_member')`,
		ids.minionUserID, ids.acme)

	var strangerID int64
	mustQuery(t, db, `
		INSERT INTO users(username, email, email_verified)
		VALUES ('stranger', 'stranger@bystander.com', 1) RETURNING id
	`, &strangerID)
	mustExec(t, db, `INSERT INTO org_memberships(user_id, org_id, role) VALUES ($1, $2, 'org_admin')`,
		strangerID, ids.bystander)

	// ---- Adventure A: full metadata + avatar, authored by boss
	ids.advLinear = "linear-quest"
	mustExec(t, db, `
		INSERT INTO adventures(slug, title, author, description, intro_text, outro_text,
			org_id, created_by_user_id, published, language, strict_assets,
			start_lat, start_lng, start_label,
			accent_color, accent_secondary_color, background_color, text_color, submit_bg_color,
			avatar_mime_type, avatar_size_bytes, avatar_content)
		VALUES ($1, 'Linear Quest', 'Acme', 'A linear walk', 'Welcome', 'Goodbye',
			$2, $3, 1, 'en', TRUE,
			59.3293, 18.0686, 'Stockholm Central Station',
			'#ff8800', '#0088ff', '#222222', '#ffffff', '#33aa33',
			'image/png', $4, $5)
	`, ids.advLinear, ids.acme, ids.bossUserID, int64(len(pngBytes)), pngBytes)

	// ---- Adventure B: minimal metadata, authored by minion ----
	ids.advShuffled = "race-relay"
	mustExec(t, db, `
		INSERT INTO adventures(slug, title, author, description, org_id, created_by_user_id, published, language)
		VALUES ($1, 'Race Relay', 'Acme', 'Shuffled middle, shared finish', $2, $3, 1, 'sv')
	`, ids.advShuffled, ids.acme, ids.minionUserID)

	// ---- Bystander adventure (must NOT appear in Acme bundle) -
	ids.advBystander = "bystander-adv"
	mustExec(t, db, `
		INSERT INTO adventures(slug, title, author, org_id, published)
		VALUES ($1, 'Bystander Adventure', 'Stranger', $2, 1)
	`, ids.advBystander, ids.bystander)

	// ---- Acme assignments ------------------------------------
	var assn1, assn2 int64
	mustQuery(t, db, `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, $2, 20) RETURNING id
	`, &assn1, ids.acme, ids.advLinear)
	mustQuery(t, db, `
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
		VALUES ($1, $2, 40) RETURNING id
	`, &assn2, ids.acme, ids.advShuffled)

	// ---- Acme events: scheduled / running / completed / cancelled / quest
	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, scheduled_at, assignment_id)
		VALUES ($1, $2, $3, 'scheduled', 'live-tok-scheduled',
			'Sunday hunt', now_text(), '2099-01-01 10:00:00', $4) RETURNING id
	`, &ids.eventScheduledID, ids.advLinear, ids.acme, ids.bossUserID, assn1)

	// Authored by minion (not boss) to verify per-event authorship.
	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, started_at)
		VALUES ($1, $2, $3, 'running', 'live-tok-running',
			'Live race', now_text(), now_text()) RETURNING id
	`, &ids.eventRunningID, ids.advLinear, ids.acme, ids.minionUserID)

	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, started_at, completed_at)
		VALUES ($1, $2, $3, 'completed', 'live-tok-completed',
			'Last week''s event', now_text(), now_text(), now_text()) RETURNING id
	`, &ids.eventCompletedID, ids.advLinear, ids.acme, ids.bossUserID)

	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, cancelled_at)
		VALUES ($1, $2, $3, 'cancelled', 'live-tok-cancelled',
			'Rained out', now_text(), now_text()) RETURNING id
	`, &ids.eventCancelledID, ids.advLinear, ids.acme, ids.bossUserID)

	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at, started_at, assignment_id,
			public_for_sale, public_blurb, ticket_price_cents, ticket_currency, ticket_team_size,
			cover_image_mime_type, cover_image_size_bytes, cover_image_content)
		VALUES ($1, $2, $3, 'running', 'live-tok-quest',
			'Always-on quest', now_text(), now_text(), $4,
			TRUE, 'Two-hour treasure hunt around the old town.', 2500, 'eur', 4,
			'image/png', $5, $6) RETURNING id
	`, &ids.eventQuestID, ids.advShuffled, ids.acme, ids.bossUserID, assn2,
		int64(len(pngBytes)), pngBytes)

	// ---- Bystander event + team + slot (must NOT appear in Acme bundle) -----
	mustQuery(t, db, `
		INSERT INTO events(adventure_slug, org_id, created_by_user_id, state, live_token,
			name, attested_no_minors_at)
		VALUES ($1, $2, $3, 'scheduled', 'live-tok-bystander',
			'Bystander event', now_text()) RETURNING id
	`, &ids.bystanderEventID, ids.advBystander, ids.bystander, strangerID)
	var bystanderTeamID int64
	mustQuery(t, db, `
		INSERT INTO teams(event_id, name, token, recap_token, email)
		VALUES ($1, 'Bystander Team', 'team-tok-bystander', 'recap-tok-bystander', 'b@bystander.com')
		RETURNING id
	`, &bystanderTeamID, ids.bystanderEventID)
	mustExec(t, db, `
		INSERT INTO player_slots(team_id, name, token)
		VALUES ($1, 'Bystander Player', 'slot-tok-bystander')
	`, bystanderTeamID)

	// ---- Teams on the running event --------------------------
	mustQuery(t, db, `
		INSERT INTO teams(event_id, name, token, recap_token, email,
			avatar_mime_type, avatar_size_bytes, avatar_content)
		VALUES ($1, 'Mid Team', 'team-tok-mid', 'recap-tok-mid', 'mid@acme.com',
			'image/png', $2, $3) RETURNING id
	`, &ids.teamMidGameID, ids.eventRunningID, int64(len(pngBytes)), pngBytes)

	mustQuery(t, db, `
		INSERT INTO teams(event_id, name, token, recap_token, email, completed_at)
		VALUES ($1, 'Completed Team', 'team-tok-done', 'recap-tok-done', 'done@acme.com', now_text())
		RETURNING id
	`, &ids.teamCompletedID, ids.eventCompletedID)

	mustQuery(t, db, `
		INSERT INTO teams(event_id, name, token, recap_token, withdrawn_at)
		VALUES ($1, 'Withdrew Team', 'team-tok-withdrew', 'recap-tok-withdrew', now_text())
		RETURNING id
	`, &ids.teamWithdrawnID, ids.eventCancelledID)

	mustQuery(t, db, `
		INSERT INTO teams(event_id, name, token, recap_token, email)
		VALUES ($1, 'buyer42', 'team-tok-quest-buyer', 'recap-tok-quest-buyer', 'buyer@external.com')
		RETURNING id
	`, &ids.teamQuestBuyerID, ids.eventQuestID)

	// ---- Slots on the mid-game team --------------------------
	mustExec(t, db, `
		INSERT INTO player_slots(team_id, name, token, expires_at, claimed_at, last_login_at)
		VALUES
		  ($1, 'Player 1', 'slot-mid-1', '2099-01-01', now_text(), now_text()),
		  ($1, 'Player 2', 'slot-mid-2', '2099-01-01', NULL,        NULL),
		  ($1, 'Player 3', 'slot-mid-3', NULL,         now_text(), now_text())
	`, ids.teamMidGameID)

	// ---- Event activity rows on the running event ------------
	// Three distinct shapes: team-event with no actor (system),
	// team-event with a slot actor, and an admin-actor event.
	mustExec(t, db, `
		INSERT INTO event_activity(event_id, team_id, kind, payload, created_at)
		VALUES ($1, $2, 'team_started', '{"page":0}', now_text())
	`, ids.eventRunningID, ids.teamMidGameID)
	mustExec(t, db, `
		INSERT INTO event_activity(event_id, team_id, actor_slot_id, kind, payload, created_at)
		VALUES ($1, $2,
		    (SELECT id FROM player_slots WHERE token = 'slot-mid-1'),
		    'answer_correct', '{"page":0,"answer":"hello"}', now_text())
	`, ids.eventRunningID, ids.teamMidGameID)
	mustExec(t, db, `
		INSERT INTO event_activity(event_id, actor_user_id, kind, payload, created_at)
		VALUES ($1, $2, 'event_paused', '{}', now_text())
	`, ids.eventRunningID, ids.bossUserID)

	// Slots on the quest-buyer team.
	mustExec(t, db, `
		INSERT INTO player_slots(team_id, name, token, expires_at)
		VALUES ($1, 'Q1', 'slot-quest-1', '2099-01-01'),
		       ($1, 'Q2', 'slot-quest-2', '2099-01-01'),
		       ($1, 'Q3', 'slot-quest-3', '2099-01-01'),
		       ($1, 'Q4', 'slot-quest-4', '2099-01-01')
	`, ids.teamQuestBuyerID)

	// ---- Ticket purchases on the quest event -----------------
	mustExec(t, db, `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size,
			amount_cents, currency, application_fee_cents,
			state, paid_at, stripe_session_id, stripe_charge_id, stripe_invoice_url, team_id)
		VALUES ($1, 'buyer@external.com', 4, 2500, 'eur', 250,
			'paid', now_text(), 'cs_test_paid', 'ch_test_paid', 'https://invoice.example/ABC', $2)
	`, ids.eventQuestID, ids.teamQuestBuyerID)
	mustExec(t, db, `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size,
			amount_cents, currency, state)
		VALUES ($1, 'pending@example.com', 4, 2500, 'eur', 'pending')
	`, ids.eventQuestID)
	mustExec(t, db, `
		INSERT INTO ticket_purchases(event_id, buyer_email, team_size,
			amount_cents, currency, state)
		VALUES ($1, 'failed@example.com', 4, 2500, 'eur', 'failed')
	`, ids.eventQuestID)

	return db, h, ids
}

// ---- Helpers ----------------------------------------------------

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", trimQuery(query), err)
	}
}

func mustQuery(t *testing.T, db *sql.DB, query string, dest any, args ...any) {
	t.Helper()
	if err := db.QueryRow(query, args...).Scan(dest); err != nil {
		t.Fatalf("queryrow %q: %v", trimQuery(query), err)
	}
}

func trimQuery(q string) string {
	if len(q) > 80 {
		return q[:80] + "…"
	}
	return q
}

// ---- Snapshot machinery -----------------------------------------
//
// orgSnapshot pulls the data we care about for round-trip equality
// out of an org's tables, keyed by stable identifiers (slug, token,
// name) rather than by primary keys. Two snapshots with equivalent
// content must compare equal even though the IDs underneath
// differ — that's the whole point.

type orgSnapshot struct {
	Org             orgRow
	Members         []memberRow
	Adventures      []advRow
	Assignments     []assignRow
	Events          []eventRow
	Teams           []teamRow
	Slots           []slotRow
	TicketPurchases []ticketRow
	Activity        []activityRow
}

type orgRow struct {
	Name      string
	LogoMime  string
	LogoBytes []byte
	// Stripe Connect identifiers must be cleared on import.
	StripeConnectAccountID sql.NullString
}
type memberRow struct {
	Username      string
	Email         string
	EmailVerified bool
	Role          string
}
type advRow struct {
	Slug, Title, Description, Author, Intro, Outro, Language string
	StrictAssets                                             bool
	StartLat, StartLng                                       sql.NullFloat64
	StartLabel                                               string
	Accent, Accent2, BG, Ink, SubmitBG                       string
	AvatarMime                                               string
	AvatarBytes                                              []byte
	CreatedByUsername                                        string
}
type assignRow struct {
	AdventureSlug string
	MaxPlayers    int
}
type eventRow struct {
	LiveToken          string
	AdventureSlug      string
	Name               string
	State              string
	ScheduledAt        string
	StartedAt          string
	CompletedAt        string
	CancelledAt        string
	AttestedNoMinorsAt string
	PublicForSale      bool
	PublicBlurb        string
	TicketPriceCents   int64
	TicketCurrency     string
	TicketTeamSize     int
	CoverMime          string
	CoverBytes         []byte
	HasAssignment      bool // we don't compare ids; just whether it points
	CreatedByUsername  string
}
type teamRow struct {
	Token          string
	RecapToken     string
	EventLiveToken string
	Name           string
	Email          string
	WithdrawnAt    string
	CompletedAt    string
	AvatarMime     string
	AvatarBytes    []byte
}

// activityRow captures one event_activity row keyed by the parent
// event's live token plus the kind/payload/created_at + actor stable
// keys. Comparisons are tolerant of NULL actor fields.
type activityRow struct {
	EventLiveToken string
	Kind           string
	Payload        string
	CreatedAt      string
	TeamToken      string
	ActorUsername  string
	ActorSlotToken string
}
type slotRow struct {
	Token        string
	TeamToken    string
	Name         string
	ExpiresAt    string
	HasClaimedAt bool
	HasLastLogin bool
}
type ticketRow struct {
	BuyerEmail          string
	EventLiveToken      string
	TeamSize            int
	AmountCents         int64
	Currency            string
	ApplicationFeeCents int64
	State               string
	StripeSessionID     string
	StripeChargeID      string
	StripeInvoiceURL    string
	TeamToken           string // empty if no team minted yet
}

func snapshotOrg(t *testing.T, db *sql.DB, orgID int64) orgSnapshot {
	t.Helper()
	var s orgSnapshot

	// Org row
	if err := db.QueryRow(`
		SELECT name, COALESCE(logo_mime_type, ''),
		       COALESCE(logo_content, ''::bytea),
		       stripe_connect_account_id
		FROM organizations WHERE id = $1
	`, orgID).Scan(&s.Org.Name, &s.Org.LogoMime, &s.Org.LogoBytes, &s.Org.StripeConnectAccountID); err != nil {
		t.Fatalf("snapshot org row: %v", err)
	}

	// Members
	rows := mustRows(t, db, `
		SELECT u.username, COALESCE(u.email, ''), u.email_verified, m.role
		FROM org_memberships m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 ORDER BY u.username
	`, orgID)
	for rows.Next() {
		var m memberRow
		var verified int
		if err := rows.Scan(&m.Username, &m.Email, &verified, &m.Role); err != nil {
			t.Fatalf("scan member: %v", err)
		}
		m.EmailVerified = verified != 0
		s.Members = append(s.Members, m)
	}
	rows.Close()

	// Adventures
	rows = mustRows(t, db, `
		SELECT a.slug, a.title, a.description, COALESCE(a.author, ''),
		       COALESCE(a.intro_text, ''), COALESCE(a.outro_text, ''),
		       COALESCE(a.language, ''), a.strict_assets,
		       a.start_lat, a.start_lng, COALESCE(a.start_label, ''),
		       COALESCE(a.accent_color, ''), COALESCE(a.accent_secondary_color, ''),
		       COALESCE(a.background_color, ''), COALESCE(a.text_color, ''),
		       COALESCE(a.submit_bg_color, ''),
		       COALESCE(a.avatar_mime_type, ''), COALESCE(a.avatar_content, ''::bytea),
		       COALESCE(u.username, '')
		FROM adventures a
		LEFT JOIN users u ON u.id = a.created_by_user_id
		WHERE a.org_id = $1 ORDER BY a.slug
	`, orgID)
	for rows.Next() {
		var a advRow
		if err := rows.Scan(&a.Slug, &a.Title, &a.Description, &a.Author,
			&a.Intro, &a.Outro, &a.Language, &a.StrictAssets,
			&a.StartLat, &a.StartLng, &a.StartLabel,
			&a.Accent, &a.Accent2, &a.BG, &a.Ink, &a.SubmitBG,
			&a.AvatarMime, &a.AvatarBytes, &a.CreatedByUsername); err != nil {
			t.Fatalf("scan adv: %v", err)
		}
		s.Adventures = append(s.Adventures, a)
	}
	rows.Close()

	// Assignments
	rows = mustRows(t, db, `
		SELECT adventure_slug, max_players FROM adventure_assignments
		WHERE org_id = $1 ORDER BY adventure_slug, max_players
	`, orgID)
	for rows.Next() {
		var a assignRow
		if err := rows.Scan(&a.AdventureSlug, &a.MaxPlayers); err != nil {
			t.Fatalf("scan assignment: %v", err)
		}
		s.Assignments = append(s.Assignments, a)
	}
	rows.Close()

	// Events
	rows = mustRows(t, db, `
		SELECT e.live_token, e.adventure_slug, COALESCE(e.name, ''), e.state,
		       COALESCE(e.scheduled_at, ''), COALESCE(e.started_at, ''),
		       COALESCE(e.completed_at, ''), COALESCE(e.cancelled_at, ''),
		       COALESCE(e.attested_no_minors_at, ''),
		       e.public_for_sale, COALESCE(e.public_blurb, ''),
		       COALESCE(e.ticket_price_cents, 0), COALESCE(e.ticket_currency, ''),
		       COALESCE(e.ticket_team_size, 0),
		       COALESCE(e.cover_image_mime_type, ''), COALESCE(e.cover_image_content, ''::bytea),
		       (e.assignment_id IS NOT NULL),
		       COALESCE(u.username, '')
		FROM events e
		LEFT JOIN users u ON u.id = e.created_by_user_id
		WHERE e.org_id = $1 ORDER BY e.live_token
	`, orgID)
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.LiveToken, &e.AdventureSlug, &e.Name, &e.State,
			&e.ScheduledAt, &e.StartedAt, &e.CompletedAt, &e.CancelledAt,
			&e.AttestedNoMinorsAt,
			&e.PublicForSale, &e.PublicBlurb,
			&e.TicketPriceCents, &e.TicketCurrency, &e.TicketTeamSize,
			&e.CoverMime, &e.CoverBytes,
			&e.HasAssignment, &e.CreatedByUsername); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		s.Events = append(s.Events, e)
	}
	rows.Close()

	// Teams
	rows = mustRows(t, db, `
		SELECT t.token, t.recap_token, e.live_token, t.name, COALESCE(t.email, ''),
		       COALESCE(t.withdrawn_at, ''),
		       COALESCE(t.completed_at, ''),
		       COALESCE(t.avatar_mime_type, ''), COALESCE(t.avatar_content, ''::bytea)
		FROM teams t JOIN events e ON e.id = t.event_id
		WHERE e.org_id = $1 ORDER BY t.token
	`, orgID)
	for rows.Next() {
		var tr teamRow
		if err := rows.Scan(&tr.Token, &tr.RecapToken, &tr.EventLiveToken, &tr.Name,
			&tr.Email, &tr.WithdrawnAt, &tr.CompletedAt,
			&tr.AvatarMime, &tr.AvatarBytes); err != nil {
			t.Fatalf("scan team: %v", err)
		}
		s.Teams = append(s.Teams, tr)
	}
	rows.Close()

	// Activity log.
	rows = mustRows(t, db, `
		SELECT e.live_token, a.kind, COALESCE(a.payload, '{}'), a.created_at,
		       COALESCE(t.token, ''),
		       COALESCE(u.username, ''),
		       COALESCE(sl.token, '')
		FROM event_activity a
		JOIN events e ON e.id = a.event_id
		LEFT JOIN teams t ON t.id = a.team_id
		LEFT JOIN users u ON u.id = a.actor_user_id
		LEFT JOIN player_slots sl ON sl.id = a.actor_slot_id
		WHERE e.org_id = $1
		ORDER BY a.id
	`, orgID)
	for rows.Next() {
		var a activityRow
		if err := rows.Scan(&a.EventLiveToken, &a.Kind, &a.Payload, &a.CreatedAt,
			&a.TeamToken, &a.ActorUsername, &a.ActorSlotToken); err != nil {
			t.Fatalf("scan activity: %v", err)
		}
		s.Activity = append(s.Activity, a)
	}
	rows.Close()

	// Slots
	rows = mustRows(t, db, `
		SELECT s.token, t.token, s.name, COALESCE(s.expires_at, ''),
		       (s.claimed_at IS NOT NULL), (s.last_login_at IS NOT NULL)
		FROM player_slots s
		JOIN teams t ON t.id = s.team_id
		JOIN events e ON e.id = t.event_id
		WHERE e.org_id = $1 ORDER BY s.token
	`, orgID)
	for rows.Next() {
		var sr slotRow
		if err := rows.Scan(&sr.Token, &sr.TeamToken, &sr.Name, &sr.ExpiresAt,
			&sr.HasClaimedAt, &sr.HasLastLogin); err != nil {
			t.Fatalf("scan slot: %v", err)
		}
		s.Slots = append(s.Slots, sr)
	}
	rows.Close()

	// Ticket purchases
	rows = mustRows(t, db, `
		SELECT p.buyer_email, e.live_token, p.team_size, p.amount_cents, p.currency,
		       COALESCE(p.application_fee_cents, 0), p.state,
		       COALESCE(p.stripe_session_id, ''), COALESCE(p.stripe_charge_id, ''),
		       COALESCE(p.stripe_invoice_url, ''),
		       COALESCE(t.token, '')
		FROM ticket_purchases p
		JOIN events e ON e.id = p.event_id
		LEFT JOIN teams t ON t.id = p.team_id
		WHERE e.org_id = $1 ORDER BY p.buyer_email, p.amount_cents
	`, orgID)
	for rows.Next() {
		var p ticketRow
		if err := rows.Scan(&p.BuyerEmail, &p.EventLiveToken, &p.TeamSize, &p.AmountCents,
			&p.Currency, &p.ApplicationFeeCents, &p.State,
			&p.StripeSessionID, &p.StripeChargeID, &p.StripeInvoiceURL,
			&p.TeamToken); err != nil {
			t.Fatalf("scan purchase: %v", err)
		}
		s.TicketPurchases = append(s.TicketPurchases, p)
	}
	rows.Close()

	return s
}

func mustRows(t *testing.T, db *sql.DB, query string, args ...any) *sql.Rows {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", trimQuery(query), err)
	}
	return rows
}

// diffSnapshots returns a slice of human-readable mismatch lines.
// Empty slice means the snapshots are equivalent (modulo Org.Name
// which the importer disambiguates).
func diffSnapshots(orig, imported orgSnapshot) []string {
	var out []string
	report := func(field string, want, got any) {
		out = append(out, fmt.Sprintf("%s: want %+v, got %+v", field, want, got))
	}

	// Org name: import disambiguates with " (imported)" so don't
	// require strict equality. The bytes / mime must match.
	if orig.Org.LogoMime != imported.Org.LogoMime {
		report("org.logo_mime", orig.Org.LogoMime, imported.Org.LogoMime)
	}
	if !bytes.Equal(orig.Org.LogoBytes, imported.Org.LogoBytes) {
		report("org.logo_bytes_len", len(orig.Org.LogoBytes), len(imported.Org.LogoBytes))
	}
	if imported.Org.StripeConnectAccountID.Valid && imported.Org.StripeConnectAccountID.String != "" {
		report("org.stripe_connect_account_id", "NULL", imported.Org.StripeConnectAccountID.String)
	}

	if !sameMembers(orig.Members, imported.Members) {
		report("members", orig.Members, imported.Members)
	}
	if !sameAdventures(orig.Adventures, imported.Adventures) {
		diffs := diffAdvList(orig.Adventures, imported.Adventures)
		if len(diffs) == 0 {
			diffs = []string{"adventures differ (no per-field detail emitted — extend diffAdvList)"}
		}
		out = append(out, diffs...)
	}
	if !sameAssignments(orig.Assignments, imported.Assignments) {
		report("assignments", orig.Assignments, imported.Assignments)
	}
	if !sameEvents(orig.Events, imported.Events) {
		diffs := diffEventList(orig.Events, imported.Events)
		if len(diffs) == 0 {
			diffs = []string{"events differ (no per-field detail emitted — extend diffEventList)"}
		}
		out = append(out, diffs...)
	}
	if !sameTeams(orig.Teams, imported.Teams) {
		diffs := diffTeamList(orig.Teams, imported.Teams)
		if len(diffs) == 0 {
			diffs = []string{"teams differ (no per-field detail emitted — extend diffTeamList)"}
		}
		out = append(out, diffs...)
	}
	if !sameSlots(orig.Slots, imported.Slots) {
		report("slots", orig.Slots, imported.Slots)
	}
	if !sameTickets(orig.TicketPurchases, imported.TicketPurchases) {
		report("ticket_purchases", orig.TicketPurchases, imported.TicketPurchases)
	}
	if !sameActivity(orig.Activity, imported.Activity) {
		report("activity", orig.Activity, imported.Activity)
	}
	return out
}

func sameMembers(a, b []memberRow) bool {
	if len(a) != len(b) {
		return false
	}
	sortM := func(s []memberRow) {
		sort.Slice(s, func(i, j int) bool { return s[i].Username < s[j].Username })
	}
	aa := append([]memberRow{}, a...)
	bb := append([]memberRow{}, b...)
	sortM(aa)
	sortM(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func sameAdventures(a, b []advRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Slug != b[i].Slug ||
			a[i].Title != b[i].Title ||
			a[i].Description != b[i].Description ||
			a[i].Author != b[i].Author ||
			a[i].Intro != b[i].Intro ||
			a[i].Outro != b[i].Outro ||
			a[i].Language != b[i].Language ||
			a[i].StrictAssets != b[i].StrictAssets ||
			!nullFloatEqual(a[i].StartLat, b[i].StartLat) ||
			!nullFloatEqual(a[i].StartLng, b[i].StartLng) ||
			a[i].StartLabel != b[i].StartLabel ||
			a[i].Accent != b[i].Accent ||
			a[i].Accent2 != b[i].Accent2 ||
			a[i].BG != b[i].BG ||
			a[i].Ink != b[i].Ink ||
			a[i].SubmitBG != b[i].SubmitBG ||
			a[i].AvatarMime != b[i].AvatarMime ||
			!bytes.Equal(a[i].AvatarBytes, b[i].AvatarBytes) ||
			a[i].CreatedByUsername != b[i].CreatedByUsername {
			return false
		}
	}
	return true
}

func diffAdvList(a, b []advRow) []string {
	var out []string
	if len(a) != len(b) {
		out = append(out, fmt.Sprintf("adventures count: want %d, got %d", len(a), len(b)))
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i].Slug != b[i].Slug {
			out = append(out, fmt.Sprintf("adventure[%d].slug: want %q, got %q", i, a[i].Slug, b[i].Slug))
		}
		if a[i].Title != b[i].Title {
			out = append(out, fmt.Sprintf("adventure[%s].title: want %q, got %q", a[i].Slug, a[i].Title, b[i].Title))
		}
		if a[i].Language != b[i].Language {
			out = append(out, fmt.Sprintf("adventure[%s].language: want %q, got %q", a[i].Slug, a[i].Language, b[i].Language))
		}
		if a[i].StrictAssets != b[i].StrictAssets {
			out = append(out, fmt.Sprintf("adventure[%s].strict_assets: want %v, got %v", a[i].Slug, a[i].StrictAssets, b[i].StrictAssets))
		}
		if !nullFloatEqual(a[i].StartLat, b[i].StartLat) {
			out = append(out, fmt.Sprintf("adventure[%s].start_lat: want %+v, got %+v", a[i].Slug, a[i].StartLat, b[i].StartLat))
		}
		if a[i].Accent != b[i].Accent {
			out = append(out, fmt.Sprintf("adventure[%s].accent_color: want %q, got %q", a[i].Slug, a[i].Accent, b[i].Accent))
		}
		if a[i].AvatarMime != b[i].AvatarMime {
			out = append(out, fmt.Sprintf("adventure[%s].avatar_mime: want %q, got %q", a[i].Slug, a[i].AvatarMime, b[i].AvatarMime))
		}
		if !bytes.Equal(a[i].AvatarBytes, b[i].AvatarBytes) {
			out = append(out, fmt.Sprintf("adventure[%s].avatar_bytes len: want %d, got %d", a[i].Slug, len(a[i].AvatarBytes), len(b[i].AvatarBytes)))
		}
		if a[i].CreatedByUsername != b[i].CreatedByUsername {
			out = append(out, fmt.Sprintf("adventure[%s].created_by_username: want %q, got %q", a[i].Slug, a[i].CreatedByUsername, b[i].CreatedByUsername))
		}
	}
	return out
}

func sameAssignments(a, b []assignRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameEvents(a, b []eventRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameEvent(a[i], b[i]) {
			return false
		}
	}
	return true
}

func sameEvent(a, b eventRow) bool {
	return a.LiveToken == b.LiveToken &&
		a.AdventureSlug == b.AdventureSlug &&
		a.Name == b.Name &&
		a.State == b.State &&
		a.ScheduledAt == b.ScheduledAt &&
		a.StartedAt == b.StartedAt &&
		a.CompletedAt == b.CompletedAt &&
		a.CancelledAt == b.CancelledAt &&
		a.AttestedNoMinorsAt == b.AttestedNoMinorsAt &&
		a.PublicForSale == b.PublicForSale &&
		a.PublicBlurb == b.PublicBlurb &&
		a.TicketPriceCents == b.TicketPriceCents &&
		a.TicketCurrency == b.TicketCurrency &&
		a.TicketTeamSize == b.TicketTeamSize &&
		a.CoverMime == b.CoverMime &&
		bytes.Equal(a.CoverBytes, b.CoverBytes) &&
		a.HasAssignment == b.HasAssignment &&
		a.CreatedByUsername == b.CreatedByUsername
}

func diffEventList(a, b []eventRow) []string {
	var out []string
	if len(a) != len(b) {
		out = append(out, fmt.Sprintf("events count: want %d, got %d", len(a), len(b)))
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		k := a[i].LiveToken
		if a[i].State != b[i].State {
			out = append(out, fmt.Sprintf("event[%s].state: want %q, got %q", k, a[i].State, b[i].State))
		}
		if a[i].AdventureSlug != b[i].AdventureSlug {
			out = append(out, fmt.Sprintf("event[%s].adventure_slug: want %q, got %q", k, a[i].AdventureSlug, b[i].AdventureSlug))
		}
		if a[i].Name != b[i].Name {
			out = append(out, fmt.Sprintf("event[%s].name: want %q, got %q", k, a[i].Name, b[i].Name))
		}
		if a[i].PublicForSale != b[i].PublicForSale {
			out = append(out, fmt.Sprintf("event[%s].public_for_sale: want %v, got %v", k, a[i].PublicForSale, b[i].PublicForSale))
		}
		if a[i].PublicBlurb != b[i].PublicBlurb {
			out = append(out, fmt.Sprintf("event[%s].public_blurb: want %q, got %q", k, a[i].PublicBlurb, b[i].PublicBlurb))
		}
		if a[i].StartedAt != b[i].StartedAt {
			out = append(out, fmt.Sprintf("event[%s].started_at: want %q, got %q", k, a[i].StartedAt, b[i].StartedAt))
		}
		if a[i].CompletedAt != b[i].CompletedAt {
			out = append(out, fmt.Sprintf("event[%s].completed_at: want %q, got %q", k, a[i].CompletedAt, b[i].CompletedAt))
		}
		if a[i].CancelledAt != b[i].CancelledAt {
			out = append(out, fmt.Sprintf("event[%s].cancelled_at: want %q, got %q", k, a[i].CancelledAt, b[i].CancelledAt))
		}
		if a[i].HasAssignment != b[i].HasAssignment {
			out = append(out, fmt.Sprintf("event[%s].has_assignment: want %v, got %v", k, a[i].HasAssignment, b[i].HasAssignment))
		}
		if a[i].CoverMime != b[i].CoverMime {
			out = append(out, fmt.Sprintf("event[%s].cover_mime: want %q, got %q", k, a[i].CoverMime, b[i].CoverMime))
		}
		if !bytes.Equal(a[i].CoverBytes, b[i].CoverBytes) {
			out = append(out, fmt.Sprintf("event[%s].cover_bytes len: want %d, got %d", k, len(a[i].CoverBytes), len(b[i].CoverBytes)))
		}
		if a[i].CreatedByUsername != b[i].CreatedByUsername {
			out = append(out, fmt.Sprintf("event[%s].created_by_username: want %q, got %q", k, a[i].CreatedByUsername, b[i].CreatedByUsername))
		}
	}
	return out
}

func sameTeams(a, b []teamRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Token != b[i].Token ||
			a[i].RecapToken != b[i].RecapToken ||
			a[i].EventLiveToken != b[i].EventLiveToken ||
			a[i].Name != b[i].Name ||
			a[i].Email != b[i].Email ||
			a[i].WithdrawnAt != b[i].WithdrawnAt ||
			a[i].CompletedAt != b[i].CompletedAt ||
			a[i].AvatarMime != b[i].AvatarMime ||
			!bytes.Equal(a[i].AvatarBytes, b[i].AvatarBytes) {
			return false
		}
	}
	return true
}

func sameActivity(a, b []activityRow) bool {
	if len(a) != len(b) {
		return false
	}
	// Activity is ordered by primary key on snapshot, but we sort
	// by (event_live_token, kind, created_at) to get a stable
	// comparison that doesn't depend on the destination's row id
	// ordering matching the source's.
	sortAct := func(s []activityRow) {
		sort.Slice(s, func(i, j int) bool {
			if s[i].EventLiveToken != s[j].EventLiveToken {
				return s[i].EventLiveToken < s[j].EventLiveToken
			}
			if s[i].Kind != s[j].Kind {
				return s[i].Kind < s[j].Kind
			}
			return s[i].CreatedAt < s[j].CreatedAt
		})
	}
	aa := append([]activityRow{}, a...)
	bb := append([]activityRow{}, b...)
	sortAct(aa)
	sortAct(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func diffTeamList(a, b []teamRow) []string {
	var out []string
	if len(a) != len(b) {
		out = append(out, fmt.Sprintf("teams count: want %d, got %d", len(a), len(b)))
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		k := a[i].Token
		if a[i].RecapToken != b[i].RecapToken {
			out = append(out, fmt.Sprintf("team[%s].recap_token: want %q, got %q", k, a[i].RecapToken, b[i].RecapToken))
		}
		if a[i].Name != b[i].Name {
			out = append(out, fmt.Sprintf("team[%s].name: want %q, got %q", k, a[i].Name, b[i].Name))
		}
		if a[i].WithdrawnAt != b[i].WithdrawnAt {
			out = append(out, fmt.Sprintf("team[%s].withdrawn_at: want %q, got %q", k, a[i].WithdrawnAt, b[i].WithdrawnAt))
		}
		if a[i].CompletedAt != b[i].CompletedAt {
			out = append(out, fmt.Sprintf("team[%s].completed_at: want %q, got %q", k, a[i].CompletedAt, b[i].CompletedAt))
		}
		if a[i].AvatarMime != b[i].AvatarMime {
			out = append(out, fmt.Sprintf("team[%s].avatar_mime: want %q, got %q", k, a[i].AvatarMime, b[i].AvatarMime))
		}
		if !bytes.Equal(a[i].AvatarBytes, b[i].AvatarBytes) {
			out = append(out, fmt.Sprintf("team[%s].avatar_bytes len: want %d, got %d", k, len(a[i].AvatarBytes), len(b[i].AvatarBytes)))
		}
	}
	return out
}

func sameSlots(a, b []slotRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameTickets(a, b []ticketRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func nullFloatEqual(a, b sql.NullFloat64) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Float64 == b.Float64
}

// (uses min from invitations_test.go in this same package)

// TestOrgBundle_RichRoundTrip is the comprehensive trust-builder.
// Seeds the rich fixture, takes a snapshot of the source org,
// exports + imports, takes a snapshot of the imported org, and
// fails with a per-mismatch report on every column that didn't
// round-trip cleanly. Also asserts the Bystander org is untouched
// (no leakage, no side-effects).
func TestOrgBundle_RichRoundTrip(t *testing.T) {
	db, h, ids := richFixture(t)
	ctx := context.Background()

	// Pre-snapshot of the source org and Bystander.
	orig := snapshotOrg(t, db, ids.acme)
	bystanderBefore := snapshotOrg(t, db, ids.bystander)

	// Export.
	env, files, err := h.ExportOrgEnvelope(ctx, ids.acme)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Cross-org leakage check at the bundle level: the manifest
	// must NOT carry any of Bystander's data — adventure slug,
	// event live token, team token, or slot token.
	for _, a := range env.Adventures {
		if a.Slug == ids.advBystander {
			t.Fatalf("export: bystander adventure %q leaked into Acme bundle", a.Slug)
		}
	}
	for _, ev := range env.Events {
		if ev.LiveToken == "live-tok-bystander" {
			t.Fatalf("export: bystander event leaked into Acme bundle")
		}
		for _, tm := range ev.Teams {
			if tm.Token == "team-tok-bystander" {
				t.Fatalf("export: bystander team leaked into Acme bundle")
			}
			for _, s := range tm.Slots {
				if s.Token == "slot-tok-bystander" {
					t.Fatalf("export: bystander slot leaked into Acme bundle")
				}
			}
		}
	}

	var zipBuf bytes.Buffer
	if err := admin.WriteOrgBundle(&zipBuf, env, files); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	// Wipe the source org so the importer has to mint a fresh one.
	// CASCADE drops members / adventures / events / teams / slots.
	mustExec(t, db, `DELETE FROM organizations WHERE id = $1`, ids.acme)

	summary, err := admin.ImportOrgBundle(ctx, db, zipBuf.Bytes())
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	imported := snapshotOrg(t, db, summary.OrgID)
	mismatches := diffSnapshots(orig, imported)
	if len(mismatches) > 0 {
		t.Errorf("round-trip mismatches (%d):", len(mismatches))
		for _, m := range mismatches {
			t.Errorf("  - %s", m)
		}
	}

	// Bystander must be byte-for-byte the same as before — the
	// import shouldn't have touched it.
	bystanderAfter := snapshotOrg(t, db, ids.bystander)
	if bystanderMismatches := diffSnapshots(bystanderBefore, bystanderAfter); len(bystanderMismatches) > 0 {
		t.Errorf("bystander org mutated by Acme migration (%d mismatches):", len(bystanderMismatches))
		for _, m := range bystanderMismatches {
			t.Errorf("  - %s", m)
		}
	}

	// Sentinel checks against the original counts so any future
	// schema addition (a new column / new child table) lights up
	// here as a count mismatch rather than silently round-tripping
	// zero rows because nobody added the export query.
	expect := map[string]int{
		"members":          2, // boss org_admin + minion org_member
		"adventures":       2, // linear-quest + race-relay
		"assignments":      2,
		"events":           5, // scheduled + running + completed + cancelled + quest
		"teams":            4, // mid + completed + withdrawn + quest-buyer
		"slots":            7, // 3 mid-team + 4 quest-buyer
		"ticket_purchases": 3, // paid + pending + failed
		"activity":         3, // team_started + answer_correct + event_paused
	}
	got := map[string]int{
		"members":          len(imported.Members),
		"adventures":       len(imported.Adventures),
		"assignments":      len(imported.Assignments),
		"events":           len(imported.Events),
		"teams":            len(imported.Teams),
		"slots":            len(imported.Slots),
		"ticket_purchases": len(imported.TicketPurchases),
		"activity":         len(imported.Activity),
	}
	for k, want := range expect {
		if got[k] != want {
			t.Errorf("imported.%s count: got %d, want %d", k, got[k], want)
		}
	}
}
