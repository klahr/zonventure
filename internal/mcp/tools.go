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
		return u.Role != auth.RoleAdmin
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
		Description: "List adventures visible to the caller. Returns slug, title, language, lane and puzzle counts. Use this before generating new content to avoid slug collisions and to reference an existing adventure as a template.",
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
		Name:        "add_puzzle",
		Scope:       scopeWrite,
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
		Name:        "set_lane_mode",
		Scope:       scopeWrite,
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
		Name:        "update_puzzle",
		Scope:       scopeWrite,
		Description: "Replace the content of an existing puzzle. Identified by (slug, page_idx) — same idx the export shape uses. The page payload is the full per-puzzle shape (title, prompt, accept[], lat, lng, lane_idx, optional radius_m, optional disabled, optional notes, optional hints[]); fields not present default to their zero values, so passing a partial object will clear the omitted ones (e.g. omitting `accept` empties the answer list). Image / audio attachments are managed by attach_image — they're not editable here. Use this to translate or rewrite an existing puzzle without losing its images or its position in the lane.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "page"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle to replace (matches export shape)."},
				"page": map[string]any{
					"type":        "object",
					"description": "Full per-puzzle shape — same fields as one element of envelope.pages[]. Omitted fields are reset to defaults; pass the puzzle's existing values for anything you want to keep.",
				},
			},
		},
	}, updatePuzzle)

	s.tools.register(ToolDescriptor{
		Name:        "attach_image",
		Scope:       scopeWrite,
		Description: "Attach an image to an adventure. Set target=\"avatar\" for the adventure cover/icon, or pass a numeric page index (matching the export shape) to attach to that puzzle. The image bytes travel base64-encoded inside this call — there is no URL fetch path. Re-calling for the same (slug, target) replaces what was there: avatar overwrites, puzzle deletes existing puzzle images then stores this one as the puzzle's only image. Server normalises (shrinks to 512 px max dim, re-encodes JPEG/PNG) before storing, mirroring the multipart UI flow. Per-image cap is ZV_MAX_IMAGE_BYTES (default 5 MiB on the decoded bytes).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "target", "data_b64", "mime_type"},
			"properties": map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Adventure to attach the image to.",
				},
				"target": map[string]any{
					"description": `"avatar" for the adventure cover icon, or a non-negative integer puzzle index (page idx, matching envelope.pages[i].idx).`,
					"oneOf": []any{
						map[string]any{"type": "string", "enum": []string{"avatar"}},
						map[string]any{"type": "integer", "minimum": 0},
					},
				},
				"data_b64": map[string]any{
					"type":        "string",
					"description": "Base64 (standard alphabet, with or without padding) of the raw image bytes. Avatars accept JPEG/PNG/WebP/SVG; puzzle images accept JPEG/PNG/WebP.",
				},
				"mime_type": map[string]any{
					"type":        "string",
					"description": `Declared content type, e.g. "image/jpeg", "image/png", "image/webp", "image/svg+xml" (avatar only). Must start with "image/".`,
				},
				"alt_text": map[string]any{
					"type":        "string",
					"description": "Optional accessibility description for puzzle images (ignored for avatars). The arranger UI surfaces this and players' screen readers read it aloud — write what the image shows, not where the player should go.",
				},
			},
		},
	}, attachImage)

	s.tools.register(ToolDescriptor{
		Name:        "delete_puzzle",
		Scope:       scopeWrite,
		Description: "Remove one puzzle from an adventure. Identified by (slug, page_idx). Subsequent puzzles slide down so the global ordering stays gap-free. Idempotent in spirit but errors if the idx doesn't exist (safer than silent no-op). Cannot be undone.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle to remove."},
			},
		},
	}, deletePuzzle)

	s.tools.register(ToolDescriptor{
		Name:        "move_puzzle",
		Scope:       scopeWrite,
		Description: "Reorder a puzzle within an adventure. from_idx is the puzzle's current index, to_idx the new position. Optional lane_idx reassigns the puzzle to a different lane in the same operation; omit it to leave the lane assignment alone. to_idx is clamped to the last position so passing a large value moves to the end.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "from_idx", "to_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"from_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Current index of the puzzle."},
				"to_idx":   map[string]any{"type": "integer", "minimum": 0, "description": "Target index (clamped to last position)."},
				"lane_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Optional new lane index. Omit to leave the lane unchanged."},
			},
		},
	}, movePuzzle)

	s.tools.register(ToolDescriptor{
		Name:        "create_lane",
		Scope:       scopeWrite,
		Description: "Append a new lane to the adventure at the next idx. Mode defaults to \"linear\" (every team visits puzzles in idx order); pass \"shuffled\" to give each team a deterministic cyclic shift. Name is the human-facing label shown in the editor / runbar; empty falls back to \"Lane N\".",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to add the lane to."},
				"mode": map[string]any{"type": "string", "enum": []string{"linear", "shuffled"}, "description": `"linear" or "shuffled". Defaults to "linear".`},
				"name": map[string]any{"type": "string", "description": "Human-facing lane name. Optional."},
			},
		},
	}, createLane)

	s.tools.register(ToolDescriptor{
		Name:        "update_lane",
		Scope:       scopeWrite,
		Description: "Patch a lane's mode and/or name addressed by (slug, lane_idx). Either field may be omitted; passing neither errors. Use to translate a lane name or flip its mode without going through set_lane_mode + a separate rename.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "lane_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the lane."},
				"lane_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the lane to update."},
				"mode":     map[string]any{"type": "string", "enum": []string{"linear", "shuffled"}, "description": `New mode — "linear" or "shuffled". Optional.`},
				"name":     map[string]any{"type": "string", "description": "New name. Empty string clears the name. Optional."},
			},
		},
	}, updateLane)

	s.tools.register(ToolDescriptor{
		Name:        "delete_lane",
		Scope:       scopeWrite,
		Description: "Remove a lane addressed by (slug, lane_idx). Refuses if the lane still owns any puzzles — move them out via move_puzzle first. Lane indices of remaining lanes are not renumbered automatically; the editor expects a follow-up reorder if you want a gap-free sequence.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "lane_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the lane."},
				"lane_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the lane to delete."},
			},
		},
	}, deleteLane)

	s.tools.register(ToolDescriptor{
		Name:        "attach_audio",
		Scope:       scopeWrite,
		Description: "Attach an audio clip to a puzzle. Bytes travel base64-encoded inside the call (no URL fetch path). Re-calling for the same (slug, page_idx) replaces the puzzle's existing audio. MIME is sniffed server-side from the bytes — the client's declared mime_type is taken as a hint but the audio/* gate is enforced on the sniffed value. Cap is ZV_MAX_AUDIO_BYTES (default 15 MiB on decoded bytes).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "data_b64"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle to attach the audio to."},
				"data_b64": map[string]any{"type": "string", "description": "Base64-encoded audio bytes. MP3, M4A/AAC, OGG/Opus, WAV all accepted."},
				"mime_type": map[string]any{
					"type":        "string",
					"description": `Hint for the declared MIME (e.g. "audio/mpeg"). Server sniffs the bytes and uses the sniffed value regardless.`,
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Optional descriptive caption shown in the arranger UI. Optional.",
				},
			},
		},
	}, attachAudio)

	s.tools.register(ToolDescriptor{
		Name:        "reorder_lanes",
		Scope:       scopeWrite,
		Description: "Rearrange the adventure's lanes by passing the current lane indices in their new order. The list must contain every lane exactly once — partial reorders are rejected. Pages keep their lane assignments; only the lane sequence changes.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "new_order"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure to reorder."},
				"new_order": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "integer", "minimum": 0},
					"description": "Current lane indices in the desired new order. e.g. [2,0,1] means: existing lane 2 becomes lane 0, existing lane 0 becomes 1, existing lane 1 becomes 2.",
				},
			},
		},
	}, reorderLanes)

	s.tools.register(ToolDescriptor{
		Name:        "detach_image",
		Scope:       scopeWrite,
		Description: "Remove the image attached to a puzzle. Idempotent — a puzzle with no image is a no-op. Use this when an image was attached by mistake; for replacing, just call attach_image again (it overwrites).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle whose image should be cleared."},
			},
		},
	}, detachImage)

	s.tools.register(ToolDescriptor{
		Name:        "detach_audio",
		Scope:       scopeWrite,
		Description: "Remove the audio clip attached to a puzzle. Idempotent — a puzzle with no audio is a no-op. For replacing audio, call attach_audio again (it overwrites).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle whose audio should be cleared."},
			},
		},
	}, detachAudio)

	s.tools.register(ToolDescriptor{
		Name:        "update_image_alt",
		Scope:       scopeWrite,
		Description: "Rewrite the alt_text on a puzzle's image without re-uploading the bytes. Useful when translating an existing adventure: the image stays, the screen-reader caption changes language. No-op when the puzzle has no image.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "alt_text"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle whose image alt_text should change."},
				"alt_text": map[string]any{"type": "string", "description": "New accessibility caption. Empty string clears it."},
			},
		},
	}, updateImageAlt)

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
		Name:        "add_hint",
		Scope:       scopeWrite,
		Description: "Append one hint to an existing puzzle. Hints are revealed in idx order in the player UI; delay_seconds gates when each becomes revealable (counted from when the team reached the puzzle). The new hint lands at the next idx after the puzzle's current hints. Use update_puzzle if you'd rather replace the entire hint list in one shot.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "text"},
			"properties": map[string]any{
				"slug":          map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx":      map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle to add the hint to."},
				"text":          map[string]any{"type": "string", "description": "Hint body. Markdown is rendered."},
				"delay_seconds": map[string]any{"type": "integer", "minimum": 0, "description": "Seconds before the hint becomes revealable (default 0 = immediately)."},
			},
		},
	}, addHint)

	s.tools.register(ToolDescriptor{
		Name:        "update_hint",
		Scope:       scopeWrite,
		Description: "Patch a hint's text and/or delay_seconds, addressed by (slug, page_idx, hint_idx). Either field may be omitted; passing neither errors. The hint's position in the reveal order doesn't change — use a delete + add or update_puzzle to reorder.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "hint_idx"},
			"properties": map[string]any{
				"slug":          map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx":      map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle."},
				"hint_idx":      map[string]any{"type": "integer", "minimum": 0, "description": "Index of the hint within the puzzle."},
				"text":          map[string]any{"type": "string", "description": "New hint body. Optional."},
				"delay_seconds": map[string]any{"type": "integer", "minimum": 0, "description": "New delay in seconds. Optional."},
			},
		},
	}, updateHint)

	s.tools.register(ToolDescriptor{
		Name:        "delete_hint",
		Scope:       scopeWrite,
		Description: "Remove one hint from a puzzle. Subsequent hints slide down so the per-page idx sequence stays gap-free. Errors if the hint doesn't exist (safer than silent no-op).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "page_idx", "hint_idx"},
			"properties": map[string]any{
				"slug":     map[string]any{"type": "string", "description": "Adventure containing the puzzle."},
				"page_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the puzzle."},
				"hint_idx": map[string]any{"type": "integer", "minimum": 0, "description": "Index of the hint within the puzzle."},
			},
		},
	}, deleteHint)

	s.tools.register(ToolDescriptor{
		Name:        "delete_adventure",
		Scope:       scopeWrite,
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
		       (SELECT COUNT(*) FROM adventure_lanes l WHERE l.adventure_id = a.id) AS lanes,
		       (SELECT COUNT(*) FROM adventure_pages p WHERE p.adventure_id = a.id) AS puzzles
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
	if err := admin.RefuseIfGraph(ctx, s.DB, advID); err != nil {
		return errorResult(err.Error()), nil
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

// updatePuzzle replaces an existing puzzle's content in place.
// Sibling of addPuzzle: where addPuzzle appends, this rewrites at a
// caller-supplied page_idx so the LLM can translate or revise a
// puzzle without losing its images / hint history that downstream
// tools (attach_image) have wired up.
func updatePuzzle(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string          `json:"slug"`
		PageIdx int             `json:"page_idx"`
		Page    json.RawMessage `json:"page"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
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
	if err := admin.RefuseIfGraph(ctx, s.DB, advID); err != nil {
		return errorResult(err.Error()), nil
	}
	if err := admin.UpdatePageFromExport(ctx, s.DB, advID, p.PageIdx, p.Page); err != nil {
		return errorResult(err.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":     p.Slug,
		"page_idx": p.PageIdx,
		"ok":       true,
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
	if err := admin.RefuseIfGraph(ctx, s.DB, advID); err != nil {
		return errorResult(err.Error()), nil
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
				"slug":  p.Slug,
				"lanes": lanes,
				"pages": pages,
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
		AltText  string          `json:"alt_text,omitempty"`
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
	advID, _, err := admin.AuthorizeAdventureEdit(ctx, s.DB, p.Slug, me)
	if err != nil {
		if errors.Is(err, admin.ErrForbidden) {
			return errorResult("forbidden: you cannot edit this adventure"), nil
		}
		return errorResult("lookup: " + err.Error()), nil
	}
	if err := admin.RefuseIfGraph(ctx, s.DB, advID); err != nil {
		return errorResult(err.Error()), nil
	}

	// target is "avatar" (string) or an integer page idx. We probe by
	// trimming and comparing rather than two unmarshal attempts so the
	// error message stays specific.
	rawTarget := strings.TrimSpace(string(p.Target))
	if rawTarget == `"avatar"` {
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

	var pageIdx int
	if err := json.Unmarshal(p.Target, &pageIdx); err != nil {
		return errorResult(`target must be "avatar" or a non-negative integer page idx`), nil
	}
	if pageIdx < 0 {
		return errorResult("page idx must be >= 0"), nil
	}
	var pageID int64
	err = s.DB.QueryRowContext(ctx,
		`SELECT id FROM adventure_pages WHERE adventure_id = $1 AND idx = $2`,
		advID, pageIdx).Scan(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return errorResult(fmt.Sprintf("no puzzle at idx %d in this adventure", pageIdx)), nil
	}
	if err != nil {
		return errorResult("page lookup: " + err.Error()), nil
	}

	// Shrink + re-encode (raster only — page images don't accept SVG,
	// matching the existing AddPageImage handler).
	shrunk, mime, err := imageutil.Shrink(data, imageutil.DefaultMaxDim)
	if err != nil {
		return errorResult("unreadable image: " + err.Error()), nil
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return errorResult("tx begin: " + err.Error()), nil
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM adventure_page_images WHERE page_id = $1`, pageID); err != nil {
		return errorResult("clear: " + err.Error()), nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO adventure_page_images(page_id, idx, mime_type, size_bytes, content, alt_text)
		VALUES ($1, 0, $2, $3, $4, $5)
	`, pageID, mime, int64(len(shrunk)), shrunk, p.AltText); err != nil {
		return errorResult("insert: " + err.Error()), nil
	}
	if err := tx.Commit(); err != nil {
		return errorResult("commit: " + err.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":      p.Slug,
		"target":    pageIdx,
		"mime_type": mime,
		"size":      len(shrunk),
	}, "", "  ")
	return textResult(string(body)), nil
}

// deletePuzzle removes one puzzle by (slug, page_idx). The admin
// helper compacts subsequent idx values in the same transaction so
// the global ordering stays gap-free.
func deletePuzzle(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		PageIdx int    `json:"page_idx"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.DeletePageByIdx(ctx, s.DB, advID, p.PageIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"deleted_idx":%d}`, p.Slug, p.PageIdx)), nil
}

// movePuzzle relocates a puzzle within an adventure (and optionally
// across lanes). The admin helper handles the parking + renumber
// dance; this wrapper just unpacks the args and forwards.
func movePuzzle(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		FromIdx int    `json:"from_idx"`
		ToIdx   int    `json:"to_idx"`
		LaneIdx *int   `json:"lane_idx,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.MovePage(ctx, s.DB, advID, p.FromIdx, p.ToIdx, p.LaneIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":     p.Slug,
		"from_idx": p.FromIdx,
		"to_idx":   p.ToIdx,
		"lane_idx": p.LaneIdx,
		"ok":       true,
	}, "", "  ")
	return textResult(string(body)), nil
}

// createLane appends a new lane and returns its idx so the model
// can immediately reference it in subsequent move_puzzle / add_puzzle
// calls without needing to re-fetch the envelope.
func createLane(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug string `json:"slug"`
		Mode string `json:"mode,omitempty"`
		Name string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	_, idx, e := admin.CreateLaneRow(ctx, s.DB, advID, p.Mode, p.Name)
	if e != nil {
		return errorResult(e.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":     p.Slug,
		"lane_idx": idx,
		"mode":     p.Mode,
		"name":     p.Name,
	}, "", "  ")
	return textResult(string(body)), nil
}

// updateLane patches a lane's mode and/or name. Either field may be
// omitted from the JSON; passing neither is rejected by the admin
// helper as "nothing to update".
func updateLane(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string  `json:"slug"`
		LaneIdx int     `json:"lane_idx"`
		Mode    *string `json:"mode,omitempty"`
		Name    *string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.UpdateLaneByIdx(ctx, s.DB, advID, p.LaneIdx, p.Mode, p.Name); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"lane_idx":%d,"ok":true}`, p.Slug, p.LaneIdx)), nil
}

// deleteLane removes an empty lane. Refuses if the lane still owns
// puzzles — that's a "data-loss-shaped surprise" guard inherited
// from the multipart UI handler.
func deleteLane(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		LaneIdx int    `json:"lane_idx"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.DeleteLaneByIdx(ctx, s.DB, advID, p.LaneIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"deleted_lane_idx":%d}`, p.Slug, p.LaneIdx)), nil
}

// attachAudio is the audio counterpart of attach_image. Bytes
// travel base64; server sniffs MIME and rejects non-audio/* blobs;
// existing audio on the puzzle is replaced.
func attachAudio(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug        string `json:"slug"`
		PageIdx     int    `json:"page_idx"`
		DataB64     string `json:"data_b64"`
		MimeType    string `json:"mime_type,omitempty"`
		Description string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	if p.DataB64 == "" {
		return errorResult("data_b64 is required"), nil
	}
	data, err := base64.StdEncoding.DecodeString(p.DataB64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(p.DataB64)
	}
	if err != nil {
		return errorResult("data_b64 is not valid base64: " + err.Error()), nil
	}
	advID, _, autherr := laneEdit(ctx, s, p.Slug)
	if autherr != nil {
		return *autherr, nil
	}
	mime, e := admin.PutPageAudio(ctx, s.DB, advID, p.PageIdx, data, p.Description)
	if e != nil {
		return errorResult(e.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":      p.Slug,
		"page_idx":  p.PageIdx,
		"mime_type": mime,
		"size":      len(data),
	}, "", "  ")
	return textResult(string(body)), nil
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

// reorderLanes accepts an array of current lane indices in their
// new order and reassigns lane idx values to match.
func reorderLanes(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug     string `json:"slug"`
		NewOrder []int  `json:"new_order"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if len(p.NewOrder) == 0 {
		return errorResult("new_order must be a non-empty array of lane indices"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.ReorderLanesByIdx(ctx, s.DB, advID, p.NewOrder); e != nil {
		return errorResult(e.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":      p.Slug,
		"new_order": p.NewOrder,
		"ok":        true,
	}, "", "  ")
	return textResult(string(body)), nil
}

// detachImage removes every image on a puzzle. Idempotent.
func detachImage(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		PageIdx int    `json:"page_idx"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.DetachPageImages(ctx, s.DB, advID, p.PageIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"page_idx":%d,"images_cleared":true}`, p.Slug, p.PageIdx)), nil
}

