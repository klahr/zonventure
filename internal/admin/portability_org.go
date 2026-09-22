package admin

// Org-level export / import — the migration-bundle format used to
// move a whole org's data from a self-host instance into the hosted
// product (or back). Format `zonventure-org-v1`.
//
// The bundle nests one per-adventure subdirectory matching the
// existing `zonventure-adventure-v1` shape byte-for-byte, so the
// asset writers and the per-adventure importer (`buildExport` /
// `ImportEnvelopeJSON`) are reused, not duplicated. Beyond
// adventures, the manifest carries: org metadata, members,
// adventure-assignments, events, teams, player slots, and ticket
// purchases — enough for the migrated org to keep operating
// (existing token-bearing URLs still resolve to the right team /
// slot / event / live page on the new instance).
//
// Out of scope for v1: event_activity, team_lane_orderings,
// location_pings, answer_attempts, audit log slice. These are
// either telemetry that regenerates as players play, or live-state
// that doesn't survive a cutover anyway. They land in subsequent
// rounds when there's a customer signal.
//
// Tokens that MUST round-trip verbatim (operators have these in
// printed QR codes, emailed magic links, bookmarks):
//   • events.live_token
//   • teams.token, teams.recap_token
//   • player_slots.token
//   • adventures.slug (TEXT FK target)
//
// Tokens that DON'T round-trip (re-mint on next login):
//   • sessions.token, slot_sessions.token, mcp_tokens.token_hash
//
// Stripe identifiers:
//   • organizations.stripe_* — cleared on import; the org re-onboards
//     Connect on the destination instance.
//   • ticket_purchases.stripe_* — kept as historical audit data; the
//     refund UI flags pre-migration charges as legacy.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/orgslug"
)

// orgExportFormat tags the org-bundle manifest. Bumped per
// breaking-change, mirroring `adventureExportFormat`.
const orgExportFormat = "zonventure-org-v1"

// maxOrgImportZipBytes caps the multipart upload. Larger than a
// single-adventure zip because the bundle carries every adventure
// the org owns plus per-event covers, but still small enough that
// one bad upload can't OOM the server.
const maxOrgImportZipBytes int64 = 500 << 20 // 500 MiB

// orgExportEnvelope is the top-level manifest written to org.json.
// All FKs use idx-based references so the importer mints fresh
// primary keys without surprises.
type orgExportEnvelope struct {
	Format      string                       `json:"format"`
	ExportedAt  string                       `json:"exported_at"`
	Org         orgExportOrg                 `json:"org"`
	Members     []orgExportMember            `json:"members,omitempty"`
	Adventures  []orgExportAdvRef            `json:"adventures,omitempty"`
	Assignments []orgExportAssignment        `json:"assignments,omitempty"`
	Events      []orgExportEvent             `json:"events,omitempty"`
}

type orgExportOrg struct {
	Name     string `json:"name"`
	LogoFile string `json:"logo_file,omitempty"`
	LogoMime string `json:"logo_mime,omitempty"`
}

