// Package results keeps each event's tamper-evident record of what
// decides its results (migration 0127): a hash chain of every answer,
// hint, step, score change, organiser action on a team, team finish and
// the event's end, with the server's time and the team's standing after
// each. Entries are appended in the transaction that makes the change,
// under a per-event advisory lock, so the record and the results can't
// disagree and two appends can't claim the same predecessor.
//
// A finished team's receipt is the hash of its finish entry: it covers
// everything recorded before it. The event's fingerprint is the hash of
// its "event.completed" entry. Verify recomputes the chain and compares
// each team's standing now with the last one recorded.
//
// Hashes are SHA-256, hex, over the JSON of chainEntry: Go marshals
// struct fields in order and map keys sorted, so the same values always
// give the same bytes. The first entry's prev_hash is "".
//
// An author's dev event (dev = TRUE) is a playground and keeps no
// record.
package results

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"
)

// Entry kinds besides an engine action's "play.<action>".
const (
	TeamFinished   = "team.finished"
	TeamWithdrawn  = "team.withdrawn"
	TeamReturned   = "team.returned"
	TeamUnlocked   = "team.unlocked"
	TeamCompleted  = "team.completed_by_organiser"
	StartAnywhere  = "team.start_anywhere"
	ScoreAdjusted  = "team.score_adjusted"
	EventCompleted = "event.completed"
	EventCancelled = "event.cancelled"
)

// Querier is what Append writes through: an open transaction.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Standing is a team's result as the leaderboard reads it.
type Standing struct {
	Points         float64 `json:"points"`
	PenaltyScore   float64 `json:"penalty_score"`
	PenaltySeconds float64 `json:"penalty_seconds"`
	CompletedAt    string  `json:"completed_at"`
	Outcome        string  `json:"outcome"`
	Withdrawn      bool    `json:"withdrawn"`
}

// chainEntry is what an entry's hash covers.
type chainEntry struct {
	PrevHash   string `json:"prev_hash"`
	EventID    int64  `json:"event_id"`
	Seq        int64  `json:"seq"`
	TeamID     int64  `json:"team_id"`
	TeamName   string `json:"team_name"`
	Kind       string `json:"kind"`
	Payload    string `json:"payload"`
	ServerTime string `json:"server_time"`
}

func (e chainEntry) hash() string {
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// lockKey is the event's advisory lock for appends, keyed as the play
// engine keys its locks.
func lockKey(eventID int64) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "zv-results:%d", eventID)
	return int64(h.Sum64())
}

