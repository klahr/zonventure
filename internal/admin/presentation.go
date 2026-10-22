package admin

// Review for the organisation's page and the adventure's presentation
// (migration 0128). Once an unverified organisation has something for
// sale, an edit to these is held back as a proposal, and only an
// approval writes it to the organisation or the adventure: everything
// that reads them — pages, the player app, emails, calendar invites —
// then shows approved content. Before anything is for sale, edits apply
// at once and are reviewed with the first listing.
//
//	GET    /api/admin/orgs/me/proposal                    the org page's open proposal
//	POST   /api/admin/orgs/me/proposal/submit             {attest}
//	DELETE /api/admin/orgs/me/proposal                    discard it
//	GET    /api/admin/orgs/me/proposal/{image|cover}      a proposed picture
//	GET    /api/admin/adventures/{slug}/proposal          the adventure's open proposal
//	POST   /api/admin/adventures/{slug}/proposal/submit   {attest}
//	DELETE /api/admin/adventures/{slug}/proposal          discard it
//	GET    /api/admin/adventures/{slug}/proposal/image    the proposed icon
//
// A proposal is reviewed as a listing is (listing_review.go), under the
// kinds "org" and "adventure", with the proposal's id. Taking a picture
// down never waits: deleting it applies at once.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
)

const (
	listingOrg       = "org"
	listingAdventure = "adventure"
)

// presentationRevision hashes a proposal: what a reviewer approves.
const presentationRevision = `md5(row(p.fields, p.image_mime_type, md5(p.image_content), p.cover_mime_type, md5(p.cover_content))::text)`

// orgProposalFields and adventureProposalFields are the columns review
// holds back. look_cover stands for look_style's cover.
var (
	orgProposalFields       = map[string]bool{"name": true, "tagline": true, "public_website": true}
	adventureProposalFields = map[string]bool{"title": true, "description": true, "author": true, "intro_text": true,
		"outro_text": true, "start_label": true, "area": true, "access_note": true, "buyer_note": true, "look_cover": true}
)

// querier reads through the pool or a transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// orgGated reports whether an organisation's page edits are reviewed:
// it has something approved for sale, and no platform admin vouches for
// it — not a verified creator, not on invoice billing (a customer under
// contract), not entitled to its own domain. Its edits apply at once and
// are reviewed afterwards (org_page_changes.go).
func orgGated(ctx context.Context, q querier, orgID int64) (bool, error) {
	var gated bool
	err := q.QueryRowContext(ctx, `
		SELECT NOT g.verified_creator AND g.trusted = 0
		   AND (g.custom_domain_until IS NULL OR g.custom_domain_until + 30 < current_date) AND (
		       EXISTS (SELECT 1 FROM event_listings l JOIN events e ON e.id = l.event_id WHERE e.org_id = g.id)
		    OR EXISTS (SELECT 1 FROM party_offer_listings l JOIN party_offers o ON o.id = l.offer_id WHERE o.org_id = g.id))
		FROM organizations g WHERE g.id = $1`, orgID).Scan(&gated)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return gated, err
}

// adventureGated reports whether an adventure's presentation edits wait
// for review: its organisation isn't a verified creator and a listing
// approved for sale plays it. It returns the adventure's id and owner.
func adventureGated(ctx context.Context, q querier, slug string) (advID, orgID int64, gated bool, err error) {
	var owner sql.NullInt64
	err = q.QueryRowContext(ctx, `
		SELECT a.id, a.org_id, a.org_id IS NOT NULL AND NOT COALESCE(g.verified_creator, FALSE) AND (
		       EXISTS (SELECT 1 FROM event_listings l JOIN events e ON e.id = l.event_id WHERE e.adventure_slug = a.slug)
		    OR EXISTS (SELECT 1 FROM party_offer_listings l JOIN party_offers o ON o.id = l.offer_id WHERE o.adventure_slug = a.slug))
		FROM adventures a LEFT JOIN organizations g ON g.id = a.org_id WHERE a.slug = $1`, slug).Scan(&advID, &owner, &gated)
	return advID, owner.Int64, gated, err
}

