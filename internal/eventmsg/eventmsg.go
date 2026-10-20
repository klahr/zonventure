// Package eventmsg stores what an event's organisers tell its teams:
// information beside the game — "lunch is at the square at 12" — not a
// part of it. Messages go to every team or to chosen ones, and are never
// in the adventure or the engine's view. A message may say itself in
// other languages too: a team then reads it in the language it plays in
// (In).
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
	ID   int64  `json:"id"`
	Text string `json:"text"`
	// Translations are the text in other languages, by language tag.
	Translations map[string]string `json:"translations,omitempty"`
	TeamIDs      []int64           `json:"team_ids,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

var (
	ErrEmpty    = errors.New("a message needs text")
	ErrTooLong  = fmt.Errorf("a message is at most %d bytes", MaxLen)
	ErrTeam     = errors.New("a team is not in this event")
	ErrLang     = errors.New("a translation needs a language tag")
	ErrNotFound = errors.New("no such message in this event")
)

type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Send stores a message for the event's teams, or with teamIDs only
// those, which must belong to the event; with translations, the text in
// other languages too (an empty one is left out).
func Send(ctx context.Context, q Querier, eventID, userID int64, text string, translations map[string]string, teamIDs []int64) (Message, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return Message{}, ErrEmpty
	case len(text) > MaxLen:
		return Message{}, ErrTooLong
	}
	tr := map[string]string{}
	for lang, s := range translations {
		lang, s = strings.TrimSpace(lang), strings.TrimSpace(s)
		switch {
		case s == "":
			continue
		case lang == "" || len(lang) > 32 || strings.ContainsAny(lang, " \t\n"):
			return Message{}, ErrLang
		case len(s) > MaxLen:
			return Message{}, ErrTooLong
		}
		tr[lang] = s
	}
	doc, err := json.Marshal(tr)
	if err != nil {
		return Message{}, err
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
	if len(tr) > 0 {
		m.Translations = tr
	}
	err = q.QueryRowContext(ctx, `
		INSERT INTO event_messages (event_id, team_ids, text, created_by, translations) VALUES ($1, $2::bigint[], $3, $4, $5)
		RETURNING id, created_at`, eventID, targets, text, by, doc).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// In is msgs as a team playing in lang reads them: each in lang where
// it says itself in it, as written elsewhere, and without the other
// languages.
func In(msgs []Message, lang string) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		if s := m.Translations[lang]; s != "" {
			m.Text = s
		}
		m.Translations = nil
		out[i] = m
	}
	return out
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
		SELECT id, text, COALESCE(array_to_json(team_ids)::text, '[]'), created_at, translations
		FROM event_messages `+where+` ORDER BY created_at DESC, id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var teams string
		var tr []byte
		if err := rows.Scan(&m.ID, &m.Text, &teams, &m.CreatedAt, &tr); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(teams), &m.TeamIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tr, &m.Translations); err != nil {
			return nil, err
		}
		if len(m.Translations) == 0 {
			m.Translations = nil
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