// Append records one entry for eventID, teamID 0 for the event as a
// whole, in q: a transaction, so it commits with the change it records,
// or a *sql.DB, when it opens one of its own. For
// a team it adds the team's standing after the change, read in the same
// transaction. It returns the entry's hash, "" when the event keeps no
// record (a dev event).
func Append(ctx context.Context, q Querier, eventID, teamID int64, kind string, fields map[string]any) (string, error) {
	if db, ok := q.(*sql.DB); ok {
		return AppendDB(ctx, db, eventID, teamID, kind, fields)
	}
	var dev bool
	if err := q.QueryRowContext(ctx, `SELECT dev FROM events WHERE id = $1`, eventID).Scan(&dev); err != nil {
		return "", fmt.Errorf("results: event %d: %w", eventID, err)
	}
	if dev {
		return "", nil
	}
	if _, err := q.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey(eventID)); err != nil {
		return "", err
	}
	payload := map[string]any{}
	for k, v := range fields {
		payload[k] = v
	}
	e := chainEntry{EventID: eventID, TeamID: teamID, Kind: kind,
		ServerTime: time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)}
	if teamID != 0 {
		st, err := StandingOf(ctx, q, teamID)
		if err != nil {
			return "", err
		}
		payload["standing"] = st
		_ = q.QueryRowContext(ctx, `SELECT name FROM teams WHERE id = $1`, teamID).Scan(&e.TeamName)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	e.Payload = string(raw)
	err = q.QueryRowContext(ctx, `
		SELECT seq, entry_hash FROM event_result_log WHERE event_id = $1 ORDER BY seq DESC LIMIT 1`, eventID).Scan(&e.Seq, &e.PrevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	e.Seq++
	h := e.hash()
	_, err = q.ExecContext(ctx, `
		INSERT INTO event_result_log (event_id, seq, team_id, team_name, kind, payload, server_time, prev_hash, entry_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		eventID, e.Seq, sql.NullInt64{Int64: teamID, Valid: teamID != 0}, e.TeamName, kind, e.Payload, e.ServerTime, e.PrevHash, h)
	return h, err
}

// AppendTeam is Append for a team, finding its event.
func AppendTeam(ctx context.Context, q Querier, teamID int64, kind string, fields map[string]any) (string, error) {
	var eventID int64
	if err := q.QueryRowContext(ctx, `SELECT event_id FROM teams WHERE id = $1`, teamID).Scan(&eventID); err != nil {
		return "", fmt.Errorf("results: team %d: %w", teamID, err)
	}
	return Append(ctx, q, eventID, teamID, kind, fields)
}

// AppendDB is Append in a transaction of its own, for a change already
// made outside one.
func AppendDB(ctx context.Context, db *sql.DB, eventID, teamID int64, kind string, fields map[string]any) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	h, err := Append(ctx, tx, eventID, teamID, kind, fields)
	if err != nil {
		return "", err
	}
	return h, tx.Commit()
}

// StandingOf reads a team's standing as the leaderboard does.
func StandingOf(ctx context.Context, q Querier, teamID int64) (Standing, error) {
	var (
		st        Standing
		completed sql.NullTime
		outcome   sql.NullString
		withdrawn sql.NullString
	)
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(s.score, 0), COALESCE(s.penalty_score, 0), COALESCE(s.penalty_seconds, 0),
		       s.completed_at, s.outcome, t.withdrawn_at
		FROM teams t LEFT JOIN team_graph_state s ON s.team_id = t.id
		WHERE t.id = $1`, teamID).Scan(&st.Points, &st.PenaltyScore, &st.PenaltySeconds, &completed, &outcome, &withdrawn)
	if err != nil {
		return st, fmt.Errorf("results: team %d: %w", teamID, err)
	}
	if completed.Valid {
		st.CompletedAt = completed.Time.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	}
	st.Outcome = outcome.String
	st.Withdrawn = withdrawn.Valid && withdrawn.String != ""
	return st, nil
}

// Problem is where an event's record doesn't add up.
type Problem struct {
	Seq     int64  `json:"seq,omitempty"`
	TeamID  int64  `json:"team_id,omitempty"`
	Problem string `json:"problem"`
}

// TeamCheck is one team's receipt and standing, recorded against now.
type TeamCheck struct {
	TeamID   int64     `json:"team_id"`
	Name     string    `json:"name"`
	Receipt  string    `json:"receipt,omitempty"`
	Recorded *Standing `json:"recorded,omitempty"`
	Current  Standing  `json:"current"`
	Matches  bool      `json:"matches"`
}

// Report is an event's record, recomputed.
type Report struct {
	OK      bool  `json:"ok"`
	Entries int64 `json:"entries"`
	// Head is the latest entry's hash; Fingerprint the event's, once it
	// is over (Head when it hasn't ended).
	Head        string      `json:"head"`
	Fingerprint string      `json:"fingerprint,omitempty"`
	Sealed      bool        `json:"sealed"`
	Problems    []Problem   `json:"problems"`
	Teams       []TeamCheck `json:"teams"`
}

// Verify recomputes an event's chain, and compares each team's standing
// now with the last one recorded: an edit to the results tables that
// skipped the record shows as a team that doesn't match.
func Verify(ctx context.Context, q interface {
	Querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, eventID int64) (Report, error) {
	rep := Report{Problems: []Problem{}, Teams: []TeamCheck{}}
	rows, err := q.QueryContext(ctx, `
		SELECT seq, COALESCE(team_id, 0), team_name, kind, payload, server_time, prev_hash, entry_hash
		FROM event_result_log WHERE event_id = $1 ORDER BY seq`, eventID)
	if err != nil {
		return rep, err
	}
	type last struct {
		standing *Standing
		receipt  string
		name     string
	}
	teams := map[int64]*last{}
	prev, want := "", int64(1)
	for rows.Next() {
		var (
			e      chainEntry
			stored string
			at     time.Time
		)
		if err := rows.Scan(&e.Seq, &e.TeamID, &e.TeamName, &e.Kind, &e.Payload, &at, &e.PrevHash, &stored); err != nil {
			rows.Close()
			return rep, err
		}
		e.EventID = eventID
		e.ServerTime = at.UTC().Format(time.RFC3339Nano)
		rep.Entries++
		if e.Seq != want || e.PrevHash != prev {
			rep.Problems = append(rep.Problems, Problem{Seq: e.Seq, Problem: "does not follow the entry before it: one was changed, removed or inserted"})
		}
		if e.hash() != stored {
			rep.Problems = append(rep.Problems, Problem{Seq: e.Seq, TeamID: e.TeamID, Problem: "it was changed after it was recorded"})
		}
		if e.TeamID != 0 {
			t := teams[e.TeamID]
			if t == nil {
				t = &last{}
				teams[e.TeamID] = t
			}
			t.name = e.TeamName
			var p struct {
				Standing *Standing `json:"standing"`
			}
			if json.Unmarshal([]byte(e.Payload), &p) == nil && p.Standing != nil {
				t.standing = p.Standing
			}
			if e.Kind == TeamFinished {
				t.receipt = stored
			}
		}
		if e.Kind == EventCompleted || e.Kind == EventCancelled {
			rep.Sealed, rep.Fingerprint = true, stored
		}
		prev, want, rep.Head = stored, e.Seq+1, stored
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}
	if !rep.Sealed {
		rep.Fingerprint = rep.Head
	}

	// Every team, recorded or not: one with a standing but no record
	// got it without the game.
	trows, err := q.QueryContext(ctx, `SELECT id, name FROM teams WHERE event_id = $1 ORDER BY id`, eventID)
	if err != nil {
		return rep, err
	}
	type team struct {
		id   int64
		name string
	}
	var all []team
	for trows.Next() {
		var t team
		if err := trows.Scan(&t.id, &t.name); err != nil {
			trows.Close()
			return rep, err
		}
		all = append(all, t)
	}
	trows.Close()
	for _, t := range all {
		cur, err := StandingOf(ctx, q, t.id)
		if err != nil {
			return rep, err
		}
		c := TeamCheck{TeamID: t.id, Name: t.name, Current: cur}
		if l := teams[t.id]; l != nil {
			c.Receipt, c.Recorded = l.receipt, l.standing
		}
		blank := Standing{Withdrawn: cur.Withdrawn}
		switch {
		case c.Recorded != nil:
			c.Matches = *c.Recorded == cur
		default:
			// Never recorded: fine while it has no result of its own.
			c.Matches = cur == blank
		}
		if !c.Matches {
			rep.Problems = append(rep.Problems, Problem{TeamID: t.id, Problem: "its result now differs from the one recorded"})
		}
		rep.Teams = append(rep.Teams, c)
	}
	rep.OK = len(rep.Problems) == 0
	return rep, nil
}

// Receipt is a team's receipt as its own page shows it, and whether the
// record still bears it out: the chain up to the team's finish
// recomputes to it.
type Receipt struct {
	Receipt     string `json:"receipt"`
	FinishedAt  string `json:"finished_at"`
	Entries     int64  `json:"entries"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Matches     bool   `json:"matches"`
}

// ReceiptOf returns a team's receipt; sql.ErrNoRows before it finished.
func ReceiptOf(ctx context.Context, q interface {
	Querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, teamID int64) (Receipt, error) {
	var (
		r       Receipt
		eventID int64
		seq     int64
		at      time.Time
	)
	err := q.QueryRowContext(ctx, `
		SELECT event_id, seq, entry_hash, server_time FROM event_result_log
		WHERE team_id = $1 AND kind = $2 ORDER BY seq DESC LIMIT 1`, teamID, TeamFinished).Scan(&eventID, &seq, &r.Receipt, &at)
	if err != nil {
		return r, err
	}
	r.FinishedAt = at.UTC().Format(time.RFC3339)
	rep, err := Verify(ctx, q, eventID)
	if err != nil {
		return r, err
	}
	r.Entries, r.Fingerprint = seq, rep.Fingerprint
	// The receipt holds when no entry up to it was touched and it is
	// still the team's finish entry.
	r.Matches = true
	for _, p := range rep.Problems {
		if p.Seq != 0 && p.Seq <= seq {
			r.Matches = false
		}
	}
	for _, t := range rep.Teams {
		if t.TeamID == teamID && t.Receipt != r.Receipt {
			r.Matches = false
		}
	}
	return r, nil
}
