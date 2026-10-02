package mcp

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	switch aud {
	case audienceAny:
		return true
	case audienceAdmin:
		return u.Role == auth.RoleAdmin
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
	out := []ToolDescriptor{}
	for _, d := range r.descriptors {
		if audienceAllows(me, d.Audience) {
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
	s.registerMediaTools()
	s.registerGuideTool()
	// Phase 4 — invitations + ICS export. invitations.go.
	s.registerInvitationTools()
	// Slice 3 polish — CSV results export and a static lifecycle
	// reference doc the model can fetch as a one-shot orientation.
	s.registerResultsTools()
	s.registerLifecycleTool()

	s.tools.register(ToolDescriptor{
		Name:        "list_adventures",
		Scope:       scopeRead,
		Description: "List adventures visible to the caller. Returns slug, title, language and puzzle count. Use this before generating new content to avoid slug collisions and to reference an existing adventure as a template.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listAdventures)

	s.tools.register(ToolDescriptor{
		Name:        "get_adventure",
		Scope:       scopeRead,
		Description: "Read one adventure as the same JSON envelope create_adventure accepts. Use as a template for variants, or to inspect what's already there before edits. Binary assets (image / audio bytes) are NOT included — only the metadata that references them.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Slug of the adventure to fetch. Use list_adventures to discover available slugs.",
				},
			},
		},
	}, getAdventure)

	s.tools.register(ToolDescriptor{
		Name:        "validate_adventure",
		Scope:       scopeRead,
		Description: "Dry-run an envelope through the importer and report any validation errors WITHOUT writing anything. Run this first when generating a new adventure so you can self-correct (bad slug, missing language, slug collision) before committing with create_adventure.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"envelope"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Override the envelope's slug for the dry-run. Optional.",
				},
				"envelope": map[string]any{
					"type":        "object",
					"description": "Adventure envelope to validate. Same shape create_adventure accepts.",
				},
			},
		},
	}, validateAdventure)

	s.tools.register(ToolDescriptor{
		Name:        "create_adventure",
		Scope:       scopeWrite,
		Description: "Create a new adventure from a structured envelope (matching the export/import JSON shape). Run validate_adventure first to surface errors. Assets (images, audio) cannot be uploaded via MCP — leave them out and have the user attach files in the arranger UI.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"envelope"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Override the envelope's slug (e.g. for collision avoidance). Optional.",
				},
				"envelope": map[string]any{
					"type":        "object",
					"description": "Adventure envelope. See exportAdventureDTO in internal/admin/portability.go for the field shape.",
				},
			},
		},
	}, createAdventure)

	s.tools.register(ToolDescriptor{
		Name:        "update_adventure_meta",
		Scope:       scopeWrite,
		Description: "Patch an adventure's metadata: rename, recolor, edit intro/outro, change language, set start coords, flip strict_assets. Catalog listing (published) is the platform operator's and is refused here. Only present keys are applied — omit a field to leave it untouched. Use {field: null} on start_lat/start_lng to clear those columns.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "patch"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to update."},
				"patch": map[string]any{
					"type":        "object",
					"description": "Partial patch matching the admin updateAdventureRequest shape. Common fields: title, description, author, intro_text, outro_text, language, accent_color, accent_secondary_color, background_color, text_color, submit_bg_color, font, heading_font, corners (rounded|soft|sharp), cards (raised|flat), card_outline (bool), strict_assets, start_lat, start_lng, start_label, slug, brief (replaces the whole brief: {theme, setting, tone, audience, age_range, group_size, minutes, difficulty, distance_m, accessibility, premise, notes} — record what was agreed with the author and design every step against it).",
				},
			},
		},
	}, updateAdventureMeta)

	s.tools.register(ToolDescriptor{
		Name:        "attach_image",
		Scope:       scopeWrite,
		Description: "Set the adventure's cover/icon (target=\"avatar\"). Pictures for steps are graph media: use upload_adventure_media. The image bytes travel base64-encoded inside this call — there is no URL fetch path. Re-calling replaces the cover. Server normalises (shrinks to 512 px max dim, re-encodes JPEG/PNG) before storing, mirroring the multipart UI flow. Per-image cap is ZV_MAX_IMAGE_BYTES (default 5 MiB on the decoded bytes).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "target", "data_b64", "mime_type"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Adventure to attach the image to.",
				},
				"target": map[string]any{
					"description": `"avatar", the adventure cover icon.`,
					"type":        "string",
					"enum":        []string{"avatar"},
				},
				"data_b64": map[string]any{
					"type":        "string",
					"description": "Base64 (standard alphabet, with or without padding) of the raw image bytes: JPEG, PNG, WebP or SVG.",
				},
				"mime_type": map[string]any{
					"type":        "string",
					"description": `Declared content type, e.g. "image/jpeg", "image/png", "image/webp", "image/svg+xml". Must start with "image/".`,
				},
			},
		},
	}, attachImage)

	s.tools.register(ToolDescriptor{
		Name:        "detach_avatar",
		Scope:       scopeWrite,
		Description: "Clear the adventure's avatar / cover icon. Idempotent — calling on an avatar-less adventure is a no-op.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to clear the avatar on."},
			},
		},
	}, detachAvatar)

	s.tools.register(ToolDescriptor{
		Name:        "delete_adventure",
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

type adventureSummary struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Language     string `json:"language,omitempty"`
	PuzzleCount  int    `json:"puzzle_count"`
	OwnedByOrgID *int64 `json:"owned_by_org_id,omitempty"`
}

// listAdventures is the read-side proof of life. The visibility
// rules mirror admin.ListAdventures: the caller's org's own rows, the
// published catalog, and anything assigned or shared to the org.
// Refactoring admin.ListAdventures to share a listing helper would be
// cleaner — for the scaffold we duplicate the query and call out the
// seam.
//
// TODO(refactor): extract a shared lister into internal/adventure
// and have both admin.ListAdventures and this tool call it.
func listAdventures(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	if me.OrgID == nil {
		return errorResult("no active organization"), nil
	}
	q := `
		SELECT a.slug, a.title, COALESCE(a.language,''), a.org_id,
		       ` + admin.PuzzleCountSQL() + ` AS puzzles
		FROM adventures a
		WHERE a.org_id = $1
		   OR ` + admin.CatalogFilter() + `
		   OR EXISTS (SELECT 1 FROM adventure_assignments aa WHERE aa.adventure_slug = a.slug AND aa.org_id = $1)
		   OR EXISTS (SELECT 1 FROM adventure_shares s WHERE s.adventure_slug = a.slug AND s.org_id = $1)
		ORDER BY a.title
	`
	rows, err := s.DB.QueryContext(ctx, q, *me.OrgID)
	if err != nil {
		return errorResult("query failed: " + err.Error()), nil
	}
	defer rows.Close()
	var out []adventureSummary
	for rows.Next() {
		var a adventureSummary
		var orgID sql.NullInt64
		if err := rows.Scan(&a.Slug, &a.Title, &a.Language, &orgID, &a.PuzzleCount); err != nil {
			return errorResult("scan failed: " + err.Error()), nil
		}
		if orgID.Valid {
			a.OwnedByOrgID = &orgID.Int64
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return errorResult("rows: " + err.Error()), nil
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return errorResult("encode: " + err.Error()), nil
	}
	return textResult(string(body)), nil
}

// createAdventure writes the manifest as a new adventure owned by
// the caller's org. Wraps admin.ImportEnvelopeJSON, which is the
// same code path the multipart upload handler uses — we just
// hand it the JSON directly and pass no asset bytes.
//
// The manifest the LLM produces is wrapped in an envelope with the
// expected `format` tag if the model omitted it; that single field
// is the only thing we want to be lenient about, since every other
// validation (slug shape, language tag) is information
// the model needs to learn from. Wrap-once-then-let-it-fail beats
// "model writes minimal JSON and we silently fill defaults".
func createAdventure(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug     string          `json:"slug,omitempty"`
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if len(p.Envelope) == 0 {
		return errorResult("envelope is required"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	manifest, err := wrapEnvelope(p.Envelope)
	if err != nil {
		return errorResult("envelope: " + err.Error()), nil
	}
	advID, slug, title, err := admin.ImportEnvelopeJSON(ctx, s.DB, manifest, p.Slug, me)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"id":    advID,
		"slug":  slug,
		"title": title,
	}, "", "  ")
	return textResult(string(body)), nil
}

// getAdventure returns one adventure's full envelope (metadata and
// graph) as JSON the model can read or copy
// into a follow-up create_adventure call. Visibility follows the
// same staff-side rules ListAdventures applies.
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
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	// Edit authorization, not read: the exported envelope carries the
	// full answer key and hint bodies. The HTTP graph export is
	// edit-gated for the same reason, and AdventureDetail strips answers
	// for non-editors — a read-scoped token or a foreign-org viewer must
	// not be able to dump the solutions here.
	advID, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me)
	if err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot read this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	body, err := admin.ExportAdventureEnvelopeJSON(ctx, s.DB, advID)
	if err != nil {
		return errorResult("export: " + err.Error()), nil
	}
	// Two blocks: a markdown view first (renders nicely in the tool
	// panel — puzzle prompts are themselves markdown so they pick
	// up real headings / emphasis instead of showing as JSON-escaped
	// strings), and the JSON envelope second so the model can feed
	// it back into create_adventure / update_adventure_meta. The
	// horizontal rule + fenced code block is the seam so the two
	// blocks don't visually run into each other when a client
	// concatenates content blocks for display.
	return multiTextResult(
		renderEnvelopeMarkdown(body),
		"\n\n---\n\n```json\n"+string(body)+"\n```",
	), nil
}