// picture is an uploaded picture held back for review.
type picture struct {
	mime    string
	content []byte
}

// propose gathers held-back edits in the open proposal: fields merge in,
// a field set back to what is live drops out, and any edit makes the
// proposal a draft again — what was submitted has changed.
func propose(ctx context.Context, db *sql.DB, kind string, targetID, orgID int64, fields map[string]any, unset []string, image, cover *picture) error {
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var im, imType, cv, cvType any
	if image != nil {
		im, imType = image.content, image.mime
	}
	if cover != nil {
		cv, cvType = cover.content, cover.mime
	}
	if unset == nil {
		unset = []string{}
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO presentation_proposals (kind, target_id, org_id, fields, image_mime_type, image_content, cover_mime_type, cover_content)
		VALUES ($1, $2, $3, $4::jsonb - $9::text[], $5, $6, $7, $8)
		ON CONFLICT (kind, target_id) WHERE listing_review <> 'approved' DO UPDATE SET
		    fields          = (presentation_proposals.fields || EXCLUDED.fields) - $9::text[],
		    image_mime_type = COALESCE(EXCLUDED.image_mime_type, presentation_proposals.image_mime_type),
		    image_content   = COALESCE(EXCLUDED.image_content, presentation_proposals.image_content),
		    cover_mime_type = COALESCE(EXCLUDED.cover_mime_type, presentation_proposals.cover_mime_type),
		    cover_content   = COALESCE(EXCLUDED.cover_content, presentation_proposals.cover_content),
		    listing_review  = 'draft', updated_at = now()`,
		kind, targetID, orgID, raw, imType, im, cvType, cv, "{"+strings.Join(unset, ",")+"}")
	if err != nil {
		return err
	}
	return dropEmptyProposal(ctx, db, kind, targetID)
}

// dropEmptyProposal removes an open proposal with nothing left in it.
func dropEmptyProposal(ctx context.Context, db *sql.DB, kind string, targetID int64) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM presentation_proposals
		WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved'
		  AND fields = '{}' AND image_content IS NULL AND cover_content IS NULL`, kind, targetID)
	return err
}

// dropProposedPicture forgets a held-back picture when the live one is
// taken down.
func dropProposedPicture(ctx context.Context, db *sql.DB, kind string, targetID int64, col string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE presentation_proposals SET `+col+`_mime_type = NULL, `+col+`_content = NULL, listing_review = 'draft', updated_at = now()
		WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved' AND `+col+`_content IS NOT NULL`, kind, targetID)
	if err != nil {
		return err
	}
	return dropEmptyProposal(ctx, db, kind, targetID)
}

