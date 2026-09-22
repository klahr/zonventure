// Package play runs teams through adventure graphs against Postgres.
//
// internal/graph/walk decides where a team goes; this package makes it
// so. Its whole job is the part the interpreter deliberately leaves out:
// loading a team's state, holding the right locks, persisting the move,
// and applying effects — all in one transaction per player action.
//
// Three guarantees matter, and each has a test:
//
//   - Two teammates submitting at once cannot both advance. Every action
//     locks the team's state row and checks that the node the client
//     acted on is still the current one; a stale action gets ErrStale
//     (HTTP 409) instead of a double move. The cursor write is also a
//     compare-and-swap on the old node, so the guard holds even if the
//     lock were ever removed.
//
//   - A capacity-limited hub branch is never over-filled by a race. The
//     occupancy count is read under a transaction-scoped advisory lock
//     per (event, hub), so two teams arriving at the last free station
//     are serialised and the second sees the first. This is the one race
//     whose failure is physically visible: two teams at a one-team spot.
//
//   - An effect with an outside-world consequence (email, webhook) is
//     queued in the same transaction that moved the team, never sent
//     inline, so a move that rolls back cannot have sent anything.
package play

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graphstore"
)

var (
	// ErrStale means the team has moved since the client last looked:
	// the node it acted on is no longer current. Map to 409.
	ErrStale = errors.New("team has already moved on")
	// ErrNotHere means a zone gate is not satisfied. Map to 403.
	ErrNotHere = errors.New("not inside the required zone")
	// ErrLocked means answering is blocked after a wrong guess. Map to
	// 429 with Retry-After from Result.BlockedUntil.
	ErrLocked = errors.New("answering is temporarily locked")
	// ErrTimeUp means the puzzle's time limit has passed. The client
	// should call Timeout to take the timeout path, if there is one.
	ErrTimeUp = errors.New("time limit has passed")
	// ErrNotAllowed means the action is not available right now: a skip
	// whose thresholds are unmet, a hint not yet due, a timeout that has
	// not happened. Map to 403.
	ErrNotAllowed = errors.New("action not available")
	// ErrFinished means the team has already finished. Map to 409.
	ErrFinished = errors.New("team has finished")
	// ErrNoGraph means the event is not running on the graph engine, or
	// the adventure has no graph. Map to 404.
	ErrNoGraph = errors.New("event has no graph")
	// ErrWrongNode means the action does not fit the node — answering a
	// story, choosing a branch at a puzzle. Map to 400.
	ErrWrongNode = errors.New("action does not apply to this node")
)

// Locator reports where a team is. The default reads location_pings;
// tests substitute a fixed position.
type Locator interface {
	// Position returns the team's last trustworthy position. ok is false
	// when there is none, or when it fails the anti-teleport check.
	Position(ctx context.Context, q Querier, teamID int64) (lat, lng float64, ok bool, err error)
}

// Querier is the subset of *sql.DB and *sql.Tx this package needs.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Engine runs graph adventures.
type Engine struct {
	DB *sql.DB
	// Now is the clock. Injected so tests can move time for hint delays,
	// lockouts and time limits without sleeping.
	Now func() time.Time
	// Locator reports team positions. Defaults to PingLocator.
	Locator Locator

	// graphs caches decoded graph versions. Safe to cache forever:
	// versions are append-only, so a given id never changes.
	graphs sync.Map // int64 -> *graph.Graph
}

// New returns an Engine with the default clock and locator.
func New(db *sql.DB) *Engine {
	return &Engine{DB: db, Now: time.Now, Locator: PingLocator{}}
}

func (e *Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

func (e *Engine) locator() Locator {
	if e.Locator == nil {
		return PingLocator{}
	}
	return e.Locator
}

func (e *Engine) graph(ctx context.Context, q Querier, id int64) (*graph.Graph, error) {
	if g, ok := e.graphs.Load(id); ok {
		return g.(*graph.Graph), nil
	}
	s, err := graphstore.ByID(ctx, q, id)
	if err != nil {
		if errors.Is(err, graphstore.ErrNotFound) {
			return nil, ErrNoGraph
		}
		return nil, err
	}
	e.graphs.Store(id, s.Graph)
	return s.Graph, nil
}

// Result is what a player action produced.
type Result struct {
	// Accepted is true when an answer matched an output.
	Accepted bool `json:"accepted"`
	// Port is the output the team left by, when it moved.
	Port string `json:"port,omitempty"`
	// Points awarded by this action, after any partial-credit multiplier.
	Points float64 `json:"points,omitempty"`
	// Response is the wrong-answer ladder text for a rejected guess.
	Response string `json:"response,omitempty"`
	// BlockedUntil is set when a wrong guess triggered a lockout.
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	// Hint is the text revealed by RevealHint.
	Hint string `json:"hint,omitempty"`

	// View is the team's state after the action.
	View View `json:"view"`
}

// View is what a player needs to render: where the team is, and what
// they can do there.
type View struct {
	Node      string       `json:"node,omitempty"`
	Type      graph.Type   `json:"type,omitempty"`
	Title     string       `json:"title,omitempty"`
	Text      string       `json:"text,omitempty"`
	Zone      *graph.Zone  `json:"zone,omitempty"`
	Hints     []string     `json:"hints,omitempty"`
	HintCount int          `json:"hint_count,omitempty"`
	Attempts  int          `json:"attempts,omitempty"`
	Offer     []OfferEntry `json:"offer,omitempty"`
	CanFinish bool         `json:"can_finish,omitempty"`
	// CanSkip reports whether the skip thresholds are currently met.
	CanSkip        bool           `json:"can_skip,omitempty"`
	BlockedUntil   *time.Time     `json:"blocked_until,omitempty"`
	Deadline       *time.Time     `json:"deadline,omitempty"`
	Score          float64        `json:"score"`
	PenaltyScore   float64        `json:"penalty_score"`
	PenaltySeconds int            `json:"penalty_seconds"`
	Finished       bool           `json:"finished,omitempty"`
	Outcome        graph.Outcome  `json:"outcome,omitempty"`
	Items          map[string]int `json:"items,omitempty"`
	Journal        []string       `json:"journal,omitempty"`
	Theme          string         `json:"theme,omitempty"`
}

// OfferEntry is one hub branch the team may choose.
type OfferEntry struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
	// Locked is set for a branch whose condition does not hold yet. It
	// is shown greyed out, unless the hub hides locked branches.
	Locked bool `json:"locked,omitempty"`
}
