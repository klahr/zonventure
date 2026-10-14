package admin

// What a listing puts in front of the public and its players, for a
// reviewer looking for offensive content (listing_review.go): the
// listing, the organisation's page, the adventure's presentation and
// every text and media item of the adventure version it plays, raw.
// The console's review page and the MCP review tools read the same.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
	"github.com/klahr/zonventure/internal/httpx"
)

// ReviewText is one piece of text people will read, and where it is.
type ReviewText struct {
	Where string `json:"where"`
	Text  string `json:"text"`
}

// ReviewMedia is one picture, sound, film or file people will see or
// hear. Ref names it for the media route; Kind is image, audio, video
// or file.
type ReviewMedia struct {
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Mime  string `json:"mime"`
	Where string `json:"where"`
	Size  int64  `json:"size"`
	URL   string `json:"url"`
}

// ReviewContent is everything a listing puts in front of people.
type ReviewContent struct {
	Kind         string `json:"kind"`
	ID           int64  `json:"id"`
	State        string `json:"state"`
	Revision     string `json:"revision"`
	SubmittedAt  string `json:"submitted_at,omitempty"`
	AttestedAt   string `json:"attested_at,omitempty"`
	AttestedBy   string `json:"attested_by,omitempty"`
	RejectReason string `json:"reject_reason,omitempty"`
	OrgID        int64  `json:"org_id"`
	OrgName      string `json:"org_name"`
	OrgSlug      string `json:"org_slug,omitempty"`
	OrgVerified  bool   `json:"org_verified"`
	// Adventure and the version reviewed, with the one approved before
	// (LiveGraphID, 0 when none).
	AdventureSlug  string            `json:"adventure_slug"`
	AdventureTitle string            `json:"adventure_title"`
	GraphID        int64             `json:"graph_id"`
	Version        int               `json:"version"`
	LiveGraphID    int64             `json:"live_graph_id,omitempty"`
	Draft          listingFieldsDTO  `json:"draft"`
	Live           *listingFieldsDTO `json:"live,omitempty"`
	Texts          []ReviewText      `json:"texts"`
	Media          []ReviewMedia     `json:"media"`
	// StepTitles names the adventure's steps by their index in Texts'
	// "nodes[i]" paths, so the console can label a text "Step 3 · Bridge".
	// For display only: snapshotFrom leaves it out of the fingerprint.
	StepTitles []string `json:"step_titles,omitempty"`
	// Changes is what a proposal (an organisation's page, an
	// adventure's presentation) changes: each field as it is and as
	// proposed. Texts and Media are the page as it would be.
	Changes []ReviewChange `json:"changes,omitempty"`
}

// ReviewChange is one field a proposal changes. A picture's URLs show
// it as it is and as proposed.
type ReviewChange struct {
	Field       string `json:"field"`
	Live        string `json:"live,omitempty"`
	Proposed    string `json:"proposed,omitempty"`
	LiveURL     string `json:"live_url,omitempty"`
	ProposedURL string `json:"proposed_url,omitempty"`
}

// reviewMediaURL is where a reviewer gets one media item of a listing.
func reviewMediaURL(kind string, id int64, ref string) string {
	return fmt.Sprintf("/api/admin/listing-reviews/%s/%d/media/%s", kind, id, ref)
}

// GetListingReview returns everything a listing puts in front of
// people. Listing reviewers only, of other organisations' listings.
//
// GET /api/admin/listing-reviews/{kind}/{id}
func (h *Handlers) GetListingReview(w http.ResponseWriter, r *http.Request) {
	me, ok := listingReader(w, r)
	if !ok {
		return
	}
	kind, id, ok := listingParams(w, r)
	if !ok {
		return
	}
	c, err := h.ReviewContentOf(r.Context(), kind, id)
	if err == nil {
		if own, oerr := h.ownOrg(r.Context(), me.ID, c.OrgID); oerr != nil || own {
			if oerr == nil {
				httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "someone outside your organisation has to review its listings", "code": "own_org"})
				return
			}
			err = oerr
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "no such listing")
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "review content failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, c)
}

