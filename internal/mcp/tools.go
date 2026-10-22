package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/klahr/zonventure/internal/admin"
	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/imageutil"
)

// errUnknownTool is the sentinel the dispatcher uses to map "no
// such tool" onto JSON-RPC method-not-found instead of an in-band
// tool error.
var errUnknownTool = errors.New("unknown tool")

// toolHandler is the signature every tool implements. Returning a
// ToolCallResult with IsError=true represents an in-band failure
// the model can read; returning a non-nil error with errUnknownTool
// surfaces as JSON-RPC -32601.
type toolHandler func(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error)

// registry is the immutable-after-startup tool table. Descriptors
// preserve registration order so tools/list returns a stable
// listing; the parallel handler / scope maps give O(1) dispatch.
type registry struct {
	descriptors []ToolDescriptor
	handlers    map[string]toolHandler
	scopes      map[string]string
	audiences   map[string]string
}

func newRegistry() *registry {
	return &registry{
		handlers:  map[string]toolHandler{},
		scopes:    map[string]string{},
		audiences: map[string]string{},
	}
}

// register panics on a missing or unknown scope. This is a
// startup-time correctness check (registerTools runs once in New)
// — better to crash the process than silently ship an unscoped
// tool that bypasses the gate.
func (r *registry) register(d ToolDescriptor, h toolHandler) {
	if d.Scope != scopeRead && d.Scope != scopeWrite {
		panic("mcp: tool " + d.Name + " registered with invalid scope: " + d.Scope)
	}
	r.descriptors = append(r.descriptors, d)
	r.handlers[d.Name] = h
	r.scopes[d.Name] = d.Scope
	r.audiences[d.Name] = d.Audience
}

// audienceAllows reports whether u may call a tool of audience aud.
func audienceAllows(u auth.User, aud string) bool {
	// A writer authors and does nothing else: no events, teams,
	// purchases or store, whatever else the tool is open to.
	if u.Role == auth.RoleOrgWriter && aud != audienceAuthor && aud != audienceReviewer {
		return false
	}
	switch aud {
	case audienceAny:
		return true
	case audienceAdmin:
		return u.Role == auth.RoleAdmin
	case audienceReviewer:
		return u.ListingReviewer
	case audienceAuthor:
		return u.Role != auth.RoleAdmin && u.OrgID != nil
	default:
		// Org-side tools: org users only — never the platform admin,
		// and never a lobby user. The handlers assume a non-nil OrgID,
		// so this is the gate that enforces it rather than relying on
		// each one to fail on nil.
		return u.Role != auth.RoleAdmin && u.OrgID != nil
	}
}

// list returns the tools the caller's audience may call.
func (r *registry) list(ctx context.Context) []ToolDescriptor {
	me, _ := auth.FromContext(ctx)
	have := scopesFromContext(ctx)
	out := []ToolDescriptor{}
	for _, d := range r.descriptors {
		// A tool the token's scopes can't call isn't offered: an
		// assistant would only try it and fail.
		if audienceAllows(me, d.Audience) && scopesAllow(have, d.Scope) {
			out = append(out, d)
		}
	}
	return out
}

func (r *registry) call(ctx context.Context, s *Server, name string, args json.RawMessage) (ToolCallResult, error) {
	h, ok := r.handlers[name]
	if !ok {
		return ToolCallResult{}, fmt.Errorf("%w: %s", errUnknownTool, name)
	}
	if me, _ := auth.FromContext(ctx); !audienceAllows(me, r.audiences[name]) {
		// In-band like the scope gate below. The platform admin
		// curates and monitors; authoring and running events is the
		// orgs' side, and operator tools are the admin's alone.
		return errorResult(fmt.Sprintf(
			"forbidden_audience: tool %q is not available to this account's role", name)), nil
	}
	need := r.scopes[name]
	have := scopesFromContext(ctx)
	if !scopesAllow(have, need) {
		// In-band tool error — the model sees a structured failure
		// it can react to (drop the call, ask the user to widen the
		// token) without the JSON-RPC layer treating it as a
		// protocol error.
		return errorResult(fmt.Sprintf(
			"forbidden_scope: tool %q requires the %q scope; this token has %v",
			name, need, have)), nil
	}
	return h(ctx, s, args)
}