// approveProposal writes a proposal to the organisation or adventure.
func (h *Handlers) approveProposal(ctx context.Context, q execer, id, reviewer int64) error {
	tx, ok := q.(*sql.Tx)
	if !ok {
		var err error
		if tx, err = h.DB.BeginTx(ctx, nil); err != nil {
			return err
		}
		defer tx.Rollback()
		if err := h.approveProposal(ctx, tx, id, reviewer); err != nil {
			return err
		}
		return tx.Commit()
	}
	var (
		kind           string
		target         int64
		raw            []byte
		imType, cvType sql.NullString
		image, cover   []byte
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT kind, target_id, fields, image_mime_type, image_content, cover_mime_type, cover_content
		FROM presentation_proposals WHERE id = $1`, id).Scan(&kind, &target, &raw, &imType, &image, &cvType, &cover); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	allowed, table := orgProposalFields, "organizations"
	if kind == listingAdventure {
		allowed, table = adventureProposalFields, "adventures"
	}
	for col, v := range fields {
		if !allowed[col] {
			continue
		}
		var err error
		if col == "look_cover" {
			_, err = tx.ExecContext(ctx, `
				UPDATE adventures SET look_style = jsonb_set(COALESCE(look_style, '{}'::jsonb), '{cover}', $2::jsonb) WHERE id = $1`, target, string(v))
		} else {
			var s string
			if err = json.Unmarshal(v, &s); err != nil {
				return err
			}
			val := any(s)
			if kind == listingOrg && s == "" {
				val = nil // the org columns clear to NULL
			}
			_, err = tx.ExecContext(ctx, `UPDATE `+table+` SET `+col+` = $2 WHERE id = $1`, target, val)
		}
		if err != nil {
			return err
		}
	}
	if kind == listingAdventure {
		if _, err := tx.ExecContext(ctx, `UPDATE adventures SET updated_at = now_text() WHERE id = $1`, target); err != nil {
			return err
		}
	}
	if image != nil {
		col := "logo"
		if kind == listingAdventure {
			col = "avatar"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET `+col+`_mime_type = $2, `+col+`_size_bytes = $3, `+col+`_content = $4 WHERE id = $1`,
			target, imType.String, int64(len(image)), image); err != nil {
			return err
		}
	}
	if cover != nil && kind == listingOrg {
		if _, err := tx.ExecContext(ctx, `
			UPDATE organizations SET cover_image_mime_type = $2, cover_image_size_bytes = $3, cover_image_content = $4 WHERE id = $1`,
			target, cvType.String, int64(len(cover)), cover); err != nil {
			return err
		}
	}
	by := sql.NullInt64{Int64: reviewer, Valid: reviewer > 0}
	_, err := tx.ExecContext(ctx, `
		UPDATE presentation_proposals SET listing_review = 'approved', listing_reviewed_at = now(),
		       listing_reviewed_by = $2, listing_reject_reason = ''
		WHERE id = $1`, id, by)
	return err
}

/* ---------------- the organisation's and adventure's views ---------------- */

// proposalDTO is an open proposal as its organisation sees it.
type proposalDTO struct {
	ID           int64          `json:"id"`
	State        string         `json:"state"` // draft | pending | rejected
	Fields       map[string]any `json:"fields"`
	HasImage     bool           `json:"has_image"`
	HasCover     bool           `json:"has_cover"`
	RejectReason string         `json:"reject_reason,omitempty"`
	// Gated: edits wait for review now. When false, they apply at once.
	Gated bool `json:"gated"`
}

func (h *Handlers) openProposal(ctx context.Context, kind string, targetID int64) (*proposalDTO, error) {
	var (
		d   proposalDTO
		raw []byte
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT id, listing_review, fields, image_content IS NOT NULL, cover_content IS NOT NULL, listing_reject_reason
		FROM presentation_proposals WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved'`, kind, targetID).
		Scan(&d.ID, &d.State, &raw, &d.HasImage, &d.HasCover, &d.RejectReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &d.Fields)
	return &d, err
}

// myOrg is the caller's organisation, for its org admins.
func myOrgAdmin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	me, ok := auth.FromContext(r.Context())
	if !ok || me.OrgID == nil || me.Role != auth.RoleOrgAdmin {
		httpx.Error(w, http.StatusForbidden, "org admins only")
		return 0, false
	}
	return *me.OrgID, true
}

func (h *Handlers) writeProposal(w http.ResponseWriter, r *http.Request, kind string, target int64, gated bool) {
	d, err := h.openProposal(r.Context(), kind, target)
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "proposal lookup failed", err)
		return
	}
	if d == nil {
		d = &proposalDTO{Fields: map[string]any{}}
	}
	d.Gated = gated
	httpx.JSON(w, http.StatusOK, d)
}

// GetMyOrgProposal is the organisation page's open proposal.
func (h *Handlers) GetMyOrgProposal(w http.ResponseWriter, r *http.Request) {
	orgID, ok := myOrgAdmin(w, r)
	if !ok {
		return
	}
	// Its page edits apply at once and are reviewed afterwards: a
	// proposal is only one held back before that, until reviewed.
	h.writeProposal(w, r, listingOrg, orgID, false)
}

// SubmitMyOrgProposal sends the organisation page's proposal for review.
func (h *Handlers) SubmitMyOrgProposal(w http.ResponseWriter, r *http.Request) {
	orgID, ok := myOrgAdmin(w, r)
	if !ok {
		return
	}
	h.submitProposal(w, r, listingOrg, orgID)
}

