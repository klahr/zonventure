package admin

// What is served against what was approved. The hash chain proves the
// review log is honest; this checks the other side: that every approved
// listing still serves exactly what its approval fingerprinted. An edit
// made around the review — straight in the database, past the triggers
// of migration 0126 — shows as a difference here.
//
// Differences come in two kinds. "changed" is what review covers and
// nothing but an approval may change: the listing's fields and cover,
// the adventure version, its texts and media, the organisation's page,
// the adventure's presentation (held back for review, presentation.go)
// and the files players get. "unreviewed" is what review doesn't cover:
// an organisation's name, which platform admins set.
//
// What a listing should serve is its latest approval, with the
// organisation's page and the adventure's presentation as approved
// last — by the listing's approval or a later proposal's.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// servedFinding is an approved listing whose served content differs
// from what was approved, or that has no approval on record.
type servedFinding struct {
	Kind     string `json:"kind"`
	ID       int64  `json:"id"`
	OrgName  string `json:"org_name"`
	Title    string `json:"title"`
	Severity string `json:"severity"` // changed | unreviewed | unrecorded
	// Parts names what differs: listing, adventure version, adventure
	// content, organisation page, adventure presentation, event files.
	Parts []string `json:"parts,omitempty"`
}

// servedReport is the outcome of checking every approved listing.
type servedReport struct {
	Checked  int             `json:"checked"`
	Matching int             `json:"matching"`
	Findings []servedFinding `json:"findings"`
}

// presentationTexts are the adventure's own texts, edited live.
var presentationTexts = map[string]bool{
	"Adventure: title": true, "Adventure: description": true, "Adventure: author": true,
	"Adventure: intro": true, "Adventure: outro": true, "Adventure: meeting point": true,
	"Adventure: area": true, "Adventure: access note": true, "Adventure: note to buyers": true,
}

// servedSnapshot fingerprints what a listing's approved copy serves.
func (h *Handlers) servedSnapshot(ctx context.Context, kind string, id int64) (reviewSnapshot, error) {
	c, err := h.contentOf(ctx, kind, id, true)
	if err != nil {
		return reviewSnapshot{}, err
	}
	return h.snapshotFrom(ctx, c, true)
}

