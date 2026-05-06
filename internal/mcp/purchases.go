package mcp

import (
	"context"
	"encoding/json"
	"strconv"

	"git.sr.ht/~klahr/zv/internal/auth"
)

// Phase 3: assignments / billing.
//
// Q1 invariant: the LLM never triggers a real-money charge. On a
// Stripe-enabled deployment, create_purchase refuses outright and
// points the caller back to the web checkout flow. On a self-host
// deployment without Stripe, free / simulated purchases are fine —
// they're how the canonical first-event flow works (1-slot free
// assignment, growable via top-ups).
//
// grant_slots_to_event is admin-only; the HTTP route mounts inside
// an admin-only middleware group, so the underlying handler doesn't
// re-check. MCP bypasses that route gate, so the tool itself
// enforces isAdmin before dispatching.

func (s *Server) registerPurchaseTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_purchases",
		Scope:       scopeRead,
		Description: "List purchases visible to the caller — admins see every row, org users see their own. Each entry includes id, adventure_slug, max_players, amount_cents, provider (free/simulated/stripe/invoice), state (pending/paid/voided), assignment_id when fulfilled, and timestamps. Use to find an unused assignment_id before create_event.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listPurchases)

	s.tools.register(ToolDescriptor{
		Name:        "get_purchase",
		Scope:       scopeRead,
		Description: "Read one purchase by id. Returns the full DTO including state and (when paid) assignment_id.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"purchase_id"},
			"properties": map[string]any{
				"purchase_id": map[string]any{"type": "integer", "description": "Purchase row id."},
			},
		},
	}, getPurchase)

	s.tools.register(ToolDescriptor{
		Name:        "create_purchase",
		Scope:       scopeWrite,
		Description: "Create a pending purchase for a player-slot assignment. Hard-coded to provider=\"simulated\" — the LLM never triggers a real charge. On a Stripe-enabled deployment, the call refuses with a deep-link error pointing at /arrange (use the web checkout for paid buys). The simulated flow yields a checkout URL that confirm_purchase can flip to paid in one shot. Set top_up_assignment_id to grow an existing assignment instead of creating a new one.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"adventure_slug", "max_players"},
			"properties": map[string]any{
				"adventure_slug":       map[string]any{"type": "string", "description": "Adventure to bind the assignment to (ignored when top_up_assignment_id is set; the assignment's slug wins)."},
				"max_players":          map[string]any{"type": "integer", "minimum": 1, "maximum": 500, "description": "Player-slot cap for the new assignment, or the increment when topping up."},
				"top_up_assignment_id": map[string]any{"type": "integer", "description": "Optional: existing assignment id to grow instead of creating a fresh one."},
			},
		},
	}, createPurchase)

	s.tools.register(ToolDescriptor{
		Name:        "confirm_purchase",
		Scope:       scopeWrite,
		Description: "Confirm a pending simulated purchase, transitioning it to paid and minting (or growing) the adventure_assignment in one DB transaction. Returns the assignment_id which create_event then consumes. Idempotent on already-paid rows.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"purchase_id"},
			"properties": map[string]any{
				"purchase_id": map[string]any{"type": "integer", "description": "Purchase to confirm."},
			},
		},
	}, confirmPurchase)

	s.tools.register(ToolDescriptor{
		Name:        "grant_slots_to_event",
		Scope:       scopeWrite,
		Description: "Admin-only: grant additional player slots to an existing event's assignment without going through the org-side purchase flow. Free for non-trusted orgs (forced); for trusted orgs the caller picks the amount (0 = goodwill, >0 = invoice). Refuses when invoked by a non-admin token.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "added_players"},
			"properties": map[string]any{
				"event_id":      map[string]any{"type": "integer", "description": "Event whose assignment cap should grow."},
				"added_players": map[string]any{"type": "integer", "minimum": 1, "maximum": 500, "description": "Slots to add."},
				"amount_cents":  map[string]any{"type": "integer", "minimum": 0, "description": "Cents to invoice the org (only meaningful for trusted orgs; non-trusted always zero)."},
			},
		},
	}, grantSlotsToEvent)
}

/* ---------------- handlers ---------------- */

