package admin

// The listing review log is a hash chain (migration 0124): every
// submission and decision is an entry whose hash covers the entry
// before it, so a past entry can't be edited or removed without
// breaking every hash after it. Each entry fingerprints the content
// reviewed (content_hash), kept in listing_review_snapshots as the
// reviewer saw it. The chain's head is mailed to the platform's admins
// daily: a copy outside the database, against rewriting the whole log.
//
//	GET /api/admin/listing-reviews/log/verify   recompute the chain   listing reviewers
//
// The hashes are SHA-256, hex, over the JSON of chainEntry and
// reviewSnapshot: Go marshals struct fields in order, so the same
// values always give the same bytes. The first entry's prev_hash is "".

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/emailtpl"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// reviewSnapshot is what a reviewer reviews, as it is fingerprinted:
// the listing's fields, every text, and each media item by the SHA-256
// of its bytes.
type reviewSnapshot struct {
	Kind          string           `json:"kind"`
	ID            int64            `json:"id"`
	Revision      string           `json:"revision"`
	OrgID         int64            `json:"org_id"`
	OrgName       string           `json:"org_name"`
	AdventureSlug string           `json:"adventure_slug"`
	GraphID       int64            `json:"graph_id"`
	Version       int              `json:"version"`
	Draft         listingFieldsDTO `json:"draft"`
	Texts         []ReviewText     `json:"texts"`
	Media         []snapshotMedia  `json:"media"`
}

type snapshotMedia struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	Mime   string `json:"mime"`
	Where  string `json:"where"`
	SHA256 string `json:"sha256"`
}