// orgExportMember captures an org_membership + the underlying user
// row. Password hashes are NOT migrated (forces a fresh login on
// the destination, which is the right posture for any cutover —
// session cookies are domain-bound and break either way). Email
// verification status is preserved so verified accounts don't
// re-prompt.
type orgExportMember struct {
	Username      string `json:"username"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Role          string `json:"role"` // "org_admin" | "org_member"
}

// orgExportAdvRef is a stub in the manifest pointing at a per-
// adventure subdirectory in the zip. The actual adventure data
// lives in `adventures/<slug>/adventure.json` (manifest matching
// the single-adventure format) plus its asset files. Authorship
// (`adventures.created_by_user_id`) is bundled here as the author's
// username so the per-adventure inner manifest can stay byte-for-
// byte compatible with the single-adventure format.
type orgExportAdvRef struct {
	Slug              string `json:"slug"`
	CreatedByUsername string `json:"created_by_username,omitempty"`
}

type orgExportAssignment struct {
	Idx           int    `json:"idx"`
	AdventureSlug string `json:"adventure_slug"`
	MaxPlayers    int    `json:"max_players"`
}

type orgExportEvent struct {
	Idx                int                       `json:"idx"`
	AdventureSlug      string                    `json:"adventure_slug"`
	AssignmentIdx      *int                      `json:"assignment_idx,omitempty"`
	Name               string                    `json:"name,omitempty"`
	State              string                    `json:"state"`
	LiveToken          string                    `json:"live_token"` // preserved
	CreatedByUsername  string                    `json:"created_by_username,omitempty"`
	PublicForSale      bool                      `json:"public_for_sale,omitempty"`
	PublicBlurb        string                    `json:"public_blurb,omitempty"`
	TicketPriceCents   int64                     `json:"ticket_price_cents,omitempty"`
	TicketCurrency     string                    `json:"ticket_currency,omitempty"`
	TicketTeamSize     int                       `json:"ticket_team_size,omitempty"`
	ScheduledAt        string                    `json:"scheduled_at,omitempty"`
	StartedAt          string                    `json:"started_at,omitempty"`
	CompletedAt        string                    `json:"completed_at,omitempty"`
	CancelledAt        string                    `json:"cancelled_at,omitempty"`
	AttestedNoMinorsAt string                    `json:"attested_no_minors_at,omitempty"`
	CoverFile          string                    `json:"cover_file,omitempty"`
	CoverMime          string                    `json:"cover_mime,omitempty"`
	Teams              []orgExportTeam           `json:"teams,omitempty"`
	TicketPurchases    []orgExportTicketPurchase `json:"ticket_purchases,omitempty"`
	Activity           []orgExportActivity       `json:"activity,omitempty"`
}

// orgExportActivity is one event_activity row. Bundle keys actors by
// stable identifiers (username, slot token) rather than primary keys
// so the authorship survives the ID rebase.
type orgExportActivity struct {
	Kind          string `json:"kind"`
	Payload       string `json:"payload,omitempty"`
	CreatedAt     string `json:"created_at"`
	TeamToken     string `json:"team_token,omitempty"`
	ActorUsername string `json:"actor_username,omitempty"`
	ActorSlotToken string `json:"actor_slot_token,omitempty"`
}

type orgExportTeam struct {
	Idx              int                     `json:"idx"`
	Name             string                  `json:"name"`
	Token            string                  `json:"token"`       // preserved
	RecapToken       string                  `json:"recap_token"` // preserved
	Email            string                  `json:"email,omitempty"`
	WithdrawnAt      string                  `json:"withdrawn_at,omitempty"`
	CompletedAt      string                  `json:"completed_at,omitempty"`
	CurrentPageIndex int                     `json:"current_page_index"`
	// CurrentPagePosition + CurrentLaneIdx anchor the lane-aware
	// cursor (`teams.current_page_id` + `current_lane_id`) so an
	// in-flight team resumes on the right puzzle on the destination
	// instead of bouncing to the adventure's first page. Both nil
	// for pre-start / completed / cancelled teams.
	CurrentPageIdx *int `json:"current_page_idx,omitempty"`
	CurrentLaneIdx *int `json:"current_lane_idx,omitempty"`
	AvatarFile     string                  `json:"avatar_file,omitempty"`
	AvatarMime     string                  `json:"avatar_mime,omitempty"`
	Slots          []orgExportSlot         `json:"slots,omitempty"`
	LaneOrderings  []orgExportLaneOrdering `json:"lane_orderings,omitempty"`
}

// orgExportLaneOrdering is one team_lane_orderings row, keyed by
// adventure-relative lane idx with page indices instead of raw
// page_id values. The importer re-resolves the indices against the
// freshly-minted page rows so the per-team puzzle order survives
// the ID rebase — without this an in-flight shuffled lane resets
// to lane order 0 on the destination, which players notice.
type orgExportLaneOrdering struct {
	LaneIdx  int   `json:"lane_idx"`
	PageIdxs []int `json:"page_idxs"`
}

type orgExportSlot struct {
	Idx         int    `json:"idx"`
	Name        string `json:"name"`
	Token       string `json:"token"` // preserved
	ExpiresAt   string `json:"expires_at,omitempty"`
	ClaimedAt   string `json:"claimed_at,omitempty"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

type orgExportTicketPurchase struct {
	BuyerEmail          string `json:"buyer_email"`
	TeamSize            int    `json:"team_size"`
	AmountCents         int64  `json:"amount_cents"`
	Currency            string `json:"currency"`
	State               string `json:"state"`
	PaidAt              string `json:"paid_at,omitempty"`
	StripeSessionID     string `json:"stripe_session_id,omitempty"`
	StripeChargeID      string `json:"stripe_charge_id,omitempty"`
	StripeInvoiceURL    string `json:"stripe_invoice_url,omitempty"`
	ApplicationFeeCents int64  `json:"application_fee_cents,omitempty"`
	TeamIdx             *int   `json:"team_idx,omitempty"` // index into the parent event's Teams[]
}

/* ---------------- Org export ---------------- */

// ExportOrgEnvelope builds the bundle in memory and returns the
// manifest JSON + the asset files. Splitting this from the HTTP
// handler keeps it easy to call from the CLI fallback and from
// tests (which assert byte-for-byte round-trip against the
// importer).
func (h *Handlers) ExportOrgEnvelope(ctx context.Context, orgID int64) (orgExportEnvelope, []exportFile, error) {
	var env orgExportEnvelope
	env.Format = orgExportFormat
	env.ExportedAt = time.Now().UTC().Format(time.RFC3339)

	// 1. Org row + logo blob.
	var (
		logoMime sql.NullString
		logoBlob []byte
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT name, logo_mime_type, logo_content
		FROM organizations WHERE id = $1
	`, orgID).Scan(&env.Org.Name, &logoMime, &logoBlob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return env, nil, fmt.Errorf("org %d: not found", orgID)
		}
		return env, nil, fmt.Errorf("load org: %w", err)
	}

	files := []exportFile{}
	if logoMime.Valid && len(logoBlob) > 0 {
		ext := extForMime(logoMime.String)
		name := "logo" + ext
		files = append(files, exportFile{name: name, bytes: logoBlob})
		env.Org.LogoFile = name
		env.Org.LogoMime = logoMime.String
	}

	// 2. Members (org_memberships + users). The post-multi-org
	// schema (migration 0020) carries the role on org_memberships,
	// not on users; users itself has no display_name column either.
	memRows, err := h.DB.QueryContext(ctx, `
		SELECT u.username, COALESCE(u.email, ''), u.email_verified, m.role
		FROM org_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY m.role DESC, u.username
	`, orgID)
	if err != nil {
		return env, nil, fmt.Errorf("load members: %w", err)
	}
	defer memRows.Close()
	for memRows.Next() {
		var m orgExportMember
		var verified int
		if err := memRows.Scan(&m.Username, &m.Email, &verified, &m.Role); err != nil {
			return env, nil, fmt.Errorf("scan member: %w", err)
		}
		m.EmailVerified = verified != 0
		env.Members = append(env.Members, m)
	}
	if err := memRows.Err(); err != nil {
		return env, nil, fmt.Errorf("members rows: %w", err)
	}

	// 3. Adventures — delegate to the existing single-adventure
	// builder, prefixing every asset filename with the per-
	// adventure subdir so the bundle stays one flat zip. The
	// authorship column (`adventures.created_by_user_id`) is
	// resolved to a username in the outer manifest so authorship
	// survives the ID rebase without changing the per-adventure
	// format.
	advRows, err := h.DB.QueryContext(ctx, `
		SELECT a.id, a.slug, COALESCE(u.username, '')
		FROM adventures a
		LEFT JOIN users u ON u.id = a.created_by_user_id
		WHERE a.org_id = $1 ORDER BY a.slug
	`, orgID)
	if err != nil {
		return env, nil, fmt.Errorf("list adventures: %w", err)
	}
	type advEntry struct {
		id     int64
		slug   string
		author string
	}
	var advs []advEntry
	for advRows.Next() {
		var a advEntry
		if err := advRows.Scan(&a.id, &a.slug, &a.author); err != nil {
			advRows.Close()
			return env, nil, fmt.Errorf("scan adventure: %w", err)
		}
		advs = append(advs, a)
	}
	advRows.Close()
	if err := advRows.Err(); err != nil {
		return env, nil, fmt.Errorf("adventure rows: %w", err)
	}
	for _, a := range advs {
		advEnv, advFiles, err := h.buildExport(ctx, a.id)
		if err != nil {
			return env, nil, fmt.Errorf("export adventure %s: %w", a.slug, err)
		}
		manifestBytes, mErr := json.MarshalIndent(advEnv, "", "  ")
		if mErr != nil {
			return env, nil, fmt.Errorf("encode adventure manifest %s: %w", a.slug, mErr)
		}
		dir := "adventures/" + a.slug + "/"
		files = append(files, exportFile{name: dir + "adventure.json", bytes: manifestBytes})
		for _, f := range advFiles {
			files = append(files, exportFile{name: dir + f.name, bytes: f.bytes})
		}
		env.Adventures = append(env.Adventures, orgExportAdvRef{
			Slug:              a.slug,
			CreatedByUsername: a.author,
		})
	}

	// 4. Adventure assignments (so events can FK back to the right
	// purchased capacity record on the destination).
	assignIdxByID := map[int64]int{}
	assignRows, err := h.DB.QueryContext(ctx, `
		SELECT id, adventure_slug, max_players
		FROM adventure_assignments
		WHERE org_id = $1
		ORDER BY id
	`, orgID)
	if err != nil {
		return env, nil, fmt.Errorf("load assignments: %w", err)
	}
	for assignRows.Next() {
		var (
			id   int64
			a    orgExportAssignment
		)
		if err := assignRows.Scan(&id, &a.AdventureSlug, &a.MaxPlayers); err != nil {
			assignRows.Close()
			return env, nil, fmt.Errorf("scan assignment: %w", err)
		}
		a.Idx = len(env.Assignments)
		assignIdxByID[id] = a.Idx
		env.Assignments = append(env.Assignments, a)
	}
	assignRows.Close()
	if err := assignRows.Err(); err != nil {
		return env, nil, fmt.Errorf("assignment rows: %w", err)
	}

	// 5. Events + nested teams, slots, ticket_purchases.
	if err := h.exportOrgEvents(ctx, orgID, &env, &files, assignIdxByID); err != nil {
		return env, nil, err
	}

	return env, files, nil
}

func (h *Handlers) exportOrgEvents(ctx context.Context, orgID int64, env *orgExportEnvelope, files *[]exportFile, assignIdxByID map[int64]int) error {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT e.id, e.adventure_slug, COALESCE(e.name, ''), e.state, e.live_token,
		       COALESCE(u.username, ''),
		       COALESCE(e.assignment_id, 0),
		       e.public_for_sale, COALESCE(e.public_blurb, ''),
		       COALESCE(e.ticket_price_cents, 0), COALESCE(e.ticket_currency, ''), COALESCE(e.ticket_team_size, 0),
		       COALESCE(e.scheduled_at, ''), COALESCE(e.started_at, ''), COALESCE(e.completed_at, ''), COALESCE(e.cancelled_at, ''),
		       COALESCE(e.attested_no_minors_at, ''),
		       e.cover_image_mime_type, e.cover_image_content
		FROM events e
		LEFT JOIN users u ON u.id = e.created_by_user_id
		WHERE e.org_id = $1
		ORDER BY e.id
	`, orgID)
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	defer rows.Close()

	type evEntry struct {
		id  int64
		idx int
	}
	var events []evEntry
	for rows.Next() {
		var (
			id           int64
			ev           orgExportEvent
			assignmentID int64
			coverMime    sql.NullString
			coverBlob    []byte
		)
		if err := rows.Scan(
			&id, &ev.AdventureSlug, &ev.Name, &ev.State, &ev.LiveToken,
			&ev.CreatedByUsername,
			&assignmentID,
			&ev.PublicForSale, &ev.PublicBlurb,
			&ev.TicketPriceCents, &ev.TicketCurrency, &ev.TicketTeamSize,
			&ev.ScheduledAt, &ev.StartedAt, &ev.CompletedAt, &ev.CancelledAt,
			&ev.AttestedNoMinorsAt,
			&coverMime, &coverBlob,
		); err != nil {
			return fmt.Errorf("scan event: %w", err)
		}
		ev.Idx = len(env.Events)
		if assignmentID > 0 {
			if idx, ok := assignIdxByID[assignmentID]; ok {
				v := idx
				ev.AssignmentIdx = &v
			}
		}
		if coverMime.Valid && len(coverBlob) > 0 {
			ext := extForMime(coverMime.String)
			name := fmt.Sprintf("events/%d/cover%s", ev.Idx, ext)
			*files = append(*files, exportFile{name: name, bytes: coverBlob})
			ev.CoverFile = name
			ev.CoverMime = coverMime.String
		}
		env.Events = append(env.Events, ev)
		events = append(events, evEntry{id: id, idx: ev.Idx})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("event rows: %w", err)
	}

	// Per-event children — done after the main loop so we don't hold
	// two cursors open against the same connection.
	for _, e := range events {
		teams, teamIdxByID, err := h.exportOrgEventTeams(ctx, e.id, e.idx, files)
		if err != nil {
			return fmt.Errorf("export teams for event %d: %w", e.id, err)
		}
		env.Events[e.idx].Teams = teams

		purchases, err := h.exportOrgTicketPurchases(ctx, e.id, teamIdxByID)
		if err != nil {
			return fmt.Errorf("export purchases for event %d: %w", e.id, err)
		}
		env.Events[e.idx].TicketPurchases = purchases

		activity, err := h.exportOrgEventActivity(ctx, e.id, teamIdxByID)
		if err != nil {
			return fmt.Errorf("export activity for event %d: %w", e.id, err)
		}
		env.Events[e.idx].Activity = activity
	}
	return nil
}

// exportOrgEventActivity dumps the per-event audit log keyed by
// stable identifiers — team token, actor username, slot token —
// so the IDs can be re-resolved on import even though the
// underlying primary keys change.
func (h *Handlers) exportOrgEventActivity(ctx context.Context, eventID int64, teamIdxByID map[int64]int) ([]orgExportActivity, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT a.kind, a.payload, a.created_at,
		       COALESCE(t.token, ''),
		       COALESCE(u.username, ''),
		       COALESCE(s.token, '')
		FROM event_activity a
		LEFT JOIN teams t ON t.id = a.team_id
		LEFT JOIN users u ON u.id = a.actor_user_id
		LEFT JOIN player_slots s ON s.id = a.actor_slot_id
		WHERE a.event_id = $1
		ORDER BY a.id
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orgExportActivity
	for rows.Next() {
		var a orgExportActivity
		if err := rows.Scan(&a.Kind, &a.Payload, &a.CreatedAt,
			&a.TeamToken, &a.ActorUsername, &a.ActorSlotToken); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (h *Handlers) exportOrgEventTeams(ctx context.Context, eventID int64, eventIdx int, files *[]exportFile) ([]orgExportTeam, map[int64]int, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT t.id, t.name, t.token, t.recap_token, COALESCE(t.email, ''),
		       COALESCE(t.withdrawn_at, ''), COALESCE(t.completed_at, ''),
		       COALESCE(t.current_page_index, 0),
		       cur_p.idx, cur_l.idx,
		       t.avatar_mime_type, t.avatar_content
		FROM teams t
		LEFT JOIN adventure_pages cur_p ON cur_p.id = t.current_page_id
		LEFT JOIN adventure_lanes cur_l ON cur_l.id = t.current_lane_id
		WHERE t.event_id = $1
		ORDER BY t.id
	`, eventID)
	if err != nil {
		return nil, nil, fmt.Errorf("load teams: %w", err)
	}
	defer rows.Close()

	type teamEntry struct {
		id  int64
		idx int
	}
	var (
		teams       []orgExportTeam
		teamEntries []teamEntry
		idxByID     = map[int64]int{}
	)
	for rows.Next() {
		var (
			id        int64
			tm        orgExportTeam
			curPgIdx  sql.NullInt64
			curLnIdx  sql.NullInt64
			avMime    sql.NullString
			avBlob    []byte
		)
		if err := rows.Scan(&id, &tm.Name, &tm.Token, &tm.RecapToken, &tm.Email,
			&tm.WithdrawnAt, &tm.CompletedAt, &tm.CurrentPageIndex,
			&curPgIdx, &curLnIdx,
			&avMime, &avBlob,
		); err != nil {
			return nil, nil, fmt.Errorf("scan team: %w", err)
		}
		if curPgIdx.Valid {
			v := int(curPgIdx.Int64)
			tm.CurrentPageIdx = &v
		}
		if curLnIdx.Valid {
			v := int(curLnIdx.Int64)
			tm.CurrentLaneIdx = &v
		}
		tm.Idx = len(teams)
		idxByID[id] = tm.Idx
		if avMime.Valid && len(avBlob) > 0 {
			ext := extForMime(avMime.String)
			name := fmt.Sprintf("events/%d/teams/%d/avatar%s", eventIdx, tm.Idx, ext)
			*files = append(*files, exportFile{name: name, bytes: avBlob})
			tm.AvatarFile = name
			tm.AvatarMime = avMime.String
		}
		teams = append(teams, tm)
		teamEntries = append(teamEntries, teamEntry{id: id, idx: tm.Idx})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("team rows: %w", err)
	}

	// Slots + lane-orderings — second loop so we don't hold two
	// cursors at once.
	for _, e := range teamEntries {
		slotRows, err := h.DB.QueryContext(ctx, `
			SELECT name, token,
			       COALESCE(expires_at, ''), COALESCE(claimed_at, ''), COALESCE(last_login_at, '')
			FROM player_slots
			WHERE team_id = $1
			ORDER BY id
		`, e.id)
		if err != nil {
			return nil, nil, fmt.Errorf("load slots for team %d: %w", e.id, err)
		}
		var slots []orgExportSlot
		for slotRows.Next() {
			var s orgExportSlot
			if err := slotRows.Scan(&s.Name, &s.Token, &s.ExpiresAt, &s.ClaimedAt, &s.LastLoginAt); err != nil {
				slotRows.Close()
				return nil, nil, fmt.Errorf("scan slot: %w", err)
			}
			s.Idx = len(slots)
			slots = append(slots, s)
		}
		slotRows.Close()
		if err := slotRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("slot rows: %w", err)
		}
		teams[e.idx].Slots = slots

		// Lane orderings: per-team frozen permutations for shuffled
		// lanes. `team_lane_orderings.page_ids` is a JSONB array of
		// `adventure_pages.id` values; we resolve those to per-
		// adventure page indices so the importer can rebuild the
		// permutation against the freshly-minted page rows.
		loRows, err := h.DB.QueryContext(ctx, `
			SELECT lane.idx, ord.page_ids
			FROM team_lane_orderings ord
			JOIN adventure_lanes lane ON lane.id = ord.lane_id
			WHERE ord.team_id = $1
			ORDER BY lane.idx
		`, e.id)
		if err != nil {
			return nil, nil, fmt.Errorf("load orderings for team %d: %w", e.id, err)
		}
		for loRows.Next() {
			var (
				laneIdx  int
				rawIDs   []byte
			)
			if err := loRows.Scan(&laneIdx, &rawIDs); err != nil {
				loRows.Close()
				return nil, nil, fmt.Errorf("scan ordering: %w", err)
			}
			var pageIDs []int64
			if jerr := json.Unmarshal(rawIDs, &pageIDs); jerr != nil {
				loRows.Close()
				return nil, nil, fmt.Errorf("decode page_ids: %w", jerr)
			}
			pageIdxs, ferr := h.pageIDsToIdxs(ctx, pageIDs)
			if ferr != nil {
				loRows.Close()
				return nil, nil, fmt.Errorf("resolve page idxs: %w", ferr)
			}
			teams[e.idx].LaneOrderings = append(teams[e.idx].LaneOrderings, orgExportLaneOrdering{
				LaneIdx:  laneIdx,
				PageIdxs: pageIdxs,
			})
		}
		loRows.Close()
		if err := loRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("ordering rows: %w", err)
		}
	}
	return teams, idxByID, nil
}

// pageIDsToIdxs maps a slice of `adventure_pages.id` to the
// corresponding `adventure_pages.idx` values, preserving order.
// Used by the lane-ordering exporter so the bundle can describe
// per-team permutations without baking primary keys. We build a
// per-element IN clause rather than `= ANY($1)` because the
// pgx-stdlib driver doesn't unwrap Go slices for ANY automatically.
func (h *Handlers) pageIDsToIdxs(ctx context.Context, ids []int64) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	idxByID := map[int64]int{}
	for _, id := range ids {
		var idx int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT idx FROM adventure_pages WHERE id = $1`, id).Scan(&idx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Page disappeared between read and write; skip
				// silently — the runtime tolerates a permutation
				// with gaps (falls through to the lane's natural
				// order).
				continue
			}
			return nil, err
		}
		idxByID[id] = idx
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if idx, ok := idxByID[id]; ok {
			out = append(out, idx)
		}
	}
	return out, nil
}