// validateAdventure runs the manifest through the importer's full
// validation pipeline against a transaction that's rolled back at
// the end. The model uses this to catch slug collisions, language-
// tag typos before committing.
func validateAdventure(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug     string          `json:"slug,omitempty"`
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if len(p.Envelope) == 0 {
		return errorResult("envelope is required"), nil
	}
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	manifest, err := wrapEnvelope(p.Envelope)
	if err != nil {
		return errorResult("envelope: " + err.Error()), nil
	}
	if err := admin.ValidateEnvelopeJSON(ctx, s.DB, manifest, p.Slug, me); err != nil {
		return errorResult("validation failed: " + err.Error()), nil
	}
	return textResult(`{"ok":true}`), nil
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
	if _, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me); err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot edit this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	if err := admin.UpdateAdventureMetaJSON(ctx, s.DB, p.Slug, p.Patch); err != nil {
		return errorResult(err.Error()), nil
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

// attachImage stores a base64-encoded image as either the
// adventure's avatar or a single puzzle image. Re-calling on the
// same (slug, target) replaces — for avatars that's a single
// UPSERT-shaped overwrite via storage.PutAdventureAvatar; for
// puzzles we delete the page's existing images and insert this one
// as the page's sole image at idx 0. Bytes go through the same
// imageutil pipeline the multipart UI uses (shrink to 512 px, force
// JPEG/PNG re-encode for raster) so storage stays consistent
// regardless of which surface produced it.
func attachImage(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug     string          `json:"slug"`
		Target   json.RawMessage `json:"target"`
		DataB64  string          `json:"data_b64"`
		MimeType string          `json:"mime_type"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if len(p.Target) == 0 {
		return errorResult(`target is required ("avatar" or a non-negative page idx)`), nil
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
	if strings.TrimSpace(string(p.Target)) != `"avatar"` {
		return errorResult(`target must be "avatar"; a step's pictures are graph media (upload_adventure_media)`), nil
	}
	{
		normalized, mime, nerr := imageutil.NormalizeUpload(data, p.MimeType, imageutil.DefaultMaxDim)
		if errors.Is(nerr, imageutil.ErrUnsupportedMIME) {
			return errorResult("avatars accept PNG, JPEG, WebP, or SVG"), nil
		}
		if nerr != nil {
			return errorResult("unreadable image: " + nerr.Error()), nil
		}
		if s.Storage == nil {
			return errorResult("storage backend not configured on this server"), nil
		}
		if err := s.Storage.PutAdventureAvatar(ctx, p.Slug, normalized, mime); err != nil {
			return errorResult("store failed: " + err.Error()), nil
		}
		body, _ := json.MarshalIndent(map[string]any{
			"slug":      p.Slug,
			"target":    "avatar",
			"mime_type": mime,
			"size":      len(normalized),
		}, "", "  ")
		return textResult(string(body)), nil
	}

}

