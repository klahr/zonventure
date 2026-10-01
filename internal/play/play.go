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
	"github.com/klahr/zonventure/internal/graph/session"
	"github.com/klahr/zonventure/internal/graphstore"
)

// The refusals a player action can meet. They are the session
// package's, re-exported so callers of the engine need not import it.
var (
	// ErrStale means the team has moved since the client last looked:
	// the node it acted on is no longer current. Map to 409.
	ErrStale = session.ErrStale
	// ErrNotHere means a zone gate is not satisfied. Map to 403.
	ErrNotHere = session.ErrNotHere
	// ErrLocked means answering is blocked after a wrong guess. Map to
	// 429 with Retry-After from Result.BlockedUntil.
	ErrLocked = session.ErrLocked
	// ErrTimeUp means the puzzle's time limit has passed. The client
	// should call Timeout to take the timeout path, if there is one.
	ErrTimeUp = session.ErrTimeUp
	// ErrNotAllowed means the action is not available right now. Map to
	// 403.
	ErrNotAllowed = session.ErrNotAllowed
	// ErrFinished means the team has already finished. Map to 409.
	ErrFinished = session.ErrFinished
	// ErrWrongNode means the action does not fit the node. Map to 400.
	ErrWrongNode = session.ErrWrongNode
	// ErrNoGraph means the event is not running on the graph engine, or
	// the adventure has no graph. Map to 404.
	ErrNoGraph = errors.New("event has no graph")
)

// Locator reports where a team is. The default reads location_pings;
// tests substitute a fixed position.
type Locator interface {
	// Position returns the team's last trustworthy position. ok is false
	// when there is none, or when it fails the anti-teleport check.
	Position(ctx context.Context, q Querier, teamID int64) (lat, lng float64, ok bool, err error)
}

// PhoneLocator is a Locator that also knows where each of a team's
// phones is, once the team splits up. A Locator that is not one places
// every player at the team's position.
type PhoneLocator interface {
	// Phones returns where each phone that has reported lately is, in
	// slot order; empty, not nil, when none has.
	Phones(ctx context.Context, q Querier, teamID int64, now time.Time) ([]Phone, error)
}

// Phone is where one player's phone is.
type Phone struct {
	Slot int64
	session.Position
}

type phoneKey struct{}

// ForPhone marks a request as one phone's: the views and actions run
// under it are built for that player, which is what a block shown only
// "here" asks about. Without it they are the team's as a whole.
func ForPhone(ctx context.Context, slotID int64) context.Context {
	return context.WithValue(ctx, phoneKey{}, slotID)
}

func phoneOf(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(phoneKey{}).(int64)
	return id, ok
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

// Result, View and OfferEntry are the session package's: the live
// engine, the designer's preview and the simulator all return exactly
// the same shapes, which is what lets one component render all three.
type (
	Result     = session.Result
	View       = session.View
	OfferEntry = session.OfferEntry
)
