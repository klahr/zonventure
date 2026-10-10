package mcp

// Listing review over MCP (internal/admin/listing_review.go): a platform
// admin's assistant reads what waits for review — every text and picture
// a listing puts in front of people — and approves it or rejects it with
// a reason. Its decisions are logged as made through an API token, for
// the console's spot-checks. An organisation's assistant submits its
// listings for review.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/imageutil"
)

const (
	reviewImagesDefault = 8
	reviewImagesMax     = 16
	reviewImageDim      = 1024
)

func (s *Server) registerListingReviewTools() {
	kindID := map[string]any{
		"kind": map[string]any{"type": "string", "enum": []string{"event", "party"}, "description": "event: a self-guided event's store listing; party: a party offer."},
		"id":   map[string]any{"type": "integer", "description": "The listing's id, from list_listing_reviews."},
	}
	s.tools.register(ToolDescriptor{
		Name:     "list_listing_reviews",
		Audience: audienceReviewer,
		Scope:    scopeRead,
		Description: "Listing reviewer: the listings waiting for review before they go public — self-guided events for sale and party offers — oldest first, " +
			"each with its organisation, adventure, what changed against the approved version (live) and the revision to decide on. " +
			"Review each with get_listing_review before approving or rejecting it.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, listListingReviews)

	s.tools.register(ToolDescriptor{
		Name:     "get_listing_review",
		Audience: audienceReviewer,
		Scope:    scopeRead,
		Description: "Listing reviewer: everything a listing waiting for review puts in front of the public and its players, to look for offensive, hateful, sexual, violent, " +
			"discriminatory or illegal content, and anything unsuitable for children: the listing, the organisation's page, the adventure's presentation and every " +
			"text of the adventure version it plays (texts, with where each is), followed by its pictures as images. A big adventure's pictures come in pages: " +
			"the reply says the media_offset for the next page. Sounds, films and files are listed but not shown; when they matter, leave the listing for a person. " +
			"Then approve_listing or reject_listing with the revision.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"kind", "id"},
			"properties": mergeProps(kindID, map[string]any{
				"media_offset": map[string]any{"type": "integer", "minimum": 0, "description": "First picture to show; 0 by default."},
				"media_limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": reviewImagesMax, "description": fmt.Sprintf("Pictures to show, at most %d; %d by default.", reviewImagesMax, reviewImagesDefault)},
			}),
		},
	}, getListingReview)

	decision := mergeProps(kindID, map[string]any{
		"revision": map[string]any{"type": "string", "description": "The revision you reviewed, from list_listing_reviews or get_listing_review. A listing changed since is refused: review it again."},
	})
	s.tools.register(ToolDescriptor{
		Name:     "approve_listing",
		Audience: audienceReviewer,
		Scope:    scopeWrite,
		Description: "Listing reviewer: make a listing you reviewed in full with get_listing_review public, with the adventure version reviewed. " +
			"Only approve when every text and picture is fine; when in doubt, reject with a reason or leave it for a person. The decision is logged as made by an assistant.",
		InputSchema: map[string]any{"type": "object", "required": []string{"kind", "id", "revision"}, "properties": decision},
	}, decideListing("approve"))

	s.tools.register(ToolDescriptor{
		Name:     "reject_listing",
		Audience: audienceReviewer,
		Scope:    scopeWrite,
		Description: "Listing reviewer: turn a listing down, flagging what is wrong. The reason is emailed to the organisation's admins, so name what to change " +
			"and where (quote the text or describe the picture) without repeating slurs. Anything already public stays as it was. Logged as made by an assistant.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"kind", "id", "revision", "reason"},
			"properties": mergeProps(decision, map[string]any{
				"reason": map[string]any{"type": "string", "maxLength": admin.MaxRejectReasonLen, "description": "What is wrong and what to change."},
			}),
		},
	}, decideListing("reject"))

	s.tools.register(ToolDescriptor{
		Name:  "submit_listing_for_review",
		Scope: scopeWrite,
		Description: "Send a self-guided event's store listing (event_id) or a party offer (adventure_slug) for review before it goes public; " +
			"a verified creator's is approved at once. Submitting is the user's confirmation that the listing and the adventure it plays contain nothing " +
			"offensive, hateful or illegal and follow the terms: ask the user and pass attest: true only when they have confirmed it. " +
			"Changes after submitting need submitting again.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"attest"},
			"properties": map[string]any{
				"event_id":       map[string]any{"type": "integer", "description": "A self-guided event, to submit its store listing."},
				"adventure_slug": map[string]any{"type": "string", "description": "An adventure the organisation offers as a party, to submit its offer."},
				"attest":         map[string]any{"type": "boolean", "description": "true once the user has confirmed the content follows the terms."},
			},
		},
	}, submitListingForReview)
}