// ReviewContentOf gathers what a listing puts in front of people.
func (h *Handlers) ReviewContentOf(ctx context.Context, kind string, id int64) (ReviewContent, error) {
	return h.contentOf(ctx, kind, id, false)
}

// servedRevision is the revision of what a listing's approved copy
// serves, hashed as eventListingRevision / partyListingRevision hash a
// draft: equal to the revision approved while the copy is untouched.
var servedRevision = map[string]string{
	listingEvent: `SELECT md5(row(l.name, l.blurb, l.price_cents, l.currency, l.team_size,
		l.cover_mime_type, md5(l.cover_content), l.graph_id,
		(SELECT string_agg(f.id::text || ':' || md5(f.content), ',' ORDER BY f.id) FROM event_files f
		 WHERE f.event_id = l.event_id AND f.approved_at IS NOT NULL))::text) FROM event_listings l WHERE l.event_id = $1`,
	listingParty: `SELECT md5(row(l.blurb, l.base_cents, l.per_kid_cents, l.currency, l.max_kids,
		l.graph_id)::text || l.name) FROM party_offer_listings l WHERE l.offer_id = $1`,
}

// contentOf gathers a listing's content: its draft, or with served what
// its approved copy serves the public and players now (the copy's
// fields, cover and adventure version). The organisation's page and the
// adventure's presentation are as they are now either way.
func (h *Handlers) contentOf(ctx context.Context, kind string, id int64, served bool) (ReviewContent, error) {
	if kind == listingOrg || kind == listingAdventure {
		return h.proposalContent(ctx, kind, id)
	}
	c := ReviewContent{Kind: kind, ID: id, Texts: []ReviewText{}, Media: []ReviewMedia{}}
	t := listingTables(kind)
	var (
		advID                         int64
		graphID, liveGraphID          sql.NullInt64
		submitted, attested           sql.NullString
		tagline, website              sql.NullString
		hasLogo, hasOrgCover, hasIcon bool
		description, author           string
		intro, outro, startLabel      sql.NullString
		area, buyerNote               string
		lookCover                     sql.NullInt64
		draftCover, liveCover         bool
		isLive                        bool
		live                          listingFieldsDTO
	)
	draftCols := `e.name, e.public_blurb, COALESCE(e.ticket_price_cents, 0), 0, e.ticket_currency, e.ticket_team_size, 0,
		e.cover_image_mime_type IS NOT NULL,
		l.event_id IS NOT NULL, COALESCE(l.name, ''), COALESCE(l.blurb, ''), COALESCE(l.price_cents, 0), 0,
		COALESCE(l.currency, ''), COALESCE(l.team_size, 0), 0, l.cover_mime_type IS NOT NULL, l.graph_id`
	joins := `JOIN adventures a ON a.slug = e.adventure_slug LEFT JOIN event_listings l ON l.event_id = e.id`
	if kind == listingParty {
		draftCols = `o.name, o.blurb, o.base_cents, o.per_kid_cents, o.currency, 0, o.max_kids, FALSE,
		l.offer_id IS NOT NULL, COALESCE(l.name, ''), COALESCE(l.blurb, ''), COALESCE(l.base_cents, 0), COALESCE(l.per_kid_cents, 0),
		COALESCE(l.currency, ''), 0, COALESCE(l.max_kids, 0), FALSE, l.graph_id`
		joins = `LEFT JOIN party_offer_listings l ON l.offer_id = o.id`
	}
	x := t.alias
	err := h.DB.QueryRowContext(ctx, `
		SELECT COALESCE(`+x+`.listing_review, ''), `+t.revision+`,
		       to_char(`+x+`.listing_submitted_at AT TIME ZONE 'UTC', `+isoTime+`),
		       to_char(`+x+`.listing_attested_at AT TIME ZONE 'UTC', `+isoTime+`), COALESCE(u.username, ''),
		       `+x+`.listing_reject_reason,
		       g.id, g.name, COALESCE(g.slug, ''), g.verified_creator, g.tagline, g.public_website,
		       (g.logo_content IS NOT NULL AND length(g.logo_content) > 0),
		       (g.cover_image_content IS NOT NULL AND length(g.cover_image_content) > 0),
		       a.id, a.slug, COALESCE(a.title, ''), COALESCE(a.description, ''), COALESCE(a.author, ''),
		       a.intro_text, a.outro_text, a.start_label, COALESCE(a.area, ''), COALESCE(a.buyer_note, ''),
		       a.avatar_mime_type IS NOT NULL, NULLIF(a.look_style->>'cover', '')::bigint,
		       `+t.graph+`, `+draftCols+`
		FROM `+t.from+` `+joins+`
		JOIN organizations g ON g.id = `+x+`.org_id
		LEFT JOIN users u ON u.id = `+x+`.listing_attested_by
		WHERE `+x+`.id = $1`, id).Scan(
		&c.State, &c.Revision, &submitted, &attested, &c.AttestedBy, &c.RejectReason,
		&c.OrgID, &c.OrgName, &c.OrgSlug, &c.OrgVerified, &tagline, &website, &hasLogo, &hasOrgCover,
		&advID, &c.AdventureSlug, &c.AdventureTitle, &description, &author,
		&intro, &outro, &startLabel, &area, &buyerNote, &hasIcon, &lookCover,
		&graphID,
		&c.Draft.Name, &c.Draft.Blurb, &c.Draft.PriceCents, &c.Draft.PerKidCents, &c.Draft.Currency, &c.Draft.TeamSize, &c.Draft.MaxKids,
		&draftCover,
		&isLive, &live.Name, &live.Blurb, &live.PriceCents, &live.PerKidCents, &live.Currency, &live.TeamSize, &live.MaxKids,
		&liveCover, &liveGraphID)
	if err != nil {
		return c, err
	}
	c.SubmittedAt, c.AttestedAt = submitted.String, attested.String
	c.GraphID, c.Draft.GraphID = graphID.Int64, graphID.Int64
	if isLive {
		live.GraphID = liveGraphID.Int64
		c.Live, c.LiveGraphID = &live, liveGraphID.Int64
	}
	if served {
		if !isLive {
			return c, sql.ErrNoRows
		}
		c.Draft, graphID, draftCover = live, liveGraphID, liveCover
		c.GraphID = graphID.Int64
		if err := h.DB.QueryRowContext(ctx, servedRevision[kind], id).Scan(&c.Revision); err != nil {
			return c, err
		}
	}

	text := func(where, s string) {
		if s = strings.TrimSpace(s); s != "" {
			c.Texts = append(c.Texts, ReviewText{Where: where, Text: s})
		}
	}
	media := func(ref, kind, mime, where string, size int64) {
		c.Media = append(c.Media, ReviewMedia{Ref: ref, Kind: kind, Mime: mime, Where: where, Size: size,
			URL: reviewMediaURL(c.Kind, c.ID, ref)})
	}

	// The listing.
	text("Listing: name", c.Draft.Name)
	text("Listing: blurb", c.Draft.Blurb)
	if draftCover {
		c.Draft.CoverURL = reviewMediaURL(kind, id, "event-cover")
		media("event-cover", "image", "", "Listing: cover", 0)
	}
	if liveCover && c.Live != nil {
		c.Live.CoverURL = reviewMediaURL(kind, id, "event-cover-live")
	}
	// The organisation's page.
	text("Organisation: name", c.OrgName)
	text("Organisation: tagline", tagline.String)
	text("Organisation: website", website.String)
	if hasLogo {
		media("org-logo", "image", "", "Organisation: logo", 0)
	}
	if hasOrgCover {
		media("org-cover", "image", "", "Organisation: cover", 0)
	}
	// The adventure's presentation.
	text("Adventure: title", c.AdventureTitle)
	text("Adventure: description", description)
	text("Adventure: author", author)
	text("Adventure: intro", intro.String)
	text("Adventure: outro", outro.String)
	text("Adventure: meeting point", startLabel.String)
	text("Adventure: area", area)
	text("Adventure: note to buyers", buyerNote)
	if hasIcon {
		media("adventure-icon", "image", "", "Adventure: icon", 0)
	}
	mediaIDs := []int64{}
	where := map[int64]string{}
	if lookCover.Valid {
		mediaIDs = append(mediaIDs, lookCover.Int64)
		where[lookCover.Int64] = "Adventure: cover"
	}
	// The adventure version it plays: every string of it, raw.
	if graphID.Valid {
		stored, err := graphstore.ByID(ctx, h.DB, graphID.Int64)
		if err != nil {
			return c, fmt.Errorf("graph %d: %w", graphID.Int64, err)
		}
		c.Version = stored.Version
		texts, err := graphTexts(stored.Graph)
		if err != nil {
			return c, err
		}
		c.Texts = append(c.Texts, texts...)
		for _, n := range stored.Graph.Nodes {
			title := strings.TrimSpace(n.Title)
			if title == "" {
				title = n.Slug
			}
			c.StepTitles = append(c.StepTitles, title)
		}
		for _, pm := range stored.Graph.PrintMaterials {
			for lang, m := range pm.Files {
				if _, ok := where[m]; !ok {
					mediaIDs = append(mediaIDs, m)
					where[m] = fmt.Sprintf("Print material: %s (%s, %s)", pm.Name, pm.Delivery, lang)
				}
			}
		}
		for _, m := range stored.Graph.MediaIDs() {
			if _, ok := where[m]; !ok {
				mediaIDs = append(mediaIDs, m)
				where[m] = "Adventure content"
			}
		}
	}
	if len(mediaIDs) > 0 {
		rows, err := h.DB.QueryContext(ctx, `
			SELECT id, kind, mime_type, size_bytes FROM adventure_media
			WHERE adventure_id = $1 AND id = ANY(string_to_array($2, ',')::bigint[]) ORDER BY id`, advID, joinIDs(mediaIDs))
		if err != nil {
			return c, err
		}
		for rows.Next() {
			var (
				mid       int64
				mkind, mt string
				size      int64
			)
			if err := rows.Scan(&mid, &mkind, &mt, &size); err != nil {
				rows.Close()
				return c, err
			}
			media("media-"+strconv.FormatInt(mid, 10), mkind, mt, where[mid], size)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return c, err
		}
	}
	// Files the event's players download.
	if kind == listingEvent {
		// Served: what players get, the approved files.
		cond := ""
		if served {
			cond = " AND approved_at IS NOT NULL"
		}
		rows, err := h.DB.QueryContext(ctx, `SELECT id, name, mime_type, size_bytes FROM event_files WHERE event_id = $1`+cond+` ORDER BY id`, id)
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				fid      int64
				name, mt string
				size     int64
			)
			if err := rows.Scan(&fid, &name, &mt, &size); err != nil {
				return c, err
			}
			text("Event file: name", name)
			media("file-"+strconv.FormatInt(fid, 10), "file", mt, "Event file: "+name, size)
		}
		if err := rows.Err(); err != nil {
			return c, err
		}
	}
	return c, nil
}

