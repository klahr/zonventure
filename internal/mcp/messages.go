package mcp

import (
	"context"
	"encoding/json"
	"strconv"
)

func (s *Server) registerMessageTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_event_messages",
		Scope:       scopeRead,
		Description: "List what the organisers have told an event's teams, newest first. team_ids empty means the message went to every team.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose messages to list."},
			},
		},
	}, listEventMessages)

	s.tools.register(ToolDescriptor{
		Name:        "send_event_message",
		Scope:       scopeWrite,
		Description: "Send teams an informational message during an event — 'lunch is served at the square at 12', 'the bridge is closed, go round'. It is not part of the game: it shows in the player app's inbox, not in the adventure. Omit team_ids to reach every team; pass team ids (from get_event) to reach only those.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "text"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event whose teams to message."},
				"text":     map[string]any{"type": "string", "description": "Markdown, at most 2000 bytes."},
				"team_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Only these teams; omit for all."},
			},
		},
	}, sendEventMessage)

	s.tools.register(ToolDescriptor{
		Name:        "delete_event_message",
		Scope:       scopeWrite,
		Description: "Take back a message sent with send_event_message: teams no longer see it.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id", "message_id"},
			"properties": map[string]any{
				"event_id":   map[string]any{"type": "integer", "description": "Event the message was sent in."},
				"message_id": map[string]any{"type": "integer", "description": "Id from list_event_messages or send_event_message."},
			},
		},
	}, deleteEventMessage)
}

func listEventMessages(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a eventIDArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 {
		return errorResult("event_id required and must be positive"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.ListEventMessages, "GET", "/api/admin/events/"+id+"/messages", map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func sendEventMessage(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		EventID int64   `json:"event_id"`
		Text    string  `json:"text"`
		TeamIDs []int64 `json:"team_ids"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 || a.Text == "" {
		return errorResult("event_id and text are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id := strconv.FormatInt(a.EventID, 10)
	status, body, err := callAdmin(ctx, s.Admin.SendEventMessage, "POST", "/api/admin/events/"+id+"/messages", map[string]string{"eventID": id}, nil,
		map[string]any{"text": a.Text, "team_ids": a.TeamIDs})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func deleteEventMessage(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		EventID   int64 `json:"event_id"`
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad args: " + err.Error()), nil
	}
	if a.EventID <= 0 || a.MessageID <= 0 {
		return errorResult("event_id and message_id are required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	id, mid := strconv.FormatInt(a.EventID, 10), strconv.FormatInt(a.MessageID, 10)
	status, body, err := callAdmin(ctx, s.Admin.DeleteEventMessage, "DELETE", "/api/admin/events/"+id+"/messages/"+mid,
		map[string]string{"eventID": id, "messageID": mid}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}