// DiscardMyOrgProposal drops the organisation page's open proposal.
func (h *Handlers) DiscardMyOrgProposal(w http.ResponseWriter, r *http.Request) {
	orgID, ok := myOrgAdmin(w, r)
	if !ok {
		return
	}
	h.discardProposal(w, r, listingOrg, orgID)
}

// GetMyOrgProposalPicture writes a proposed logo (image) or cover.
func (h *Handlers) GetMyOrgProposalPicture(w http.ResponseWriter, r *http.Request) {
	orgID, ok := myOrgAdmin(w, r)
	if !ok {
		return
	}
	h.writeProposalPicture(w, r, listingOrg, orgID, chi.URLParam(r, "which"))
}

func (h *Handlers) adventureProposalTarget(w http.ResponseWriter, r *http.Request) (int64, bool, bool) {
	slug := chi.URLParam(r, "slug")
	if _, ok := h.requireEditAdventure(w, r, slug); !ok {
		return 0, false, false
	}
	advID, _, gated, err := adventureGated(r.Context(), h.DB, slug)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "unknown adventure")
		return 0, false, false
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "proposal lookup failed", err)
		return 0, false, false
	}
	return advID, gated, true
}

// GetAdventureProposal is the adventure's open proposal.
func (h *Handlers) GetAdventureProposal(w http.ResponseWriter, r *http.Request) {
	advID, gated, ok := h.adventureProposalTarget(w, r)
	if !ok {
		return
	}
	h.writeProposal(w, r, listingAdventure, advID, gated)
}

// SubmitAdventureProposal sends the adventure's proposal for review.
func (h *Handlers) SubmitAdventureProposal(w http.ResponseWriter, r *http.Request) {
	advID, _, ok := h.adventureProposalTarget(w, r)
	if !ok {
		return
	}
	h.submitProposal(w, r, listingAdventure, advID)
}

// DiscardAdventureProposal drops the adventure's open proposal.
func (h *Handlers) DiscardAdventureProposal(w http.ResponseWriter, r *http.Request) {
	advID, _, ok := h.adventureProposalTarget(w, r)
	if !ok {
		return
	}
	h.discardProposal(w, r, listingAdventure, advID)
}

// GetAdventureProposalPicture writes the proposed icon.
func (h *Handlers) GetAdventureProposalPicture(w http.ResponseWriter, r *http.Request) {
	advID, _, ok := h.adventureProposalTarget(w, r)
	if !ok {
		return
	}
	h.writeProposalPicture(w, r, listingAdventure, advID, "image")
}

func (h *Handlers) submitProposal(w http.ResponseWriter, r *http.Request, kind string, target int64) {
	var id int64
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id FROM presentation_proposals WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved'`, kind, target).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.JSON(w, http.StatusConflict, map[string]string{"error": errNothingToSubmit.Error(), "code": "unchanged"})
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "proposal lookup failed", err)
		return
	}
	h.submitListing(w, r, kind, id)
}

func (h *Handlers) discardProposal(w http.ResponseWriter, r *http.Request, kind string, target int64) {
	if _, err := h.DB.ExecContext(r.Context(), `
		DELETE FROM presentation_proposals WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved'`, kind, target); err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "discard failed", err)
		return
	}
	httpx.JSON(w, http.StatusNoContent, nil)
}