func listPurchases(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListPurchases,
		"GET", "/api/admin/purchases", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type purchaseIDArgs struct {
	PurchaseID int64 `json:"purchase_id"`
}

func getPurchase(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a purchaseIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.PurchaseID <= 0 {
		return errorResult("purchase_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.PurchaseID, 10)
	status, body, err := callAdmin(ctx, s.Admin.GetPurchase,
		"GET", "/api/admin/purchases/"+id,
		map[string]string{"purchaseID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type createPurchaseArgs struct {
	AdventureSlug     string `json:"adventure_slug"`
	MaxPlayers        int    `json:"max_players"`
	TopUpAssignmentID *int64 `json:"top_up_assignment_id,omitempty"`
}

// stripeConfigured mirrors admin.Handlers.stripeEnabled — both keys
// must be present for Stripe traffic to be considered real. Replicated
// here so MCP can refuse before dispatching, without needing to add
// a new exported method on the admin package.
func stripeConfigured(s *Server) bool {
	if s.Admin == nil {
		return false
	}
	return s.Admin.StripeSecretKey != "" && s.Admin.StripeWebhookSecret != ""
}

func createPurchase(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a createPurchaseArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.AdventureSlug == "" && a.TopUpAssignmentID == nil {
		return errorResult("adventure_slug required (or top_up_assignment_id for a top-up)"), nil
	}
	if a.MaxPlayers <= 0 || a.MaxPlayers > 500 {
		return errorResult("max_players must be 1–500"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	// Q1 gate: refuse on Stripe-enabled deployments. The MCP
	// surface is never the right place to initiate a real charge —
	// the LLM has no way to authenticate the human paying.
	if stripeConfigured(s) {
		return errorResult(`stripe is configured on this deployment; create_purchase via MCP is disabled. Use the web checkout at /arrange/<org>/billing — or, if you really want a fee-free buy, ask an admin to call grant_slots_to_event for you.`), nil
	}
	body := map[string]any{
		"adventure_slug": a.AdventureSlug,
		"max_players":    a.MaxPlayers,
		// Force simulated: even though the handler would default to
		// "simulated" on a non-Stripe deployment, pinning the value
		// here makes the intent obvious and survives a future change
		// to the default-picking logic.
		"provider": "simulated",
	}
	if a.TopUpAssignmentID != nil {
		body["top_up_assignment_id"] = *a.TopUpAssignmentID
	}
	status, respBody, err := callAdmin(ctx, s.Admin.CreatePurchase,
		"POST", "/api/admin/purchases", nil, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}

func confirmPurchase(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a purchaseIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.PurchaseID <= 0 {
		return errorResult("purchase_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.PurchaseID, 10)
	status, body, err := callAdmin(ctx, s.Admin.ConfirmPurchase,
		"POST", "/api/admin/purchases/"+id+"/confirm",
		map[string]string{"purchaseID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type grantSlotsArgs struct {
	EventID      int64 `json:"event_id"`
	AddedPlayers int   `json:"added_players"`
	AmountCents  int   `json:"amount_cents"`
}

func grantSlotsToEvent(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a grantSlotsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if a.AddedPlayers <= 0 || a.AddedPlayers > 500 {
		return errorResult("added_players must be 1–500"), nil
	}
	if a.AmountCents < 0 {
		return errorResult("amount_cents must be ≥ 0"), nil
	}
	// Admin-only: the HTTP route mounts under an admin middleware
	// group, but MCP bypasses that and goes straight to the
	// handler. Re-enforce the gate here so a non-admin token can't
	// silently grant slots through this tool.
	me, ok := auth.FromContext(ctx)
	if !ok {
		return errorResult("no authenticated caller"), nil
	}
	if me.Role != auth.RoleAdmin && !me.IsAdmin {
		return errorResult("forbidden: grant_slots_to_event is admin-only"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	body := map[string]any{
		"added_players": a.AddedPlayers,
		"amount_cents":  a.AmountCents,
	}
	status, respBody, err := callAdmin(ctx, s.Admin.GrantSlotsToEvent,
		"POST", "/api/admin/events/"+id+"/grant-slots",
		map[string]string{"eventID": id}, nil, body)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, respBody), nil
}