// chainEntry is what an entry's hash covers.
type chainEntry struct {
	PrevHash    string `json:"prev_hash"`
	Kind        string `json:"kind"`
	ItemID      int64  `json:"item_id"`
	OrgID       int64  `json:"org_id"`
	OrgName     string `json:"org_name"`
	Action      string `json:"action"`
	Via         string `json:"via"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Revision    string `json:"revision"`
	GraphID     int64  `json:"graph_id"`
	Reason      string `json:"reason"`
	ContentHash string `json:"content_hash"`
	CreatedAt   string `json:"created_at"`
	// A human reviewer's passkey signature over the decision
	// (reviewer_passkeys.go); empty when unsigned.
	SigCredentialID      string `json:"sig_credential_id"`
	SigPublicKey         string `json:"sig_public_key"`
	SigNonce             string `json:"sig_nonce"`
	SigAuthenticatorData string `json:"sig_authenticator_data"`
	SigClientData        string `json:"sig_client_data"`
	SigSignature         string `json:"sig_signature"`
}

func hashJSON(v any) (string, []byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

// snapshotOf fingerprints what a listing puts in front of people now.
func (h *Handlers) snapshotOf(ctx context.Context, kind string, id int64) (string, []byte, error) {
	c, err := h.ReviewContentOf(ctx, kind, id)
	if err != nil {
		return "", nil, err
	}
	s, err := h.snapshotFrom(ctx, c, false)
	if err != nil {
		return "", nil, err
	}
	return hashJSON(s)
}

// snapshotFrom fingerprints gathered content: each media item by the
// SHA-256 of its bytes, the cover from the approved copy when served.
func (h *Handlers) snapshotFrom(ctx context.Context, c ReviewContent, served bool) (reviewSnapshot, error) {
	s := reviewSnapshot{Kind: c.Kind, ID: c.ID, Revision: c.Revision, OrgID: c.OrgID, OrgName: c.OrgName,
		AdventureSlug: c.AdventureSlug, GraphID: c.GraphID, Version: c.Version, Draft: c.Draft,
		Texts: c.Texts, Media: make([]snapshotMedia, 0, len(c.Media))}
	for _, m := range c.Media {
		ref := m.Ref
		if served && ref == "event-cover" {
			ref = "event-cover-live"
		}
		_, content, err := h.reviewMedia(ctx, c.Kind, c.ID, ref)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return s, err
		}
		sum := sha256.Sum256(content)
		s.Media = append(s.Media, snapshotMedia{Ref: m.Ref, Kind: m.Kind, Mime: m.Mime, Where: m.Where, SHA256: hex.EncodeToString(sum[:])})
	}
	return s, nil
}

// logListing appends a submission or decision to the chain, in the
// transaction that makes it: no decision stands unlogged. A
// transaction-scoped advisory lock keeps two appends from naming the
// same entry as their predecessor.
func (h *Handlers) logListing(ctx context.Context, tx *sql.Tx, kind string, id, orgID int64, action, via string, userID int64, revision string, graphID sql.NullInt64, reason string, sig decisionSignature) error {
	contentHash, content, err := h.snapshotOf(ctx, kind, id)
	if err != nil {
		return fmt.Errorf("review snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO listing_review_snapshots (content_hash, content) VALUES ($1, $2)
		ON CONFLICT (content_hash) DO NOTHING`, contentHash, content); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('listing_review_log'))`); err != nil {
		return err
	}
	e := chainEntry{Kind: kind, ItemID: id, OrgID: orgID, Action: action, Via: via, UserID: userID,
		Revision: revision, GraphID: graphID.Int64, Reason: reason, ContentHash: contentHash,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		SigCredentialID: sig.CredentialID, SigPublicKey: sig.PublicKey, SigNonce: sig.Nonce,
		SigAuthenticatorData: sig.AuthenticatorData, SigClientData: sig.ClientData, SigSignature: sig.Signature}
	if err := tx.QueryRowContext(ctx, `SELECT entry_hash FROM listing_review_log ORDER BY id DESC LIMIT 1`).Scan(&e.PrevHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_ = tx.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&e.OrgName)
	if userID > 0 {
		_ = tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&e.Username)
	}
	entryHash, _, err := hashJSON(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO listing_review_log (kind, item_id, org_id, org_name, action, via, user_id, username,
		       revision, graph_id, reason, content_hash, prev_hash, entry_hash, created_at,
		       sig_credential_id, sig_public_key, sig_nonce, sig_authenticator_data, sig_client_data, sig_signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		kind, id, orgID, e.OrgName, action, via, sql.NullInt64{Int64: userID, Valid: userID > 0}, e.Username,
		revision, graphID, reason, contentHash, e.PrevHash, entryHash, e.CreatedAt,
		sig.CredentialID, sig.PublicKey, sig.Nonce, sig.AuthenticatorData, sig.ClientData, sig.Signature)
	return err
}

// chainProblem is where the chain doesn't add up.
type chainProblem struct {
	ID      int64  `json:"id"`
	Problem string `json:"problem"`
}

// chainReport is the outcome of recomputing the chain.
type chainReport struct {
	OK      bool  `json:"ok"`
	Entries int64 `json:"entries"`
	// Signed counts decisions signed with a reviewer's passkey, each
	// checked; Unsigned those made without one: by a reviewer with
	// no passkey, through an API token or by a verified creator's
	// automatic approval.
	Signed   int64 `json:"signed"`
	Unsigned int64 `json:"unsigned"`
	// Served compares what approved listings serve now with what was
	// approved (listing_review_served.go).
	Served   servedReport   `json:"served"`
	HeadHash string         `json:"head_hash"`
	Problems []chainProblem `json:"problems"`
}

// verifyChain recomputes every entry's hash and every snapshot's.
func (h *Handlers) verifyChain(ctx context.Context) (chainReport, error) {
	rep := chainReport{Problems: []chainProblem{}}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT l.id, l.kind, l.item_id, COALESCE(l.org_id, 0), l.org_name, l.action, l.via, COALESCE(l.user_id, 0),
		       l.username, l.revision, COALESCE(l.graph_id, 0), l.reason, l.content_hash, l.prev_hash, l.entry_hash,
		       l.created_at, s.content,
		       l.sig_credential_id, l.sig_public_key, l.sig_nonce, l.sig_authenticator_data, l.sig_client_data, l.sig_signature
		FROM listing_review_log l
		LEFT JOIN listing_review_snapshots s ON s.content_hash = l.content_hash
		ORDER BY l.id`)
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		var (
			id        int64
			e         chainEntry
			stored    string
			createdAt time.Time
			content   []byte
		)
		if err := rows.Scan(&id, &e.Kind, &e.ItemID, &e.OrgID, &e.OrgName, &e.Action, &e.Via, &e.UserID,
			&e.Username, &e.Revision, &e.GraphID, &e.Reason, &e.ContentHash, &e.PrevHash, &stored,
			&createdAt, &content,
			&e.SigCredentialID, &e.SigPublicKey, &e.SigNonce, &e.SigAuthenticatorData, &e.SigClientData, &e.SigSignature); err != nil {
			return rep, err
		}
		rep.Entries++
		e.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		if e.PrevHash != prev {
			rep.Problems = append(rep.Problems, chainProblem{id, "does not follow the entry before it: one was changed, removed or inserted"})
		}
		if got, _, _ := hashJSON(e); got != stored {
			rep.Problems = append(rep.Problems, chainProblem{id, "its fields were changed after it was logged"})
		}
		var snap reviewSnapshot
		switch {
		case content == nil:
			rep.Problems = append(rep.Problems, chainProblem{id, "the content it reviewed is missing"})
		case json.Unmarshal(content, &snap) != nil:
			rep.Problems = append(rep.Problems, chainProblem{id, "the content it reviewed can't be read"})
		default:
			if got, _, _ := hashJSON(snap); got != e.ContentHash {
				rep.Problems = append(rep.Problems, chainProblem{id, "the content it reviewed was changed"})
			}
		}
		if e.Action != "submitted" {
			switch {
			case e.SigSignature != "":
				rep.Signed++
				if err := verifyDecisionSignature(e); err != nil {
					rep.Problems = append(rep.Problems, chainProblem{id, err.Error()})
				}
			default:
				rep.Unsigned++
			}
		}
		prev, rep.HeadHash = stored, stored
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}
	if rep.Served, err = h.checkServed(ctx); err != nil {
		return rep, err
	}
	rep.OK = len(rep.Problems) == 0
	for _, f := range rep.Served.Findings {
		if f.Severity == "changed" {
			rep.OK = false
		}
	}
	return rep, nil
}