// registerTools is the single place new tools get wired in. Keep
// the descriptors near the handlers so a contributor sees the
// public schema and the server-side behaviour together.
func (s *Server) registerTools() {
	// Phase 1 — event lifecycle. Lives in events.go alongside the
	// per-tool handlers so the descriptor and behaviour stay
	// adjacent to each other.
	s.registerEventTools()
	s.registerMessageTools()
	// Phase 2 — teams + slots. teams.go.
	s.registerTeamTools()
	// Phase 5 — read-only observation tools used during a live
	// event (positions, activity, tracks, summary). observation.go.
	s.registerObservationTools()
	// Phase 3 — assignments / billing. purchases.go. Real-money
	// charges are explicitly NOT exposed: create_purchase pins the
	// provider to "simulated" and refuses on Stripe-enabled
	// deployments.
	s.registerPurchaseTools()
	s.registerGraphTools()
	s.registerGraphEditTools()
	s.registerCollabTools()
	s.registerSimulateTools()
	s.registerPlayTools()
	s.registerDevTools()
	s.registerStepTools()
	s.registerOrgThemeTools()
	s.registerMediaTools()
	s.registerOrderTools()
	s.registerGuideTool()
	// Phase 4 — invitations + ICS export. invitations.go.
	s.registerInvitationTools()
	// Slice 3 polish — CSV results export and a static lifecycle
	// reference doc the model can fetch as a one-shot orientation.
	s.registerResultsTools()
	s.registerLifecycleTool()
	// Listing review: platform admins' assistants review what goes
	// public; orgs' assistants submit it. listing_review.go.
	s.registerListingReviewTools()

	s.tools.register(ToolDescriptor{
		Name:        "list_adventures",
		Audience:    audienceAuthor,
		Scope:       scopeRead,
		Description: "The adventures the caller's organisation can use: slug, title, language, can_edit (its own, to author) and playable (published, so events can run it). Check it before choosing a new adventure's slug.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listAdventures)

	s.tools.register(ToolDescriptor{
		Name:        "get_adventure",
		Audience:    audienceAuthor,
		Scope:       scopeRead,
		Description: "An adventure's settings: title, description, language, the brief agreed with the author, its look (colours, fonts, backdrop, style), starting point, icon_url and how many events run it. The steps are in get_adventure_graph; a single step in get_step.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug (list_adventures)."},
			},
		},
	}, getAdventure)

	s.tools.register(ToolDescriptor{
		Name:        "update_adventure_meta",
		Audience:    audienceAuthor,
		Scope:       scopeWrite,
		Description: "Patch an adventure's metadata: rename, recolor, edit intro/outro, change language, set start coords, flip strict_assets. Catalog listing (published) is the platform operator's and is refused here. Only present keys are applied — omit a field to leave it untouched. Use {field: null} on start_lat/start_lng to clear those columns.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "patch"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to update."},
				"patch": map[string]any{
					"type":        "object",
					"description": "Partial patch matching the admin updateAdventureRequest shape. Common fields: title, description, author, intro_text, outro_text, buyer_note (Markdown added to the buyer's ticket or host email, never shown to players), area (where it is played, a few words for the store card and map: \"Göteborg · Slottsskogen\", \"Hemma\"), facts (replaces the whole set the store shows buyers; leave out what isn't known: {minutes (a typical team's playing time, 1–1440), distance_m (the walk, 1–100000; none for one played in a spot), min_age (the youngest who can play in a team of their own, 1–99), difficulty easy|medium|hard, access step_free|some_steps|not_step_free (can a wheelchair or a pram follow the whole route)}; {} clears them; the brief's numbers are the author's intent, these what buyers are told), access_note (what access leaves unsaid, up to 200 characters: \"Stairs at the fourth stop\"; translated in the graph as meta:access_note), language; only for an adventure whose graph has no main theme (otherwise change the main theme with edit_adventure_graph put_theme): accent_color, accent_secondary_color, background_color, text_color, submit_bg_color, font, heading_font, corners (rounded|soft|sharp), cards (raised|flat), card_outline (bool), backdrop (a pattern behind the cards: {kind: dots|stripes|checks|grid|sunburst|waves|zigzag|confetti|snow|gradient|spots|horizon (a retro scene: color the grid, color2 the sun, colors the sky top to horizon; lined: a notebook page, color the rules, color2 the margin; picture: an uploaded image of this adventure as media, with blur 0–20 px), color and color2 #rrggbb or empty to follow the text and accent colours, opacity 0.02–0.6, scale 8–200 px, angle 0–359, fade, fixed, motion; spots take points [{x, y, color}]; confetti, snow and stripes may take colors, a list of up to 8 #rrggbb; snow with motion falls}; an empty kind removes it), style (finer choices: {card_opacity 0.2–1, 1 or absent is solid; card_blur bool; transition none|fade|slide|zoom|flip|pop; button_shape rounded|square; button_fill gradient|flat|outline|soft; button_size small|large; answer_button_color and answer_button_text_color #rrggbb (the answer bar's button and its text, over the fill and the bar; text defaults to black or white); dock floating; text_scale 0.85–1.4; title_style ribbon|underline|shadow; title_upper bool; celebrate quiet|none; confetti_colors up to 8 #rrggbb; confetti_emoji up to 8 emoji; cheer_text and miss_text up to 40 characters; sound_right and sound_wrong: this adventure's audio media ids}; {} clears it), strict_assets, start_lat, start_lng, start_label, start_radius_m (metres, 5–5000: teams can only begin within it of the starting point; null removes the requirement), slug, brief (replaces the whole brief: {theme, setting, tone, audience, age_range, group_size, minutes, difficulty, distance_m, accessibility, premise, notes} — record what was agreed with the author and design every step against it).",
				},
			},
		},
	}, updateAdventureMeta)

	s.tools.register(ToolDescriptor{
		Name:        "set_adventure_icon",
		Audience:    audienceAuthor,
		Scope:       scopeWrite,
		Description: "Set the adventure's icon (shown in lists, the store and the player's top bar) from image bytes sent base64 in this call. From a phone, or for anything but a small image, use get_upload_link with for: icon instead — the user taps the link and picks the picture. Replaces any icon; shrunk to 512 px.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "data_b64", "mime_type"},
			"properties": map[string]any{
				"slug":      map[string]any{"type": "string", "description": "Adventure slug."},
				"data_b64":  map[string]any{"type": "string", "description": "Base64 of the image bytes: JPEG, PNG, WebP or SVG."},
				"mime_type": map[string]any{"type": "string", "description": `e.g. "image/png".`},
			},
		},
	}, setAdventureIcon)

	s.tools.register(ToolDescriptor{
		Name:        "remove_adventure_icon",
		Audience:    audienceAuthor,
		Scope:       scopeWrite,
		Description: "Remove the adventure's icon; the organisation's logo shows instead.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
			},
		},
	}, removeAdventureIcon)

	s.tools.register(ToolDescriptor{
		Name:        "delete_adventure",
		Audience:    audienceAuthor,
		Scope:       scopeWrite,
		Description: "Delete an adventure and everything it owns — its graph versions, draft and media. Refuses if the adventure still backs any events or assignments — clean those up via the arranger UI first. Requires confirm:true to actually run; without it the tool reports what would be deleted.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug":    map[string]any{"type": "string", "description": "Adventure to delete."},
				"confirm": map[string]any{"type": "boolean", "description": "Must be true to actually delete. Without it, the tool returns a dry-run summary."},
			},
		},
	}, deleteAdventure)
}