func (h *Handlers) writeProposalPicture(w http.ResponseWriter, r *http.Request, kind string, target int64, which string) {
	if which != "image" && which != "cover" {
		http.NotFound(w, r)
		return
	}
	var (
		mime    sql.NullString
		content []byte
	)
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT `+which+`_mime_type, `+which+`_content FROM presentation_proposals
		WHERE kind = $1 AND target_id = $2 AND listing_review <> 'approved'`, kind, target).Scan(&mime, &content)
	if err != nil || !mime.Valid || len(content) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime.String)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(content)
}

/* ---------------- what a reviewer sees ---------------- */

// presentationLabels name the fields as listing content names them
// (listing_content.go), so a proposal's snapshot and a listing's agree.
var presentationLabels = map[string]string{
	"name": "Organisation: name", "tagline": "Organisation: tagline", "public_website": "Organisation: website",
	"title": "Adventure: title", "description": "Adventure: description", "author": "Adventure: author",
	"intro_text": "Adventure: intro", "outro_text": "Adventure: outro", "start_label": "Adventure: meeting point",
	"area": "Adventure: area", "access_note": "Adventure: access note", "buyer_note": "Adventure: note to buyers",
}

// proposalContent is an organisation page or adventure presentation as
// a proposal would leave it, with what it changes.
func (h *Handlers) proposalContent(ctx context.Context, kind string, id int64) (ReviewContent, error) {
	c := ReviewContent{Kind: kind, ID: id, Texts: []ReviewText{}, Media: []ReviewMedia{}}
	var (
		target              int64
		raw                 []byte
		hasImage, hasCover  bool
		submitted, attested sql.NullString
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT p.listing_review, `+presentationRevision+`,
		       to_char(p.listing_submitted_at AT TIME ZONE 'UTC', `+isoTime+`),
		       to_char(p.listing_attested_at AT TIME ZONE 'UTC', `+isoTime+`), COALESCE(u.username, ''),
		       p.listing_reject_reason, p.target_id, p.fields, p.image_content IS NOT NULL, p.cover_content IS NOT NULL,
		       g.id, g.name, COALESCE(g.slug, ''), g.verified_creator
		FROM presentation_proposals p JOIN organizations g ON g.id = p.org_id
		LEFT JOIN users u ON u.id = p.listing_attested_by
		WHERE p.id = $1 AND p.kind = $2`, id, kind).Scan(
		&c.State, &c.Revision, &submitted, &attested, &c.AttestedBy, &c.RejectReason, &target, &raw, &hasImage, &hasCover,
		&c.OrgID, &c.OrgName, &c.OrgSlug, &c.OrgVerified)
	if err != nil {
		return c, err
	}
	c.SubmittedAt, c.AttestedAt = submitted.String, attested.String
	var proposed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &proposed); err != nil {
		return c, err
	}
	str := func(col string) (string, bool) {
		v, ok := proposed[col]
		if !ok {
			return "", false
		}
		var s string
		_ = json.Unmarshal(v, &s)
		return s, true
	}
	field := func(col, live string) {
		value := live
		if p, ok := str(col); ok {
			value = p
			c.Changes = append(c.Changes, ReviewChange{Field: presentationLabels[col], Live: live, Proposed: p})
		}
		if value = strings.TrimSpace(value); value != "" {
			c.Texts = append(c.Texts, ReviewText{Where: presentationLabels[col], Text: value})
		}
	}
	media := func(ref, mkind, where string) {
		c.Media = append(c.Media, ReviewMedia{Ref: ref, Kind: mkind, Where: where, URL: reviewMediaURL(kind, id, ref)})
	}
	picture := func(ref, label string, proposedPic, livePic bool) {
		if proposedPic || livePic {
			media(ref, "image", label)
		}
		if proposedPic {
			ch := ReviewChange{Field: label, Proposed: "new picture", ProposedURL: reviewMediaURL(kind, id, ref)}
			if livePic {
				ch.Live, ch.LiveURL = "current picture", reviewMediaURL(kind, id, ref+"-live")
			}
			c.Changes = append(c.Changes, ch)
		}
	}

	if kind == listingOrg {
		var (
			tagline, website sql.NullString
			logo, cover      bool
		)
		if err := h.DB.QueryRowContext(ctx, `
			SELECT tagline, public_website,
			       (logo_content IS NOT NULL AND length(logo_content) > 0),
			       (cover_image_content IS NOT NULL AND length(cover_image_content) > 0)
			FROM organizations WHERE id = $1`, target).Scan(&tagline, &website, &logo, &cover); err != nil {
			return c, err
		}
		field("name", c.OrgName)
		field("tagline", tagline.String)
		field("public_website", website.String)
		picture("org-logo", "Organisation: logo", hasImage, logo)
		picture("org-cover", "Organisation: cover", hasCover, cover)
		return c, nil
	}

	var (
		description, author, area, accessNote, buyerNote string
		intro, outro, startLabel                         sql.NullString
		icon                                             bool
		cover                                            sql.NullInt64
	)
	if err := h.DB.QueryRowContext(ctx, `
		SELECT slug, COALESCE(title, ''), COALESCE(description, ''), COALESCE(author, ''), intro_text, outro_text,
		       start_label, COALESCE(area, ''), access_note, COALESCE(buyer_note, ''), avatar_mime_type IS NOT NULL,
		       NULLIF(look_style->>'cover', '')::bigint
		FROM adventures WHERE id = $1`, target).Scan(&c.AdventureSlug, &c.AdventureTitle, &description, &author,
		&intro, &outro, &startLabel, &area, &accessNote, &buyerNote, &icon, &cover); err != nil {
		return c, err
	}
	field("title", c.AdventureTitle)
	if t, ok := str("title"); ok {
		c.AdventureTitle = t
	}
	field("description", description)
	field("author", author)
	field("intro_text", intro.String)
	field("outro_text", outro.String)
	field("start_label", startLabel.String)
	field("area", area)
	field("access_note", accessNote)
	field("buyer_note", buyerNote)
	picture("adventure-icon", "Adventure: icon", hasImage, icon)
	// The cover is one of the adventure's own media.
	coverID := cover
	if v, ok := proposed["look_cover"]; ok {
		var n int64
		if json.Unmarshal(v, &n) == nil || json.Unmarshal([]byte(strings.Trim(string(v), `"`)), &n) == nil {
			coverID = sql.NullInt64{Int64: n, Valid: n > 0}
		}
		ch := ReviewChange{Field: "Adventure: cover", Proposed: "new picture"}
		if coverID.Valid {
			ch.ProposedURL = reviewMediaURL(kind, id, "media-"+strconv.FormatInt(coverID.Int64, 10))
		}
		if cover.Valid {
			ch.Live, ch.LiveURL = "current picture", reviewMediaURL(kind, id, "media-"+strconv.FormatInt(cover.Int64, 10))
		}
		c.Changes = append(c.Changes, ch)
	}
	if coverID.Valid {
		media("media-"+strconv.FormatInt(coverID.Int64, 10), "image", "Adventure: cover")
	}
	return c, nil
}