// VerifyListingLog recomputes the review log's hash chain and says
// where it doesn't add up. Compare head_hash with the last one mailed
// to the platform's admins. Listing reviewers and global admins only.
func (h *Handlers) VerifyListingLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := listingReader(w, r); !ok {
		return
	}
	rep, err := h.verifyChain(r.Context())
	if err != nil {
		httpx.ServerErr(w, http.StatusInternalServerError, "verify failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, rep)
}

// listingAnchorEvery is how often, at most, the chain's head is mailed.
const listingAnchorEvery = 24 * time.Hour

// AnchorListingLog mails the review log's head to the platform's admins
// once a day when it has moved, with the day's check of the log and of
// what approved listings serve — and when the check finds a problem
// even if it hasn't moved: run it periodically. A copy outside the
// database makes even a rewritten log show, when its head no longer
// matches the mail.
func (h *Handlers) AnchorListingLog(ctx context.Context) {
	var (
		head    string
		entries int64
	)
	err := h.DB.QueryRowContext(ctx, `
		SELECT entry_hash, (SELECT count(*) FROM listing_review_log)
		FROM listing_review_log ORDER BY id DESC LIMIT 1`).Scan(&head, &entries)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		logx.Warn("admin.listing.anchor", "err", err.Error())
		return
	}
	var (
		lastHead string
		lastAt   time.Time
	)
	err = h.DB.QueryRowContext(ctx, `SELECT head_hash, created_at FROM listing_review_anchors ORDER BY id DESC LIMIT 1`).Scan(&lastHead, &lastAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		logx.Warn("admin.listing.anchor", "err", err.Error())
		return
	}
	if err == nil && time.Since(lastAt) < listingAnchorEvery {
		return
	}
	// The daily check: the chain, its signatures, and what approved
	// listings serve against what was approved.
	rep, verr := h.verifyChain(ctx)
	if verr != nil {
		logx.Warn("admin.listing.anchor", "err", verr.Error())
	}
	problems := len(rep.Problems)
	for _, f := range rep.Served.Findings {
		if f.Severity == "changed" {
			problems++
		}
	}
	if lastHead == head && problems == 0 {
		return
	}
	to, err := h.emailsOf(ctx, `SELECT email FROM users WHERE is_admin AND NOT is_support AND COALESCE(email, '') <> ''`)
	if err != nil || len(to) == 0 {
		return
	}
	l := h.orgLook(ctx, 0)
	subject, c := emailtpl.ListingLogHead(l.Lang, head, entries, problems, h.arrangeLink("#/listing-reviews/log"))
	if err := h.send(ctx, "listing_log_head", l.message(to, subject, c)); err != nil {
		logx.Warn("admin.listing.anchor", "err", err.Error())
		return
	}
	if _, err := h.DB.ExecContext(ctx, `INSERT INTO listing_review_anchors (head_hash, entries) VALUES ($1, $2)`, head, entries); err != nil {
		logx.Warn("admin.listing.anchor", "err", err.Error())
	}
}