func (h *Handlers) exportOrgTicketPurchases(ctx context.Context, eventID int64, teamIdxByID map[int64]int) ([]orgExportTicketPurchase, error) {
	rows, err := h.DB.QueryContext(ctx, `
		SELECT buyer_email, team_size, amount_cents, currency, state,
		       COALESCE(paid_at, ''),
		       COALESCE(stripe_session_id, ''), COALESCE(stripe_charge_id, ''), COALESCE(stripe_invoice_url, ''),
		       COALESCE(application_fee_cents, 0),
		       COALESCE(team_id, 0)
		FROM ticket_purchases
		WHERE event_id = $1
		ORDER BY id
	`, eventID)
	if err != nil {
		return nil, fmt.Errorf("load purchases: %w", err)
	}
	defer rows.Close()
	var out []orgExportTicketPurchase
	for rows.Next() {
		var (
			p      orgExportTicketPurchase
			teamID int64
		)
		if err := rows.Scan(&p.BuyerEmail, &p.TeamSize, &p.AmountCents, &p.Currency, &p.State,
			&p.PaidAt,
			&p.StripeSessionID, &p.StripeChargeID, &p.StripeInvoiceURL,
			&p.ApplicationFeeCents,
			&teamID,
		); err != nil {
			return nil, fmt.Errorf("scan purchase: %w", err)
		}
		if teamID > 0 {
			if idx, ok := teamIdxByID[teamID]; ok {
				v := idx
				p.TeamIdx = &v
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("purchase rows: %w", err)
	}
	return out, nil
}

// WriteOrgBundle streams a built envelope+files into a zip writer.
// Used by the HTTP handler and the CLI fallback so both paths
// produce byte-identical archives.
func WriteOrgBundle(w io.Writer, env orgExportEnvelope, files []exportFile) error {
	zw := zip.NewWriter(w)
	mw, err := zw.Create("org.json")
	if err != nil {
		return fmt.Errorf("zip manifest: %w", err)
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return fmt.Errorf("zip %s: %w", f.name, err)
		}
		if _, err := fw.Write(f.bytes); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return zw.Close()
}

/* ---------------- Org import ---------------- */

// ImportOrgBundle reads an org-bundle zip from `data` and creates a
// fresh org with all the data replayed under new primary keys but
// with all preserved tokens and adventure slugs intact. Returns the
// new org id + a summary.
//
// On slug collision (an adventure in the bundle already exists in
// the destination DB) the importer rejects with 409, mirroring the
// single-adventure importer's posture. Operators can resolve by
// renaming on the source side or — in a future round — passing a
// per-bundle slug-prefix override.
type OrgImportSummary struct {
	OrgID       int64 `json:"org_id"`
	Adventures  int   `json:"adventures"`
	Members     int   `json:"members"`
	Events      int   `json:"events"`
	Teams       int   `json:"teams"`
	Slots       int   `json:"slots"`
	Purchases   int   `json:"ticket_purchases"`
	Assignments int   `json:"assignments"`
}

func ImportOrgBundle(ctx context.Context, db *sql.DB, data []byte) (OrgImportSummary, error) {
	manifest, assets, err := splitOrgImportZip(data)
	if err != nil {
		return OrgImportSummary{}, err
	}
	var env orgExportEnvelope
	if jerr := json.Unmarshal(manifest, &env); jerr != nil {
		return OrgImportSummary{}, badRequestf("manifest is not valid JSON: %v", jerr)
	}
	if env.Format != orgExportFormat {
		return OrgImportSummary{}, badRequestf("unsupported manifest format %q (want %q)", env.Format, orgExportFormat)
	}

	summary := OrgImportSummary{}

	// Phase A — org row, committed immediately so the per-adventure
	// importer (which opens its own transaction) can see the row
	// when validating org-scoped foreign keys. Stripe Connect
	// identifiers stay nil; the org re-onboards on the destination.
	// `organizations.name` is globally unique; re-imports of the
	// same bundle (or two self-hosts with the same org name landing
	// on the same hosted instance) would otherwise hit a 23505. The
	// importer disambiguates by appending " (imported)" / " (imported 2)"
	// / ... until it lands on a free name. Operators can rename
	// from the admin Settings page after the import.
	resolvedName, err := resolveOrgName(ctx, db, env.Org.Name)
	if err != nil {
		return summary, fmt.Errorf("resolve org name: %w", err)
	}
	// Mint a slug from the resolved name so the imported org is
	// reachable at /o/{slug}/store. Reserve handles the "-2/-3"
	// suffix dance against existing slugs in this destination.
	slug, err := orgslug.Reserve(ctx, db, resolvedName)
	if err != nil {
		return summary, fmt.Errorf("reserve slug: %w", err)
	}
	var orgID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations(name, slug, logo_mime_type, logo_content, logo_size_bytes)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, resolvedName, slug, sqlNullStr(env.Org.LogoMime), assets[env.Org.LogoFile], int64(len(assets[env.Org.LogoFile]))).Scan(&orgID); err != nil {
		return summary, fmt.Errorf("create org: %w", err)
	}
	summary.OrgID = orgID

	// Cleanup on later failure: delete the org row so we don't
	// leave half-imported state behind. CASCADE drops every
	// dependency (memberships, adventures, events, teams, …).
	cleanupOrg := func() {
		if _, err := db.ExecContext(context.Background(),
			`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
			logx.Warn("admin.org_bundle.import.cleanup_failed",
				"org_id", orgID, "err", err.Error())
		}
	}

	// Phase B — members, in their own short transaction so user +
	// membership inserts roll back together on a single failure.
	if err := importOrgMembers(ctx, db, orgID, env.Members, &summary); err != nil {
		cleanupOrg()
		return summary, err
	}

	// Phase C — adventures via the per-adventure importer, each in
	// its own transaction. Slug collisions surface as the
	// importer's *importError with status 409.
	//
	// Authorship (`adventures.created_by_user_id`) is preserved
	// per-adventure: we look up the user id by username from the
	// freshly-imported members. Falls back to a default org_admin
	// when the bundle didn't carry an author or the user isn't a
	// member of this org (rare, but possible if the source had a
	// cross-org contributor we didn't migrate). With no members at
	// all the importer fails — adventures need an authoring user
	// because the FK on `adventures.created_by_user_id` is real.
	userIDByUsername, defaultUserID, err := buildUserLookup(ctx, db, orgID)
	if err != nil {
		cleanupOrg()
		return summary, err
	}
	if defaultUserID == 0 {
		cleanupOrg()
		return summary, badRequestf("bundle has no members; cannot attribute adventures")
	}
	resolveAuthor := func(username string) int64 {
		if username == "" {
			return defaultUserID
		}
		if id, ok := userIDByUsername[username]; ok {
			return id
		}
		return defaultUserID
	}
	for _, advRef := range env.Adventures {
		dir := "adventures/" + advRef.Slug + "/"
		manifestKey := dir + "adventure.json"
		manifestBytes, ok := assets[manifestKey]
		if !ok {
			cleanupOrg()
			return summary, badRequestf("missing %s in archive", manifestKey)
		}
		subAssets := map[string][]byte{}
		for k, v := range assets {
			if !strings.HasPrefix(k, dir) || k == manifestKey {
				continue
			}
			subAssets[strings.TrimPrefix(k, dir)] = v
		}
		authorID := resolveAuthor(advRef.CreatedByUsername)
		pseudoUser := auth.User{ID: authorID, Role: auth.RoleAdmin, OrgID: &orgID}
		if _, _, _, err := importAdventureForOrg(ctx, db, manifestBytes, subAssets, pseudoUser); err != nil {
			cleanupOrg()
			return summary, fmt.Errorf("import adventure %s: %w", advRef.Slug, err)
		}
		summary.Adventures++
	}

	// Phase D — assignments + events + teams + slots + purchases
	// in one transaction so a failure mid-event doesn't leave a
	// partially-populated event around for the operator to puzzle
	// over. On failure we still cleanupOrg() to drop everything.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		cleanupOrg()
		return summary, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Assignments first so events can FK back.
	assignIDByIdx := map[int]int64{}
	for _, a := range env.Assignments {
		var newID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO adventure_assignments(org_id, adventure_slug, max_players)
			VALUES ($1, $2, $3) RETURNING id
		`, orgID, a.AdventureSlug, a.MaxPlayers).Scan(&newID); err != nil {
			cleanupOrg()
			return summary, fmt.Errorf("create assignment idx %d: %w", a.Idx, err)
		}
		assignIDByIdx[a.Idx] = newID
		summary.Assignments++
	}

	// Events + teams + slots + purchases.
	for _, ev := range env.Events {
		var assignArg any
		if ev.AssignmentIdx != nil {
			if id, ok := assignIDByIdx[*ev.AssignmentIdx]; ok {
				assignArg = id
			}
		}
		var coverMime sql.NullString
		var coverBytes []byte
		if ev.CoverFile != "" {
			if b, ok := assets[ev.CoverFile]; ok {
				coverBytes = b
				coverMime = sql.NullString{String: ev.CoverMime, Valid: true}
			}
		}
		var newEventID int64
		eventAuthorID := resolveAuthor(ev.CreatedByUsername)
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO events(adventure_slug, org_id, created_by_user_id, state,
				scheduled_at, assignment_id, live_token, name, attested_no_minors_at,
				public_for_sale, public_blurb,
				ticket_price_cents, ticket_currency, ticket_team_size,
				started_at, completed_at, cancelled_at,
				cover_image_mime_type, cover_image_content, cover_image_size_bytes)
			VALUES ($1, $2, $3, $4,
				$5, $6, $7, $8, $9,
				$10, $11,
				$12, $13, $14,
				$15, $16, $17,
				$18, $19, $20)
			RETURNING id
		`,
			ev.AdventureSlug, orgID, eventAuthorID, ev.State,
			sqlNullStr(ev.ScheduledAt), assignArg, ev.LiveToken, ev.Name, sqlNullStr(ev.AttestedNoMinorsAt),
			ev.PublicForSale, ev.PublicBlurb,
			nullableInt64(ev.TicketPriceCents), defaultStr(ev.TicketCurrency, "eur"), defaultInt(ev.TicketTeamSize, 5),
			sqlNullStr(ev.StartedAt), sqlNullStr(ev.CompletedAt), sqlNullStr(ev.CancelledAt),
			coverMime, coverBytes, int64(len(coverBytes)),
		).Scan(&newEventID); err != nil {
			cleanupOrg()
			return summary, fmt.Errorf("create event idx %d: %w", ev.Idx, err)
		}
		summary.Events++

		teamIDByIdx := map[int]int64{}
		// Map team token → new id and slot token → new id so the
		// activity-log replay (below) and ticket-purchase FK can
		// re-resolve actor identities after the ID rebase.
		teamIDByToken := map[string]int64{}
		slotIDByToken := map[string]int64{}
		for _, tm := range ev.Teams {
			var avMime sql.NullString
			var avBytes []byte
			if tm.AvatarFile != "" {
				if b, ok := assets[tm.AvatarFile]; ok {
					avBytes = b
					avMime = sql.NullString{String: tm.AvatarMime, Valid: true}
				}
			}
			var newTeamID int64
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO teams(event_id, name, token, recap_token, email,
					withdrawn_at, completed_at, current_page_index,
					avatar_mime_type, avatar_content, avatar_size_bytes)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				RETURNING id
			`,
				newEventID, tm.Name, tm.Token, tm.RecapToken, tm.Email,
				sqlNullStr(tm.WithdrawnAt), sqlNullStr(tm.CompletedAt), tm.CurrentPageIndex,
				avMime, avBytes, int64(len(avBytes)),
			).Scan(&newTeamID); err != nil {
				cleanupOrg()
				return summary, fmt.Errorf("create team idx %d on event %d: %w", tm.Idx, ev.Idx, err)
			}
			summary.Teams++
			teamIDByIdx[tm.Idx] = newTeamID
			teamIDByToken[tm.Token] = newTeamID

			for _, s := range tm.Slots {
				var newSlotID int64
				if err := tx.QueryRowContext(ctx, `
					INSERT INTO player_slots(team_id, name, token,
						expires_at, claimed_at, last_login_at)
					VALUES ($1, $2, $3, $4, $5, $6)
					RETURNING id
				`, newTeamID, s.Name, s.Token,
					sqlNullStr(s.ExpiresAt), sqlNullStr(s.ClaimedAt), sqlNullStr(s.LastLoginAt)).Scan(&newSlotID); err != nil {
					cleanupOrg()
					return summary, fmt.Errorf("create slot %d/%d/%d: %w", ev.Idx, tm.Idx, s.Idx, err)
				}
				summary.Slots++
				slotIDByToken[s.Token] = newSlotID
			}

			// Resolve current_page_id / current_lane_id against the
			// freshly-imported pages and lanes for this adventure,
			// so an in-flight team's cursor lands on the right
			// puzzle on the destination instead of resetting to
			// page 0.
			if tm.CurrentPageIdx != nil || tm.CurrentLaneIdx != nil {
				var pageID, laneID sql.NullInt64
				if tm.CurrentPageIdx != nil {
					if err := tx.QueryRowContext(ctx, `
						SELECT p.id FROM adventure_pages p
						JOIN adventures a ON a.id = p.adventure_id
						WHERE a.org_id = $1 AND a.slug = $2 AND p.idx = $3
					`, orgID, ev.AdventureSlug, *tm.CurrentPageIdx).Scan(&pageID.Int64); err == nil {
						pageID.Valid = true
					}
				}
				if tm.CurrentLaneIdx != nil {
					if err := tx.QueryRowContext(ctx, `
						SELECT l.id FROM adventure_lanes l
						JOIN adventures a ON a.id = l.adventure_id
						WHERE a.org_id = $1 AND a.slug = $2 AND l.idx = $3
					`, orgID, ev.AdventureSlug, *tm.CurrentLaneIdx).Scan(&laneID.Int64); err == nil {
						laneID.Valid = true
					}
				}
				if pageID.Valid || laneID.Valid {
					if _, err := tx.ExecContext(ctx, `
						UPDATE teams SET current_page_id = $1, current_lane_id = $2
						WHERE id = $3
					`, pageID, laneID, newTeamID); err != nil {
						cleanupOrg()
						return summary, fmt.Errorf("set team cursor %d/%d: %w", ev.Idx, tm.Idx, err)
					}
				}
			}

			// Lane orderings: per-team frozen permutations for the
			// shuffled lanes. Resolve each lane idx + page idxs to
			// the new lane_id + array of new page_ids for the
			// destination's freshly-minted rows.
			for _, lo := range tm.LaneOrderings {
				var laneID int64
				if err := tx.QueryRowContext(ctx, `
					SELECT l.id FROM adventure_lanes l
					JOIN adventures a ON a.id = l.adventure_id
					WHERE a.org_id = $1 AND a.slug = $2 AND l.idx = $3
				`, orgID, ev.AdventureSlug, lo.LaneIdx).Scan(&laneID); err != nil {
					cleanupOrg()
					return summary, fmt.Errorf("resolve lane %d for team %d: %w", lo.LaneIdx, tm.Idx, err)
				}
				pageIDs := make([]int64, 0, len(lo.PageIdxs))
				for _, idx := range lo.PageIdxs {
					var pid int64
					if err := tx.QueryRowContext(ctx, `
						SELECT p.id FROM adventure_pages p
						JOIN adventures a ON a.id = p.adventure_id
						WHERE a.org_id = $1 AND a.slug = $2 AND p.idx = $3
					`, orgID, ev.AdventureSlug, idx).Scan(&pid); err != nil {
						cleanupOrg()
						return summary, fmt.Errorf("resolve page %d for team %d lane %d: %w", idx, tm.Idx, lo.LaneIdx, err)
					}
					pageIDs = append(pageIDs, pid)
				}
				pageIDsJSON, _ := json.Marshal(pageIDs)
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO team_lane_orderings(team_id, lane_id, page_ids)
					VALUES ($1, $2, $3)
				`, newTeamID, laneID, string(pageIDsJSON)); err != nil {
					cleanupOrg()
					return summary, fmt.Errorf("insert lane ordering for team %d lane %d: %w", tm.Idx, lo.LaneIdx, err)
				}
			}
		}

		for _, p := range ev.TicketPurchases {
			var teamArg any
			if p.TeamIdx != nil {
				if id, ok := teamIDByIdx[*p.TeamIdx]; ok {
					teamArg = id
				}
			}
			// pre_migration=TRUE tags imported rows so the refund
			// handler refuses with a clear "use the pre-migration
			// Stripe dashboard" message — the stripe_charge_id we
			// preserved is on the source instance's Stripe account,
			// not ours, and calling Refund against it on this
			// instance's credentials would 404 or match an unrelated
			// charge.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ticket_purchases(event_id, buyer_email, team_size,
					amount_cents, currency, application_fee_cents,
					state, paid_at,
					stripe_session_id, stripe_charge_id, stripe_invoice_url,
					team_id, pre_migration)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, TRUE)
			`,
				newEventID, p.BuyerEmail, p.TeamSize,
				p.AmountCents, p.Currency, p.ApplicationFeeCents,
				p.State, sqlNullStr(p.PaidAt),
				sqlNullStr(p.StripeSessionID), sqlNullStr(p.StripeChargeID), sqlNullStr(p.StripeInvoiceURL),
				teamArg,
			); err != nil {
				cleanupOrg()
				return summary, fmt.Errorf("create purchase on event %d: %w", ev.Idx, err)
			}
			summary.Purchases++
		}

		// Activity log — done AFTER teams + slots are minted so we
		// can resolve team token → team id and slot token → slot id.
		// Actor user_id resolves through the same buildUserLookup
		// table the adventure / event authorship uses.
		for _, a := range ev.Activity {
			var teamArg, actorUserArg, actorSlotArg any
			if a.TeamToken != "" {
				if id, ok := teamIDByToken[a.TeamToken]; ok {
					teamArg = id
				}
			}
			if a.ActorUsername != "" {
				if id, ok := userIDByUsername[a.ActorUsername]; ok {
					actorUserArg = id
				}
			}
			if a.ActorSlotToken != "" {
				if id, ok := slotIDByToken[a.ActorSlotToken]; ok {
					actorSlotArg = id
				}
			}
			payload := a.Payload
			if payload == "" {
				payload = "{}"
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO event_activity(event_id, team_id, actor_user_id, actor_slot_id,
					kind, payload, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, newEventID, teamArg, actorUserArg, actorSlotArg,
				a.Kind, payload, defaultStr(a.CreatedAt, "")); err != nil {
				cleanupOrg()
				return summary, fmt.Errorf("create activity on event %d: %w", ev.Idx, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		cleanupOrg()
		return summary, fmt.Errorf("commit: %w", err)
	}
	logx.Info("admin.org_bundle.import",
		"org_id", orgID, "adventures", summary.Adventures, "events", summary.Events,
		"teams", summary.Teams, "slots", summary.Slots, "purchases", summary.Purchases)
	return summary, nil
}

// importAdventureForOrg wraps the per-adventure importer with an
// org-context user. The single-adventure importer derives org from
// the user's OrgID, so we synthesise a RoleAdmin user pinned to the
// destination org.
func importAdventureForOrg(ctx context.Context, db *sql.DB, manifest []byte, assets map[string][]byte, user auth.User) (int64, string, string, error) {
	return importEnvelopeJSON(ctx, db, manifest, "", user, assets, false)
}

// buildUserLookup returns a username → user_id map for the freshly-
// imported org plus the org's default authorship target (the first
// org_admin if any, otherwise the first member). Returns
// `defaultUserID == 0` when the org has no members.
func buildUserLookup(ctx context.Context, db *sql.DB, orgID int64) (map[string]int64, int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT u.username, u.id, m.role
		FROM org_memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY CASE m.role WHEN 'org_admin' THEN 0 ELSE 1 END, u.id
	`, orgID)
	if err != nil {
		return nil, 0, fmt.Errorf("look up users: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	var defaultID int64
	for rows.Next() {
		var (
			name string
			id   int64
			role string
		)
		if err := rows.Scan(&name, &id, &role); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		out[name] = id
		if defaultID == 0 {
			defaultID = id
		}
	}
	return out, defaultID, rows.Err()
}

// importOrgMembers creates the user rows (where missing) and the
// org_memberships rows for the migrated org. Wrapped in a tx so a
// partial member set can't half-apply on a constraint violation.
// Password hashes are deliberately NOT carried — every user
// recovers via the lost-link / forgot-password flow on first
// login on the destination instance.
func importOrgMembers(ctx context.Context, db *sql.DB, orgID int64, members []orgExportMember, summary *OrgImportSummary) error {
	if len(members) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("members: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, m := range members {
		var userID int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM users WHERE username = $1`, m.Username).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO users(username, email, email_verified)
				VALUES ($1, $2, $3) RETURNING id
			`, m.Username, sqlNullStr(m.Email), boolToInt(m.EmailVerified)).Scan(&userID); err != nil {
				return fmt.Errorf("create user %s: %w", m.Username, err)
			}
		} else if err != nil {
			return fmt.Errorf("lookup user %s: %w", m.Username, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO org_memberships(user_id, org_id, role)
			VALUES ($1, $2, $3)
		`, userID, orgID, m.Role); err != nil {
			return fmt.Errorf("create membership %s: %w", m.Username, err)
		}
		summary.Members++
	}
	return tx.Commit()
}

