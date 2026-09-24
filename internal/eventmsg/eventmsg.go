// Package eventmsg stores what an event's organisers tell its teams:
// information beside the game — "lunch is at the square at 12" — not a
// part of it. Messages go to every team or to chosen ones, and are never
// in the adventure or the engine's view.
package eventmsg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaxLen is the longest message, in bytes: a few paragraphs.
const MaxLen = 2000

// Message is one message. TeamIDs empty means every team.
type Message struct {
	ID        int64     `json:"id"`
	Text      string    `json:"text"`
	TeamIDs   []int64   `json:"team_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrEmpty    = errors.New("a message needs text")
	ErrTooLong  = fmt.Errorf("a message is at most %d bytes", MaxLen)
	ErrTeam     = errors.New("a team is not in this event")
	ErrNotFound = errors.New("no such message in this event")
)

type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Send stores a message for the event's teams, or with teamIDs only
// those, which must belong to the event.
func Send(ctx context.Context, q Querier, eventID, userID int64, text string, teamIDs []int64) (Message, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return Message{}, ErrEmpty
	case len(text) > MaxLen:
		return Message{}, ErrTooLong
	}
	var targets any
	if len(teamIDs) > 0 {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM teams WHERE event_id = $1 AND id = ANY($2::bigint[])`, eventID, arrayLiteral(teamIDs)).Scan(&n); err != nil {
			return Message{}, err
		}
		if n != len(unique(teamIDs)) {
			return Message{}, ErrTeam
		}
		targets = arrayLiteral(unique(teamIDs))
	}
	var by any
	if userID > 0 {
		by = userID
	}
	m := Message{Text: text, TeamIDs: unique(teamIDs)}
	err := q.QueryRowContext(ctx, `
		INSERT INTO event_messages (event_id, team_ids, text, created_by) VALUES ($1, $2::bigint[], $3, $4)
		RETURNING id, created_at`, eventID, targets, text, by).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// Delete removes one of the event's messages.
func Delete(ctx context.Context, q Querier, eventID, id int64) error {
	res, err := q.ExecContext(ctx, `DELETE FROM event_messages WHERE event_id = $1 AND id = $2`, eventID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// List is every message of the event, newest first.
func List(ctx context.Context, q Querier, eventID int64) ([]Message, error) {
	return list(ctx, q, `WHERE event_id = $1`, eventID)
}

// ForTeam is what one team has been sent, newest first: the messages to
// everyone and those to it.
func ForTeam(ctx context.Context, q Querier, eventID, teamID int64) ([]Message, error) {
	return list(ctx, q, `WHERE event_id = $1 AND (team_ids IS NULL OR $2 = ANY(team_ids))`, eventID, teamID)
}

func list(ctx context.Context, q Querier, where string, args ...any) ([]Message, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, text, COALESCE(array_to_json(team_ids)::text, '[]'), created_at
		FROM event_messages `+where+` ORDER BY created_at DESC, id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var teams string
		if err := rows.Scan(&m.ID, &m.Text, &teams, &m.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(teams), &m.TeamIDs); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// arrayLiteral is a bigint[] in Postgres's text form, which every driver
// passes through as a plain string.
func arrayLiteral(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func unique(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
