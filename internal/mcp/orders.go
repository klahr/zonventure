package mcp

// Orders to post and how an event delivers its print materials: what the
// console's Box redemptions page and an event's Print materials card do.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

func (s *Server) registerOrderTools() {
	s.tools.register(ToolDescriptor{
		Name:  "set_print_delivery",
		Scope: scopeWrite,
		Description: "Change how one event delivers its adventure's print materials, without a new adventure version: " +
			`{"event_id": 12, "deliveries": {"letter": "emailed"}} emails the letter for the buyer to print, as for summer; "posted" has the organiser print and post it; "organiser" keeps it for organisers; "" goes back to the adventure's own. ` +
			"Material ids are the adventure's print_materials ids; get_event lists them as print_materials with adventure_delivery and delivery. Returns the event's materials as they now stand.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "deliveries"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to change."},
				"deliveries": map[string]any{
					"type":                 "object",
					"description":          "Material id → posted, emailed, organiser, or \"\" for the adventure's own. Materials left out keep theirs.",
					"additionalProperties": map[string]any{"type": "string", "enum": []string{"posted", "emailed", "organiser", ""}},
				},
			},
		},
	}, setPrintDelivery)

	s.tools.register(ToolDescriptor{
		Name:  "list_orders",
		Scope: scopeRead,
		Description: "The organisation's gift box redemptions, as on the console's Box redemptions page. state to_post (the default) lists those still to post, the earliest post date first; " +
			"pending, approved, rejected, cancelled or all lists by state. Each has the buyer's name, email and postal address, letter_lang (the language their envelope is printed in), post_at, posted_at, " +
			"and print: the materials the organiser prints for it, in that language. lang narrows the list to one envelope language. counts has each state's number and to_post's.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"state": map[string]any{"type": "string", "enum": []string{"to_post", "pending", "approved", "rejected", "cancelled", "all"}},
				"lang":  map[string]any{"type": "string", "description": "Only orders whose envelope is in this language, e.g. sv."},
			},
		},
	}, listOrders)

	s.tools.register(ToolDescriptor{
		Name:        "mark_order_posted",
		Scope:       scopeWrite,
		Description: "Mark a gift box redemption's envelope as posted, once the organiser has printed and sent it, or (posted: false) as not posted after all. It then leaves list_orders' to_post.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"redemption_id"},
			"properties": map[string]any{
				"redemption_id": map[string]any{"type": "integer", "description": "The id from list_orders."},
				"posted":        map[string]any{"type": "boolean", "description": "Default true."},
			},
		},
	}, markOrderPosted)
}

func setPrintDelivery(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		EventID    int64             `json:"event_id"`
		Deliveries map[string]string `json:"deliveries"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 || len(a.Deliveries) == 0 {
		return errorResult("event_id and at least one delivery are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.SetEventPrintDelivery, "PUT", "/api/admin/events/"+id+"/print-delivery",
		map[string]string{"eventID": id}, nil, map[string]any{"deliveries": a.Deliveries})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func listOrders(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		State string `json:"state"`
		Lang  string `json:"lang"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return errorResult("bad args: " + err.Error()), nil
		}
	}
	if a.State == "" {
		a.State = "to_post"
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.ListRedemptions, "GET", "/api/admin/redemptions?state="+url.QueryEscape(a.State), nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != 200 || a.Lang == "" {
		return adminCallResult(status, body), nil
	}
	var list struct {
		Redemptions []json.RawMessage `json:"redemptions"`
		Counts      map[string]int    `json:"counts"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return errorResult("decoding orders: " + err.Error()), nil
	}
	kept := []json.RawMessage{}
	for _, r := range list.Redemptions {
		var o struct {
			LetterLang string `json:"letter_lang"`
		}
		if json.Unmarshal(r, &o) == nil && o.LetterLang == a.Lang {
			kept = append(kept, r)
		}
	}
	list.Redemptions = kept
	out, _ := json.Marshal(list)
	return textResult(string(out)), nil
}

func markOrderPosted(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		RedemptionID int64 `json:"redemption_id"`
		Posted       *bool `json:"posted"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.RedemptionID <= 0 {
		return errorResult("redemption_id required and must be positive"), nil
	}
	posted := a.Posted == nil || *a.Posted
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.RedemptionID, 10)
	status, body, err := callAdmin(ctx, s.Admin.UpdateRedemption, "PATCH", "/api/admin/redemptions/"+id,
		map[string]string{"redemptionID": id}, nil, map[string]any{"posted": posted})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