// checkServed compares every approved listing with its latest approval.
func (h *Handlers) checkServed(ctx context.Context) (servedReport, error) {
	rep := servedReport{Findings: []servedFinding{}}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT 'event', l.event_id, g.name, COALESCE(NULLIF(l.name, ''), e.adventure_slug),
		       l.graph_id IS DISTINCT FROM e.graph_id
		FROM event_listings l JOIN events e ON e.id = l.event_id JOIN organizations g ON g.id = e.org_id
		UNION ALL
		SELECT 'party', l.offer_id, g.name, COALESCE(NULLIF(l.name, ''), NULLIF(a.title, ''), o.adventure_slug), FALSE
		FROM party_offer_listings l JOIN party_offers o ON o.id = l.offer_id
		JOIN organizations g ON g.id = o.org_id LEFT JOIN adventures a ON a.slug = o.adventure_slug
		ORDER BY 1, 2`)
	if err != nil {
		return rep, err
	}
	type item struct {
		kind, org, title string
		id               int64
		otherVersion     bool
	}
	var items []item
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.kind, &x.id, &x.org, &x.title, &x.otherVersion); err != nil {
			rows.Close()
			return rep, err
		}
		items = append(items, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	for _, x := range items {
		rep.Checked++
		f := servedFinding{Kind: x.kind, ID: x.id, OrgName: x.org, Title: x.title}
		var raw []byte
		err := h.DB.QueryRowContext(ctx, `
			SELECT s.content FROM listing_review_log l JOIN listing_review_snapshots s ON s.content_hash = l.content_hash
			WHERE l.kind = $1 AND l.item_id = $2 AND l.action = 'approved'
			ORDER BY l.id DESC LIMIT 1`, x.kind, x.id).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			// Approved before review was logged (what was for sale when
			// review came in): nothing to compare with.
			f.Severity = "unrecorded"
			rep.Findings = append(rep.Findings, f)
			continue
		}
		if err != nil {
			return rep, err
		}
		var approved reviewSnapshot
		if err := json.Unmarshal(raw, &approved); err != nil {
			f.Severity, f.Parts = "changed", []string{"approval record"}
			rep.Findings = append(rep.Findings, f)
			continue
		}
		if err := h.withLaterApprovals(ctx, x.kind, x.id, &approved); err != nil {
			return rep, err
		}
		served, err := h.servedSnapshot(ctx, x.kind, x.id)
		if err != nil {
			return rep, err
		}
		changed, unreviewed := diffSnapshots(approved, served)
		if x.otherVersion {
			changed = append(changed, "the event plays another adventure version")
		}
		switch {
		case len(changed) > 0:
			f.Severity, f.Parts = "changed", append(changed, unreviewed...)
		case len(unreviewed) > 0:
			f.Severity, f.Parts = "unreviewed", unreviewed
		default:
			rep.Matching++
			continue
		}
		rep.Findings = append(rep.Findings, f)
	}
	return rep, nil
}

// sameFields reports whether two listings say the same. By their JSON:
// a snapshot taken before listings had translations has none, as one
// with an empty set.
func sameFields(a, b listingFieldsDTO) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// diffSnapshots names what differs between what was approved and what
// is served: what only an approval may change, and what is edited live.
func diffSnapshots(approved, served reviewSnapshot) (changed, unreviewed []string) {
	parts := map[string]string{} // part -> "changed" | "unreviewed"
	mark := func(part, kind string) {
		if parts[part] != "changed" {
			parts[part] = kind
		}
	}
	if approved.Revision != served.Revision || !sameFields(approved.Draft, served.Draft) {
		mark("listing", "changed")
	}
	if approved.GraphID != served.GraphID || approved.Version != served.Version {
		mark("adventure version", "changed")
	}
	texts := func(s reviewSnapshot) map[string]string {
		m := map[string]string{}
		for _, t := range s.Texts {
			m[t.Where] += t.Text + "\n"
		}
		return m
	}
	a, b := texts(approved), texts(served)
	for where := range union(a, b) {
		// Files are compared by their bytes below.
		if a[where] != b[where] && !strings.HasPrefix(where, "Event file:") {
			mark(textPart(where))
		}
	}
	media := func(s reviewSnapshot) map[string]snapshotMedia {
		m := map[string]snapshotMedia{}
		for _, x := range s.Media {
			m[x.Ref] = x
		}
		return m
	}
	ma, mb := media(approved), media(served)
	for ref := range union(ma, mb) {
		_, wasApproved := ma[ref]
		_, isServed := mb[ref]
		if ma[ref].SHA256 == mb[ref].SHA256 && wasApproved == isServed {
			continue
		}
		// A file taken down since is fine; one served unapproved isn't.
		if strings.HasPrefix(ref, "file-") && !isServed {
			continue
		}
		where := ma[ref].Where
		if where == "" {
			where = mb[ref].Where
		}
		mark(mediaPart(ref, where))
	}
	for part, kind := range parts {
		if kind == "changed" {
			changed = append(changed, part)
		} else {
			unreviewed = append(unreviewed, part)
		}
	}
	sort.Strings(changed)
	sort.Strings(unreviewed)
	return changed, unreviewed
}

func union[V any](a, b map[string]V) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// textPart is the part of a listing a text belongs to, and whether only
// an approval may change it.
func textPart(where string) (string, string) {
	switch {
	case strings.HasPrefix(where, "Listing:"):
		return "listing", "changed"
	case where == "Organisation: name":
		return "organisation name", "unreviewed"
	case strings.HasPrefix(where, "Organisation:"):
		return "organisation page", "changed"
	case presentationTexts[where]:
		return "adventure presentation", "changed"
	}
	return "adventure content", "changed"
}

// mediaPart is textPart for a media item.
func mediaPart(ref, where string) (string, string) {
	switch {
	case ref == "event-cover":
		return "listing", "changed"
	case strings.HasPrefix(ref, "org-"):
		return "organisation page", "changed"
	case strings.HasPrefix(ref, "file-"):
		return "event files", "changed"
	case ref == "adventure-icon" || where == "Adventure: cover":
		return "adventure presentation", "changed"
	}
	return "adventure content", "changed"
}

// withLaterApprovals brings an approved listing snapshot's organisation
// page and adventure presentation up to their latest approval: a
// proposal approved after the listing changes what it should serve.
func (h *Handlers) withLaterApprovals(ctx context.Context, kind string, id int64, s *reviewSnapshot) error {
	var since int64
	if err := h.DB.QueryRowContext(ctx, `
		SELECT max(id) FROM listing_review_log WHERE kind = $1 AND item_id = $2 AND action = 'approved'`, kind, id).Scan(&since); err != nil {
		return err
	}
	later := func(q string, args ...any) (*reviewSnapshot, error) {
		var raw []byte
		err := h.DB.QueryRowContext(ctx, q, args...).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var p reviewSnapshot
		return &p, json.Unmarshal(raw, &p)
	}
	org, err := later(`
		SELECT s.content FROM listing_review_log l JOIN listing_review_snapshots s ON s.content_hash = l.content_hash
		WHERE l.kind = 'org' AND l.action = 'approved' AND l.org_id = $1 AND l.id > $2
		ORDER BY l.id DESC LIMIT 1`, s.OrgID, since)
	if err != nil {
		return err
	}
	adv, err := later(`
		SELECT s.content FROM listing_review_log l JOIN listing_review_snapshots s ON s.content_hash = l.content_hash
		WHERE l.kind = 'adventure' AND l.action = 'approved' AND s.content->>'adventure_slug' = $1 AND l.id > $2
		ORDER BY l.id DESC LIMIT 1`, s.AdventureSlug, since)
	if err != nil {
		return err
	}
	isOrg := func(where, ref string) bool {
		return (strings.HasPrefix(where, "Organisation:") && where != "Organisation: name") || strings.HasPrefix(ref, "org-")
	}
	isPresentation := func(where, ref string) bool {
		return presentationTexts[where] || ref == "adventure-icon" || where == "Adventure: cover"
	}
	replace := func(from *reviewSnapshot, part func(where, ref string) bool) {
		if from == nil {
			return
		}
		texts := s.Texts[:0:0]
		for _, t := range s.Texts {
			if !part(t.Where, "") {
				texts = append(texts, t)
			}
		}
		for _, t := range from.Texts {
			if part(t.Where, "") {
				texts = append(texts, t)
			}
		}
		media := s.Media[:0:0]
		for _, m := range s.Media {
			if !part(m.Where, m.Ref) {
				media = append(media, m)
			}
		}
		for _, m := range from.Media {
			if part(m.Where, m.Ref) {
				media = append(media, m)
			}
		}
		s.Texts, s.Media = texts, media
	}
	replace(org, isOrg)
	replace(adv, isPresentation)
	return nil
}