// detachAvatar clears the adventure's cover icon via the same
// storage seam the multipart DELETE handler uses. Idempotent.
func detachAvatar(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
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
	return textResult(fmt.Sprintf(`{"slug":%q,"avatar_cleared":true}`, p.Slug)), nil
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

// wrapEnvelope normalises whatever shape the LLM produced into the
// {format, exported_at, adventure} envelope the importer expects.
//
// Three accepted inputs, in priority order:
//  1. A full envelope with a "format" key — passed through as-is.
//  2. An object with an "adventure" key but no format — we add
//     the format tag so the importer doesn't reject it.
//  3. A bare adventure object (with "title" at the top level) — we wrap it in {format, adventure: <obj>}.
//
// Anything else falls through and the importer's manifest-parse
// error surfaces verbatim — that's the signal the model needs.
func wrapEnvelope(raw json.RawMessage) ([]byte, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if _, hasFormat := probe["format"]; hasFormat {
		return raw, nil
	}
	if _, hasAdventure := probe["adventure"]; hasAdventure {
		probe["format"] = json.RawMessage(`"zonventure-adventure-v1"`)
		return json.Marshal(probe)
	}
	wrapped := map[string]json.RawMessage{
		"format":    json.RawMessage(`"zonventure-adventure-v1"`),
		"adventure": raw,
	}
	return json.Marshal(wrapped)
}