/* ---------------- Handlers ---------------- */

// listAdventures is admin.ListAdventures — the caller's org's own rows,
// the published catalog, and anything assigned or shared to the org —
// trimmed to what an assistant picks an adventure by.
func listAdventures(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListAdventures, "GET", "/api/admin/adventures", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var resp struct {
		Adventures []struct {
			Slug       string `json:"slug"`
			Title      string `json:"title"`
			Language   string `json:"language"`
			MineToEdit bool   `json:"mine_to_edit"`
			Playable   bool   `json:"playable"`
		} `json:"adventures"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errorResult("decoding adventures: " + err.Error()), nil
	}
	type row struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		Language string `json:"language,omitempty"`
		CanEdit  bool   `json:"can_edit"`
		Playable bool   `json:"playable"`
	}
	out := []row{}
	for _, a := range resp.Adventures {
		out = append(out, row{a.Slug, a.Title, a.Language, a.MineToEdit, a.Playable})
	}
	b, _ := json.Marshal(out)
	return textResult(string(b)), nil
}

// updateAdventureMeta forwards a partial patch to the admin layer's
// shared meta-update core. Only the keys the LLM included in its
// patch are applied; the rest of the row is untouched.
func updateAdventureMeta(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug  string          `json:"slug"`
		Patch json.RawMessage `json:"patch"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if len(p.Patch) == 0 {
		return errorResult("patch is required"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	advID, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me)
	if err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot edit this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	held, err := admin.UpdateAdventureMetaJSON(ctx, s.DB, p.Slug, p.Patch)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if s.Admin != nil {
		s.Admin.AdventureChanged(ctx, advID)
	}
	if len(held) > 0 {
		body, _ := json.Marshal(map[string]any{"ok": true, "held_for_review": held,
			"note": "Something of this adventure's is for sale, so these presentation changes wait for review: " +
				"they reach players once a reviewer approves them. Ask the user to confirm the content follows the terms, then call submit_listing_for_review with adventure_presentation."})
		return textResult(string(body)), nil
	}
	return textResult(`{"ok":true}`), nil
}

// deleteAdventure removes the named adventure and everything it
// owns. Two-phase: without confirm:true the tool returns a summary
// of what would be deleted (its puzzle count) so the
// model can show the user something concrete before committing.
// With confirm:true it actually runs the cascade.
func deleteAdventure(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	advID, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me)
	if err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot edit this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	if !p.Confirm {
		// Dry-run: report what's about to disappear so the model
		// can show the user a concrete preview ("delete X with 12
		// puzzles?") before re-calling with confirm.
		var puzzles int
		_ = s.DB.QueryRowContext(ctx,
			`SELECT `+admin.PuzzleCountSQL()+` FROM adventures a WHERE a.id = $1`, advID).Scan(&puzzles)
		body, _ := json.MarshalIndent(map[string]any{
			"would_delete": map[string]any{
				"slug":    p.Slug,
				"puzzles": puzzles,
			},
			"hint": "re-call with confirm:true to actually delete",
		}, "", "  ")
		return textResult(string(body)), nil
	}
	if err := admin.DeleteAdventureBySlug(ctx, s.DB, p.Slug); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(`{"ok":true,"deleted":"` + p.Slug + `"}`), nil
}

// setAdventureIcon stores a base64-encoded image as either the
// adventure's avatar or a single puzzle image. Re-calling on the
// same (slug, target) replaces — for avatars that's a single
// UPSERT-shaped overwrite via storage.PutAdventureAvatar; for
// puzzles we delete the page's existing images and insert this one
// as the page's sole image at idx 0. Bytes go through the same
// imageutil pipeline the multipart UI uses (shrink to 512 px, force
// JPEG/PNG re-encode for raster) so storage stays consistent
// regardless of which surface produced it.
func setAdventureIcon(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug     string `json:"slug"`
		DataB64  string `json:"data_b64"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.DataB64 == "" {
		return errorResult("data_b64 is required"), nil
	}
	if !strings.HasPrefix(p.MimeType, "image/") {
		return errorResult(`mime_type must start with "image/"`), nil
	}
	// Accept both standard and raw (no-padding) base64; some clients
	// strip the trailing '=' for compactness.
	data, err := base64.StdEncoding.DecodeString(p.DataB64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(p.DataB64)
	}
	if err != nil {
		return errorResult("data_b64 is not valid base64: " + err.Error()), nil
	}
	if maxBytes := admin.MaxImageBytes(); int64(len(data)) > maxBytes {
		return errorResult(fmt.Sprintf("image too large: %d bytes (cap %d). Lower the resolution or shrink before encoding.", len(data), maxBytes)), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	if _, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me); err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot edit this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	{
		normalized, mime, nerr := imageutil.NormalizeUpload(data, p.MimeType, imageutil.DefaultMaxDim)
		if errors.Is(nerr, imageutil.ErrUnsupportedMIME) {
			return errorResult("an icon is PNG, JPEG, WebP or SVG"), nil
		}
		if nerr != nil {
			return errorResult("unreadable image: " + nerr.Error()), nil
		}
		if s.Storage == nil {
			return errorResult("storage backend not configured on this server"), nil
		}
		held, herr := admin.HoldAdventureIcon(ctx, s.DB, p.Slug, normalized, mime)
		if herr != nil {
			return errorResult("store failed: " + herr.Error()), nil
		}
		if held {
			return textResult(`{"ok":true,"held_for_review":["icon"],"note":"The new icon waits for review before players see it: submit it with submit_listing_for_review (adventure_presentation), with the user's confirmation."}`), nil
		}
		if err := s.Storage.PutAdventureAvatar(ctx, p.Slug, normalized, mime); err != nil {
			return errorResult("store failed: " + err.Error()), nil
		}
		body, _ := json.MarshalIndent(map[string]any{
			"slug":      p.Slug,
			"mime_type": mime,
			"size":      len(normalized),
		}, "", "  ")
		return textResult(string(body)), nil
	}

}

// removeAdventureIcon clears the adventure's cover icon via the same
// storage seam the multipart DELETE handler uses. Idempotent.
func removeAdventureIcon(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if _, _, err := authoriseEdit(ctx, s, p.Slug); err != nil {
		return *err, nil
	}
	if s.Storage == nil {
		return errorResult("storage backend not configured on this server"), nil
	}
	if err := s.Storage.DeleteAdventureAvatar(ctx, p.Slug); err != nil {
		return errorResult("clear failed: " + err.Error()), nil
	}
	if err := admin.DropAdventureIconProposal(ctx, s.DB, p.Slug); err != nil {
		return errorResult("clear failed: " + err.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"icon_removed":true}`, p.Slug)), nil
}