// graphTexts is every string of an adventure people may read, with the
// path to it in the adventure's JSON: titles, prompts, hints, answers,
// options, emails, roles, translations. Keys that only name or wire
// things (ids, slugs, ports, kinds) are left out.
func graphTexts(g *graph.Graph) ([]ReviewText, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var out []ReviewText
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if graphWiringKeys[k] {
					continue
				}
				p := k
				if path != "" {
					p = path + "." + k
				}
				walk(p, x[k])
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		case string:
			if s := strings.TrimSpace(x); s != "" && hasWords.MatchString(s) && !colour.MatchString(s) {
				out = append(out, ReviewText{Where: "Adventure: " + path, Text: s})
			}
		}
	}
	walk("", v)
	return out, nil
}

// graphWiringKeys are the adventure JSON keys that hold references and
// settings, never words for people.
var graphWiringKeys = map[string]bool{
	"id": true, "slug": true, "port": true, "from": true, "to": true, "from_port": true,
	"kind": true, "type": true, "trigger": true, "zone": true, "item": true, "group": true,
	"lang": true, "mode": true, "start": true, "condition": true, "section": true,
	"translation_originals": true, "translation_sources": true,
}

var (
	hasWords = regexp.MustCompile(`\pL`)
	colour   = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)
)

// GetListingReviewMedia writes one media item of a listing: a cover, a
// logo, the adventure's icon, a picture or sound of its content, or a
// file the event's players download. Listing reviewers only.
//
// GET /api/admin/listing-reviews/{kind}/{id}/media/{ref}
func (h *Handlers) GetListingReviewMedia(w http.ResponseWriter, r *http.Request) {
	if _, ok := listingReader(w, r); !ok {
		return
	}
	kind, id, ok := listingParams(w, r)
	if !ok {
		return
	}
	mime, content, err := h.reviewMedia(r.Context(), kind, id, chi.URLParam(r, "ref"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "media lookup failed", err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.HasPrefix(mime, "image/") && !strings.HasPrefix(mime, "audio/") && !strings.HasPrefix(mime, "video/") {
		w.Header().Set("Content-Disposition", "attachment")
	}
	// An uploaded SVG is a document: never run what it carries.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(content)
}

// reviewMedia reads one media item of a listing, by ref. Every lookup is
// tied to the listing, so a ref can't reach another organisation's.
func (h *Handlers) reviewMedia(ctx context.Context, kind string, id int64, ref string) (string, []byte, error) {
	var (
		mime    sql.NullString
		content []byte
		q       string
		args    = []any{id}
	)
	orgOf := `(SELECT org_id FROM events WHERE id = $1)`
	advOf := `(SELECT a.id FROM events e JOIN adventures a ON a.slug = e.adventure_slug WHERE e.id = $1)`
	if kind == listingParty {
		orgOf = `(SELECT org_id FROM party_offers WHERE id = $1)`
		advOf = `(SELECT a.id FROM party_offers o JOIN adventures a ON a.slug = o.adventure_slug WHERE o.id = $1)`
	}
	if kind == listingOrg || kind == listingAdventure {
		return h.proposalMedia(ctx, kind, id, ref)
	}
	switch {
	case ref == "event-cover" && kind == listingEvent:
		q = `SELECT cover_image_mime_type, cover_image_content FROM events WHERE id = $1`
	case ref == "event-cover-live" && kind == listingEvent:
		q = `SELECT cover_mime_type, cover_content FROM event_listings WHERE event_id = $1`
	case ref == "org-logo":
		q = `SELECT logo_mime_type, logo_content FROM organizations WHERE id = ` + orgOf
	case ref == "org-cover":
		q = `SELECT cover_image_mime_type, cover_image_content FROM organizations WHERE id = ` + orgOf
	case ref == "adventure-icon":
		q = `SELECT avatar_mime_type, avatar_content FROM adventures WHERE id = ` + advOf
	case strings.HasPrefix(ref, "media-"):
		mid, err := strconv.ParseInt(strings.TrimPrefix(ref, "media-"), 10, 64)
		if err != nil {
			return "", nil, sql.ErrNoRows
		}
		q, args = `SELECT mime_type, content FROM adventure_media WHERE id = $2 AND adventure_id = `+advOf, []any{id, mid}
	case strings.HasPrefix(ref, "file-") && kind == listingEvent:
		fid, err := strconv.ParseInt(strings.TrimPrefix(ref, "file-"), 10, 64)
		if err != nil {
			return "", nil, sql.ErrNoRows
		}
		q, args = `SELECT mime_type, content FROM event_files WHERE id = $2 AND event_id = $1`, []any{id, fid}
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

// ReviewMediaOf reads one media item of a listing, for the MCP review
// tools.
func (h *Handlers) ReviewMediaOf(ctx context.Context, kind string, id int64, ref string) (string, []byte, error) {
	return h.reviewMedia(ctx, kind, id, ref)
}
