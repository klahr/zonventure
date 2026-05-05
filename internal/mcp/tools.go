package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"git.sr.ht/~klahr/zv/internal/admin"
	"git.sr.ht/~klahr/zv/internal/auth"
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

// registry is the immutable-after-startup tool table.
type registry struct {
	descriptors []ToolDescriptor
	handlers    map[string]toolHandler
}

func newRegistry() *registry {
	return &registry{handlers: map[string]toolHandler{}}
}

func (r *registry) register(d ToolDescriptor, h toolHandler) {
	r.descriptors = append(r.descriptors, d)
	r.handlers[d.Name] = h
}

func (r *registry) list() []ToolDescriptor { return r.descriptors }

func (r *registry) call(ctx context.Context, s *Server, name string, args json.RawMessage) (ToolCallResult, error) {
	h, ok := r.handlers[name]
	if !ok {
		return ToolCallResult{}, fmt.Errorf("%w: %s", errUnknownTool, name)
	}
	return h(ctx, s, args)
}

// registerTools is the single place new tools get wired in. Keep
// the descriptors near the handlers so a contributor sees the
// public schema and the server-side behaviour together.
func (s *Server) registerTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_adventures",
		Description: "List adventures visible to the caller. Returns slug, title, language, lane and puzzle counts. Use this before generating new content to avoid slug collisions and to reference an existing adventure as a template.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listAdventures)

	s.tools.register(ToolDescriptor{
		Name:        "get_adventure",
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
		Description: "Dry-run an envelope through the importer and report any validation errors WITHOUT writing anything. Run this first when generating a new adventure so you can self-correct (bad slug, missing language, broken lane refs, slug collision) before committing with create_adventure.",
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
		Name:        "add_puzzle",
		Description: "Append one puzzle to an existing adventure. Use this to grow an adventure incrementally — cheaper than a full create_adventure rewrite. Lane is selected by lane_idx (matching the export shape); unknown idx falls back to the first lane.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Adventure to append the puzzle to.",
				},
				"page": map[string]any{
					"type":        "object",
					"description": "Puzzle definition. Same shape as one element of envelope.pages[]: title, prompt, accept[], lat, lng, lane_idx, optional radius_m, optional hints[]. Image / audio fields not supported here.",
				},
			},
		},
	}, addPuzzle)

	s.tools.register(ToolDescriptor{
		Name:        "update_adventure_meta",
		Description: "Patch an adventure's metadata: rename, recolor, edit intro/outro, change language, set start coords, flip published/strict_assets. Only present keys are applied — omit a field to leave it untouched. Use {field: null} on start_lat/start_lng to clear those columns.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "patch"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to update."},
				"patch": map[string]any{
					"type":        "object",
					"description": "Partial patch matching the admin updateAdventureRequest shape. Common fields: title, description, author, intro_text, outro_text, language, accent_color, accent_secondary_color, background_color, text_color, submit_bg_color, published, strict_assets, start_lat, start_lng, start_label, slug.",
				},
			},
		},
	}, updateAdventureMeta)

	s.tools.register(ToolDescriptor{
		Name:        "set_lane_mode",
		Description: `Flip a lane between "linear" (every team visits puzzles in idx order) and "shuffled" (each team gets a deterministic cyclic shift to spread starts). Lane is identified by lane_idx (the export-shape index, not a DB id).`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "lane_idx", "mode"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the lane."},
				"lane_idx": map[string]any{"type": "integer", "description": "Lane index (0 = first lane)."},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"linear", "shuffled"},
					"description": `"linear" or "shuffled".`,
				},
			},
		},
	}, setLaneMode)

	s.tools.register(ToolDescriptor{
		Name:        "delete_adventure",
		Description: "Delete an adventure and every page / lane / image it owns. Refuses if the adventure still backs any events or assignments — clean those up via the arranger UI first. Requires confirm:true to actually run; without it the tool reports what would be deleted.",
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
	LaneCount    int    `json:"lane_count"`
	PuzzleCount  int    `json:"puzzle_count"`
	OwnedByOrgID *int64 `json:"owned_by_org_id,omitempty"`
}

// listAdventures is the read-side proof of life. The visibility
// rules mirror admin.ListAdventures: org-owned rows are visible to
// members of that org, NULL-owned rows are admin-curated globals
// visible to everyone. Refactoring admin.ListAdventures to share a
// listing helper would be cleaner — for the scaffold we duplicate
// the query and call out the seam.
//
// TODO(refactor): extract a shared lister into internal/adventure
// and have both admin.ListAdventures and this tool call it.
func listAdventures(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	const q = `
		SELECT a.slug, a.title, COALESCE(a.language,''), a.org_id,
		       (SELECT COUNT(*) FROM adventure_lanes l WHERE l.adventure_id = a.id) AS lanes,
		       (SELECT COUNT(*) FROM adventure_pages p WHERE p.adventure_id = a.id) AS puzzles
		FROM adventures a
		WHERE a.org_id IS NULL OR a.org_id = $1
		ORDER BY a.title
	`
	var orgArg sql.NullInt64
	if me.OrgID != nil {
		orgArg = sql.NullInt64{Int64: *me.OrgID, Valid: true}
	}
	rows, err := s.DB.QueryContext(ctx, q, orgArg)
	if err != nil {
		return errorResult("query failed: " + err.Error()), nil
	}
	defer rows.Close()
	var out []adventureSummary
	for rows.Next() {
		var a adventureSummary
		var orgID sql.NullInt64
		if err := rows.Scan(&a.Slug, &a.Title, &a.Language, &orgID, &a.LaneCount, &a.PuzzleCount); err != nil {
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
// validation (slug shape, language tag, lane modes) is information
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

// getAdventure returns one adventure's full envelope (metadata,
// lanes, pages, hint refs) as JSON the model can read or copy
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
	advID, _, err := admin.AuthorizeAdventureRead(ctx, s.DB, p.Slug, me)
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
	// it back into create_adventure / update_adventure_meta.
	return multiTextResult(
		renderEnvelopeMarkdown(body),
		string(body),
	), nil
}

// validateAdventure runs the manifest through the importer's full
// validation pipeline against a transaction that's rolled back at
// the end. The model uses this to catch slug collisions, language-
// tag typos, and lane-mode invalid values before committing.
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

// addPuzzle appends one page to an existing adventure. Matches the
// per-page shape inside an envelope so the model can re-use what it
// already knows from create_adventure.
func addPuzzle(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug string          `json:"slug"`
		Page json.RawMessage `json:"page"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if len(p.Page) == 0 {
		return errorResult("page is required"), nil
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
	pageID, err := admin.AddPageFromExport(ctx, s.DB, advID, p.Page)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":    p.Slug,
		"page_id": pageID,
	}, "", "  ")
	return textResult(string(body)), nil
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

// setLaneMode flips a lane between linear and shuffled. Lane is
// identified by idx (export-shape) rather than DB id; the helper
// resolves to the adventure id behind the scenes.
func setLaneMode(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		LaneIdx int    `json:"lane_idx"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.Mode == "" {
		return errorResult("mode is required"), nil
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
	if err := admin.SetLaneMode(ctx, s.DB, advID, p.LaneIdx, p.Mode); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(`{"ok":true}`), nil
}

// deleteAdventure removes the named adventure and everything it
// owns. Two-phase: without confirm:true the tool returns a summary
// of what would be deleted (lane / page / hint counts) so the
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
		// puzzles and 3 lanes?") before re-calling with confirm.
		var lanes, pages int
		_ = s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM adventure_lanes WHERE adventure_id = $1`, advID).Scan(&lanes)
		_ = s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM adventure_pages WHERE adventure_id = $1`, advID).Scan(&pages)
		body, _ := json.MarshalIndent(map[string]any{
			"would_delete": map[string]any{
				"slug":   p.Slug,
				"lanes":  lanes,
				"pages":  pages,
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

// wrapEnvelope normalises whatever shape the LLM produced into the
// {format, exported_at, adventure} envelope the importer expects.
//
// Three accepted inputs, in priority order:
//  1. A full envelope with a "format" key — passed through as-is.
//  2. An object with an "adventure" key but no format — we add
//     the format tag so the importer doesn't reject it.
//  3. A bare adventure object (with "title", "lanes", "pages" at
//     the top level) — we wrap it in {format, adventure: <obj>}.
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