// authoriseEdit consolidates the auth + edit-permission boilerplate
// shared by every mutating MCP tool. Returns advID and the user, or
// a populated ToolCallResult when the caller should bail (auth
// missing / forbidden / lookup failed). The caller dereferences and
// returns the result as-is — keeps each tool handler under ~30
// lines without a blanket "errAuth" sentinel.
func authoriseEdit(ctx context.Context, s *Server, slug string) (int64, auth.User, *ToolCallResult) {
	me, ok := auth.FromContext(ctx)
	if !ok {
		r := errorResult("no authenticated caller")
		return 0, auth.User{}, &r
	}
	advID, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, slug, me)
	if err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			r := errorResult("forbidden: you cannot edit this adventure")
			return 0, auth.User{}, &r
		}
		r := errorResult("lookup: " + err.Error())
		return 0, auth.User{}, &r
	}
	return advID, me, nil
}

// getAdventure is the adventure's settings as the designer loads them,
// trimmed to what an assistant works from.
func getAdventure(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.LoadAdventureForEdit, "GET", "/api/admin/adventures/"+p.Slug+"/edit", map[string]string{"slug": p.Slug}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return errorResult("decoding the adventure: " + err.Error()), nil
	}
	// The rendered intro and outro repeat their text; shares and the
	// catalog listing are the platform's, not the author's.
	for _, k := range []string{"intro_html", "outro_html", "shares", "published"} {
		delete(m, k)
	}
	if v, ok := m["avatar_url"]; ok {
		if u, _ := v.(string); u != "" && strings.HasPrefix(u, "/") {
			v = strings.TrimRight(s.Admin.PublicURL, "/") + u
		}
		m["icon_url"] = v
		delete(m, "avatar_url")
	}
	out, _ := json.Marshal(m)
	return textResult(string(out)), nil
}