// proposalMedia reads a picture of a proposal: as proposed, or with a
// "-live" ref as it is now. An adventure's media must be its own.
func (h *Handlers) proposalMedia(ctx context.Context, kind string, id int64, ref string) (string, []byte, error) {
	var (
		mime    sql.NullString
		content []byte
	)
	live := strings.HasSuffix(ref, "-live")
	base := strings.TrimSuffix(ref, "-live")
	col := map[string]string{"org-logo": "image", "org-cover": "cover", "adventure-icon": "image"}[base]
	if col != "" && !live {
		err := h.DB.QueryRowContext(ctx, `
			SELECT `+col+`_mime_type, `+col+`_content FROM presentation_proposals WHERE id = $1 AND kind = $2`, id, kind).Scan(&mime, &content)
		if err != nil {
			return "", nil, err
		}
		if mime.Valid && len(content) > 0 {
			return mime.String, content, nil
		}
	}
	target := `(SELECT target_id FROM presentation_proposals WHERE id = $1 AND kind = '` + kind + `')`
	var q string
	args := []any{id}
	switch {
	case base == "org-logo" && kind == listingOrg:
		q = `SELECT logo_mime_type, logo_content FROM organizations WHERE id = ` + target
	case base == "org-cover" && kind == listingOrg:
		q = `SELECT cover_image_mime_type, cover_image_content FROM organizations WHERE id = ` + target
	case base == "adventure-icon" && kind == listingAdventure:
		q = `SELECT avatar_mime_type, avatar_content FROM adventures WHERE id = ` + target
	case strings.HasPrefix(ref, "media-") && kind == listingAdventure:
		mid, err := strconv.ParseInt(strings.TrimPrefix(ref, "media-"), 10, 64)
		if err != nil {
			return "", nil, sql.ErrNoRows
		}
		q, args = `SELECT mime_type, content FROM adventure_media WHERE id = $2 AND adventure_id = `+target, []any{id, mid}
	default:
		return "", nil, sql.ErrNoRows
	}
	if err := h.DB.QueryRowContext(ctx, q, args...).Scan(&mime, &content); err != nil {
		return "", nil, err
	}
	if !mime.Valid || len(content) == 0 {
		return "", nil, sql.ErrNoRows
	}
	return mime.String, content, nil
}

