package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
)

// Phase 5: read-only observation. Each tool wraps the equivalent
// admin GET endpoint. MCP has no streaming surface so the model
// polls — every description recommends a cadence based on what the
// data can actually change at.

func (s *Server) registerObservationTools() {
	s.tools.register(ToolDescriptor{
		Name:        "list_positions",
		Scope:       scopeRead,
		Description: "Latest known location ping per team for an event. One row per team that has ever pinged: team_id, team_name, page_index (their current puzzle), lat, lng, server_ts. Poll every 10–30 seconds during a live event; the data refreshes per ping the players' phones send (every 5–15 seconds typically).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to query."},
			},
		},
	}, listPositions)

	s.tools.register(ToolDescriptor{
		Name:        "list_activity",
		Scope:       scopeRead,
		Description: "Activity feed for an event, newest first. Includes team-progress events (started, answered, hint shown, completed) and arranger actions (renamed, withdrew, unlocked). Optional team_id filter. Poll every 30s when watching a live event; activity is the canonical source of 'what changed' between polls.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to query."},
				"team_id":  map[string]any{"type": "integer", "description": "Optional: scope the feed to one team."},
			},
		},
	}, listActivity)

	s.tools.register(ToolDescriptor{
		Name:        "list_tracks",
		Scope:       scopeRead,
		Description: "Full ping history per team, smoothed into 10s buckets with accuracy-weighted centroids — same data the live audience map renders. Poll sparingly; the payload grows linearly with event duration. Most observation use cases want list_positions instead.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to query."},
			},
		},
	}, listTracks)

	s.tools.register(ToolDescriptor{
		Name:        "get_event_state_summary",
		Scope:       scopeRead,
		Description: "Compact poll-friendly summary of one event's progress. Returns: state, allowed_ops (legal next transitions), teams_count, completed_count, withdrawn_count, leaderboard (top 5 teams by current page index, with completion / withdrawal flags). Use as the heartbeat in a 'wait for this event to finish' loop — much smaller payload than get_event.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"event_id"},
			"properties": map[string]any{
				"event_id": map[string]any{"type": "integer", "description": "Event to summarise."},
			},
		},
	}, getEventStateSummary)
}

func listPositions(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	status, body, err := callAdmin(ctx, s.Admin.ListPositions,
		"GET", "/api/admin/events/"+id+"/positions",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

type listActivityArgs struct {
	EventID int64 `json:"event_id"`
	TeamID  int64 `json:"team_id,omitempty"`
}

func listActivity(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a listActivityArgs
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
	path := "/api/admin/events/" + id + "/activity"
	if a.TeamID > 0 {
		path += "?team_id=" + strconv.FormatInt(a.TeamID, 10)
	}
	status, body, err := callAdmin(ctx, s.Admin.ListActivity,
		"GET", path,
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func listTracks(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	status, body, err := callAdmin(ctx, s.Admin.ListTracks,
		"GET", "/api/admin/events/"+id+"/tracks",
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

// eventDetailForSummary mirrors the subset of admin.eventDTO +
// teamOut fields the summary cares about. Decoding the full
// EventDetail payload through this struct lets unrelated fields
// drift in the admin layer without breaking the summary tool.
type eventDetailForSummary struct {
	State        string   `json:"state"`
	ScheduledAt  string   `json:"scheduled_at"`
	StartedAt    string   `json:"started_at"`
	CompletedAt  string   `json:"completed_at"`
	CancelledAt  string   `json:"cancelled_at"`
	AllowedOps   []string `json:"allowed_ops"`
	TeamsCount   int      `json:"teams_count"`
	Teams        []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		PageIndex   int    `json:"page_index"`
		CompletedAt string `json:"completed_at,omitempty"`
		WithdrawnAt string `json:"withdrawn_at,omitempty"`
	} `json:"teams"`
}

type leaderboardEntry struct {
	TeamID    int64  `json:"team_id"`
	Name      string `json:"name"`
	PageIndex int    `json:"page_index"`
	Completed bool   `json:"completed"`
	Withdrawn bool   `json:"withdrawn"`
}

type eventStateSummary struct {
	State           string             `json:"state"`
	ScheduledAt     string             `json:"scheduled_at,omitempty"`
	StartedAt       string             `json:"started_at,omitempty"`
	CompletedAt     string             `json:"completed_at,omitempty"`
	CancelledAt     string             `json:"cancelled_at,omitempty"`
	AllowedOps      []string           `json:"allowed_ops"`
	TeamsCount      int                `json:"teams_count"`
	CompletedCount  int                `json:"completed_count"`
	WithdrawnCount  int                `json:"withdrawn_count"`
	Leaderboard     []leaderboardEntry `json:"leaderboard"`
}

const summaryLeaderboardSize = 5

func getEventStateSummary(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
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
	status, body, err := callAdmin(ctx, s.Admin.EventDetail,
		"GET", "/api/admin/events/"+id,
		map[string]string{"eventID": id}, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status < 200 || status >= 300 {
		// Hand back the admin error verbatim so the model sees the
		// same status / message it would from get_event.
		return adminCallResult(status, body), nil
	}
	var detail eventDetailForSummary
	if err := json.Unmarshal(body, &detail); err != nil {
		return errorResult("decode admin response: " + err.Error()), nil
	}
	summary := summariseDetail(detail)
	out, err := json.Marshal(summary)
	if err != nil {
		return errorResult("encode summary: " + err.Error()), nil
	}
	return textResult(string(out)), nil
}

// summariseDetail collapses a full EventDetail payload into the
// poll-friendly summary shape. Pure function — kept separate from
// getEventStateSummary so the test can exercise the projection
// without standing up an admin handler.
func summariseDetail(d eventDetailForSummary) eventStateSummary {
	out := eventStateSummary{
		State:       d.State,
		ScheduledAt: d.ScheduledAt,
		StartedAt:   d.StartedAt,
		CompletedAt: d.CompletedAt,
		CancelledAt: d.CancelledAt,
		AllowedOps:  d.AllowedOps,
		TeamsCount:  d.TeamsCount,
	}
	if out.AllowedOps == nil {
		out.AllowedOps = []string{}
	}
	leaderboard := make([]leaderboardEntry, 0, len(d.Teams))
	for _, t := range d.Teams {
		entry := leaderboardEntry{
			TeamID:    t.ID,
			Name:      t.Name,
			PageIndex: t.PageIndex,
			Completed: t.CompletedAt != "",
			Withdrawn: t.WithdrawnAt != "",
		}
		if entry.Completed {
			out.CompletedCount++
		}
		if entry.Withdrawn {
			out.WithdrawnCount++
		}
		leaderboard = append(leaderboard, entry)
	}
	// Sort: completed teams first (rank by completion order would
	// be nicer but completed_at strings sort ISO-correctly so we use
	// that as a tiebreak via page_index for non-completed teams).
	// Withdrawn sink to the bottom regardless. Stable sort so the
	// ordering between equal-progress teams stays consistent across
	// polls — handy for the model when it diffs successive summaries.
	sort.SliceStable(leaderboard, func(i, j int) bool {
		a, b := leaderboard[i], leaderboard[j]
		if a.Withdrawn != b.Withdrawn {
			return !a.Withdrawn
		}
		if a.Completed != b.Completed {
			return a.Completed
		}
		return a.PageIndex > b.PageIndex
	})
	if len(leaderboard) > summaryLeaderboardSize {
		leaderboard = leaderboard[:summaryLeaderboardSize]
	}
	out.Leaderboard = leaderboard
	return out
}