func mergeProps(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func listListingReviews(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListListingReviews, "GET", "/api/admin/listing-reviews", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type listingArgs struct {
	Kind        string `json:"kind"`
	ID          int64  `json:"id"`
	MediaOffset int    `json:"media_offset,omitempty"`
	MediaLimit  int    `json:"media_limit,omitempty"`
	Revision    string `json:"revision,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func (a listingArgs) check() *ToolCallResult {
	if a.Kind != "event" && a.Kind != "party" {
		r := errorResult(`kind must be "event" or "party"`)
		return &r
	}
	if a.ID <= 0 {
		r := errorResult("id required and must be positive")
		return &r
	}
	return nil
}

func getListingReview(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a listingArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if r := a.check(); r != nil {
		return *r, nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.ID, 10)
	status, body, err := callAdmin(ctx, s.Admin.GetListingReview, "GET", "/api/admin/listing-reviews/"+a.Kind+"/"+id,
		map[string]string{"kind": a.Kind, "id": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var c admin.ReviewContent
	if err := json.Unmarshal(body, &c); err != nil {
		return errorResult("decode: " + err.Error()), nil
	}

	var pictures, others []admin.ReviewMedia
	for _, m := range c.Media {
		if m.Kind == "image" {
			pictures = append(pictures, m)
		} else {
			others = append(others, m)
		}
	}
	limit := a.MediaLimit
	if limit <= 0 {
		limit = reviewImagesDefault
	}
	if limit > reviewImagesMax {
		limit = reviewImagesMax
	}
	from := min(max(a.MediaOffset, 0), len(pictures))
	to := min(from+limit, len(pictures))

	var b strings.Builder
	fmt.Fprintf(&b, "Listing %s %d by %s (%s)", c.Kind, c.ID, c.OrgName, map[bool]string{true: "verified creator", false: "not verified"}[c.OrgVerified])
	fmt.Fprintf(&b, "\nState: %s. Revision: %s.", c.State, c.Revision)
	if c.AttestedBy != "" {
		fmt.Fprintf(&b, " Submitted by %s, who confirmed it follows the terms.", c.AttestedBy)
	}
	fmt.Fprintf(&b, "\nAdventure %q (%s), version %d", c.AdventureTitle, c.AdventureSlug, c.Version)
	if c.LiveGraphID != 0 && c.LiveGraphID != c.GraphID {
		b.WriteString(" — a different version from the one approved before")
	}
	fmt.Fprintf(&b, ".\n\nTexts (%d):\n", len(c.Texts))
	for _, t := range c.Texts {
		fmt.Fprintf(&b, "- [%s] %s\n", t.Where, t.Text)
	}
	if len(others) > 0 {
		b.WriteString("\nNot shown here (sounds, films, files):\n")
		for _, m := range others {
			fmt.Fprintf(&b, "- %s %s (%s, %d bytes): %s\n", m.Kind, m.Ref, m.Mime, m.Size, m.Where)
		}
	}
	fmt.Fprintf(&b, "\nPictures %d–%d of %d follow.", from+1, to, len(pictures))
	if to < len(pictures) {
		fmt.Fprintf(&b, " Call again with media_offset=%d for the rest before deciding.", to)
	}
	out := ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}
	for _, m := range pictures[from:to] {
		label := ToolContent{Type: "text", Text: fmt.Sprintf("Picture %s: %s", m.Ref, m.Where)}
		mime, content, err := s.Admin.ReviewMediaOf(ctx, c.Kind, c.ID, m.Ref)
		if err == nil {
			if small, smallMime, err := imageutil.Shrink(content, reviewImageDim); err == nil {
				out.Content = append(out.Content, label, ToolContent{Type: "image", Data: base64.StdEncoding.EncodeToString(small), MimeType: smallMime})
				continue
			}
		}
		// An SVG or a broken file: a person has to look at it.
		label.Text += " — could not be shown here (" + mime + "); leave the listing for a person if it matters."
		out.Content = append(out.Content, label)
	}
	return out, nil
}

func decideListing(action string) func(context.Context, *Server, json.RawMessage) (ToolCallResult, error) {
	return func(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
		var a listingArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return errorResult("bad args: " + err.Error()), nil
		}
		if r := a.check(); r != nil {
			return *r, nil
		}
		if strings.TrimSpace(a.Revision) == "" {
			return errorResult("revision required: review the listing with get_listing_review first"), nil
		}
		if r := adminConfigured(s); r != nil {
			return *r, nil
		}
		handler := s.Admin.ApproveListing
		if action == "reject" {
			handler = s.Admin.RejectListing
		}
		id := strconv.FormatInt(a.ID, 10)
		status, body, err := callAdmin(ctx, handler, "POST", "/api/admin/listing-reviews/"+a.Kind+"/"+id+"/"+action,
			map[string]string{"kind": a.Kind, "id": id}, nil, map[string]string{"revision": a.Revision, "reason": a.Reason})
		if err != nil {
			return errorResult("call: " + err.Error()), nil
		}
		return adminCallResult(status, body), nil
	}
}

type submitListingArgs struct {
	EventID       int64  `json:"event_id,omitempty"`
	AdventureSlug string `json:"adventure_slug,omitempty"`
	Attest        bool   `json:"attest"`
}

func submitListingForReview(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a submitListingArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if (a.EventID > 0) == (strings.TrimSpace(a.AdventureSlug) != "") {
		return errorResult("pass either event_id or adventure_slug"), nil
	}
	if !a.Attest {
		return errorResult("the user must confirm the content follows the terms: ask them, then pass attest: true"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	body := map[string]bool{"attest": true}
	var (
		status int
		resp   []byte
		err    error
	)
	if a.EventID > 0 {
		id := strconv.FormatInt(a.EventID, 10)
		status, resp, err = callAdmin(ctx, s.Admin.SubmitEventListing, "POST", "/api/admin/events/"+id+"/listing/submit",
			map[string]string{"eventID": id}, nil, body)
	} else {
		slug := strings.TrimSpace(a.AdventureSlug)
		status, resp, err = callAdmin(ctx, s.Admin.SubmitPartyOffer, "POST", "/api/admin/party-offers/"+slug+"/submit",
			map[string]string{"slug": slug}, nil, body)
	}
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, resp), nil
}