// detachAudio removes every audio clip on a puzzle. Idempotent.
func detachAudio(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		PageIdx int    `json:"page_idx"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.DetachPageAudio(ctx, s.DB, advID, p.PageIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"page_idx":%d,"audio_cleared":true}`, p.Slug, p.PageIdx)), nil
}

// updateImageAlt rewrites alt_text on a puzzle's image without
// requiring a re-upload of the bytes. No-ops when the puzzle has
// no image.
func updateImageAlt(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		PageIdx int    `json:"page_idx"`
		AltText string `json:"alt_text"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.UpdatePageImageAlt(ctx, s.DB, advID, p.PageIdx, p.AltText); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"page_idx":%d,"alt_text":%q}`, p.Slug, p.PageIdx, p.AltText)), nil
}

// addHint appends one hint to a puzzle. Returns the assigned idx
// so the model can immediately update_hint or delete_hint without
// re-fetching the envelope.
func addHint(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug         string `json:"slug"`
		PageIdx      int    `json:"page_idx"`
		Text         string `json:"text"`
		DelaySeconds int    `json:"delay_seconds,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 {
		return errorResult("page_idx must be >= 0"), nil
	}
	if p.Text == "" {
		return errorResult("text is required"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	idx, e := admin.AddHintAtEnd(ctx, s.DB, advID, p.PageIdx, p.Text, p.DelaySeconds)
	if e != nil {
		return errorResult(e.Error()), nil
	}
	body, _ := json.MarshalIndent(map[string]any{
		"slug":          p.Slug,
		"page_idx":      p.PageIdx,
		"hint_idx":      idx,
		"delay_seconds": p.DelaySeconds,
	}, "", "  ")
	return textResult(string(body)), nil
}

// updateHint patches text and/or delay_seconds on an existing hint.
func updateHint(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug         string  `json:"slug"`
		PageIdx      int     `json:"page_idx"`
		HintIdx      int     `json:"hint_idx"`
		Text         *string `json:"text,omitempty"`
		DelaySeconds *int    `json:"delay_seconds,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 || p.HintIdx < 0 {
		return errorResult("page_idx and hint_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.UpdateHintByIdx(ctx, s.DB, advID, p.PageIdx, p.HintIdx, p.Text, p.DelaySeconds); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"page_idx":%d,"hint_idx":%d,"ok":true}`,
		p.Slug, p.PageIdx, p.HintIdx)), nil
}