// splitOrgImportZip allows nested adventures/<slug>/ paths but
// otherwise mirrors the single-adventure splitter's posture — no
// `..` traversal, no backslashes, no leading slash. Returns the
// org-manifest bytes + a flat asset map keyed by full path.
func splitOrgImportZip(data []byte) (manifestJSON []byte, assets map[string][]byte, err error) {
	zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zerr != nil {
		return nil, nil, badRequestf("not a valid zip archive")
	}
	assets = map[string][]byte{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		if strings.ContainsAny(zf.Name, `\`) || strings.Contains(zf.Name, "..") || strings.HasPrefix(zf.Name, "/") {
			return nil, nil, badRequestf("archive contains a non-flat path")
		}
		b, rerr := readZipFile(zf, maxOrgImportZipBytes)
		if rerr != nil {
			return nil, nil, badRequestf("read %s: %v", zf.Name, rerr)
		}
		if zf.Name == "org.json" {
			manifestJSON = b
		} else {
			assets[zf.Name] = b
		}
	}
	if manifestJSON == nil {
		return nil, nil, badRequestf("missing org.json in archive")
	}
	return manifestJSON, assets, nil
}

/* ---------------- helpers ---------------- */

func sqlNullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// defaultStr returns dflt when v is empty — used for NOT-NULL DEFAULT
// columns where we want zero-value rows in the bundle to map to the
// schema default rather than violating the NOT NULL constraint.
func defaultStr(v, dflt string) string {
	if v == "" {
		return dflt
	}
	return v
}

func defaultInt(v, dflt int) int {
	if v == 0 {
		return dflt
	}
	return v
}

// resolveOrgName picks a free `organizations.name` for the import:
// the bundle's name as-is when available, otherwise appended with
// " (imported)" / " (imported 2)" / ... Caps at a generous 50 to
// stop a pathological loop on a wedged uniqueness check; in
// practice the second or third attempt always wins.
func resolveOrgName(ctx context.Context, db *sql.DB, base string) (string, error) {
	candidate := base
	for n := 0; n < 50; n++ {
		var taken bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM organizations WHERE name = $1)`, candidate).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		if n == 0 {
			candidate = base + " (imported)"
		} else {
			candidate = fmt.Sprintf("%s (imported %d)", base, n+1)
		}
	}
	return "", fmt.Errorf("could not find a free org name after 50 attempts (base %q)", base)
}