// HoldAdventureIcon keeps an uploaded icon back for review when the
// adventure's presentation edits wait for it: for the console and the
// MCP tools. held says it did.
func HoldAdventureIcon(ctx context.Context, db *sql.DB, slug string, icon []byte, mime string) (bool, error) {
	advID, orgID, gated, err := adventureGated(ctx, db, slug)
	if err != nil || !gated {
		return false, err
	}
	return true, propose(ctx, db, listingAdventure, advID, orgID, map[string]any{}, nil, &picture{mime: mime, content: icon}, nil)
}

// DropAdventureIconProposal forgets a held-back icon when the live one
// is taken down.
func DropAdventureIconProposal(ctx context.Context, db *sql.DB, slug string) error {
	var advID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM adventures WHERE slug = $1`, slug).Scan(&advID); err != nil {
		return nil
	}
	return dropProposedPicture(ctx, db, listingAdventure, advID, "image")
}

// metaGate holds back an adventure's presentation fields during one
// meta update, when they wait for review.
type metaGate struct {
	on           bool
	advID, orgID int64
	live         map[string]string
	liveCover    int64
	held         map[string]any
	unset        []string
}

// presentationGate reads whether the adventure's presentation waits for
// review, with its live values to tell a change from a no-op.
func presentationGate(ctx context.Context, db *sql.DB, slug string) (*metaGate, error) {
	g := &metaGate{held: map[string]any{}, live: map[string]string{}}
	advID, orgID, gated, err := adventureGated(ctx, db, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return g, nil // the update reports the unknown adventure
	}
	if err != nil || !gated {
		return g, err
	}
	g.on, g.advID, g.orgID = true, advID, orgID
	var cover sql.NullInt64
	vals := make([]sql.NullString, 9)
	cols := []string{"title", "description", "author", "intro_text", "outro_text", "start_label", "area", "access_note", "buyer_note"}
	if err := db.QueryRowContext(ctx, `
		SELECT title, description, author, intro_text, outro_text, start_label, area, access_note, buyer_note,
		       NULLIF(look_style->>'cover', '')::bigint
		FROM adventures WHERE id = $1`, advID).Scan(&vals[0], &vals[1], &vals[2], &vals[3], &vals[4], &vals[5], &vals[6], &vals[7], &vals[8], &cover); err != nil {
		return nil, err
	}
	for i, c := range cols {
		g.live[c] = vals[i].String
	}
	g.liveCover = cover.Int64
	return g, nil
}

// hold takes a presentation field out of the update into the proposal.
// A value equal to the live one drops out of the proposal; clearing a
// field (taking it down) applies at once.
func (g *metaGate) hold(col string, val any) bool {
	if !g.on || !adventureProposalFields[col] {
		return false
	}
	s, ok := val.(string)
	if !ok || strings.TrimSpace(s) == "" {
		// Cleared: it applies, and what was proposed for it goes.
		g.unset = append(g.unset, col)
		return false
	}
	if s == g.live[col] {
		g.unset = append(g.unset, col)
	} else {
		g.held[col] = s
	}
	return true
}

// names are the fields held back, sorted.
func (g *metaGate) names() []string {
	out := make([]string, 0, len(g.held))
	for k := range g.held {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
