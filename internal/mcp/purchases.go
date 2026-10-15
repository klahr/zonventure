package mcp

import (
	"context"
	"encoding/json"
	"strconv"
)

// Event passes bought before pay per team stay readable: list and
// get. Nothing is bought through MCP any more; teams are paid for as
// they start (internal/billing).

func (s *Server) registerPurchaseTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_purchases",
		Audience:    audienceAny,
		Scope:       scopeRead,
		Description: "List purchases visible to the caller — admins see every row, org users see their own. Each entry includes id, adventure_slug, max_players, amount_cents, provider (free/simulated/stripe/invoice), state (pending/paid/voided), assignment_id when fulfilled, and timestamps. Event passes are from before pay per team: none are sold now.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, listPurchases)

	s.tools.register(ToolDescriptor{
		Name:        "get_purchase",
		Audience:    audienceAny,
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