// deleteHint removes one hint and compacts subsequent idx values
// so the per-page hint sequence stays gap-free.
func deleteHint(ctx context.Context, s *Server, args json.RawMessage) (ToolCallResult, error) {
	var p struct {
		Slug    string `json:"slug"`
		PageIdx int    `json:"page_idx"`
		HintIdx int    `json:"hint_idx"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if p.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if p.PageIdx < 0 || p.HintIdx < 0 {
		return errorResult("page_idx and hint_idx must be >= 0"), nil
	}
	advID, _, err := laneEdit(ctx, s, p.Slug)
	if err != nil {
		return *err, nil
	}
	if e := admin.DeleteHintByIdx(ctx, s.DB, advID, p.PageIdx, p.HintIdx); e != nil {
		return errorResult(e.Error()), nil
	}
	return textResult(fmt.Sprintf(`{"slug":%q,"page_idx":%d,"deleted_hint_idx":%d}`,
		p.Slug, p.PageIdx, p.HintIdx)), nil
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

// laneEdit is authoriseEdit for a tool that edits lane pages, lanes or
// hints: it also refuses an adventure with a published graph, whose
// events never show its lane pages again (admin.ErrGraphAdventure).
func laneEdit(ctx context.Context, s *Server, slug string) (int64, auth.User, *ToolCallResult) {
	advID, me, res := authoriseEdit(ctx, s, slug)
	if res != nil {
		return advID, me, res
	}
	if err := admin.RefuseIfGraph(ctx, s.DB, advID); err != nil {
		r := errorResult(err.Error())
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
