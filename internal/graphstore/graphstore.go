// Package graphstore persists authored adventure graphs.
//
// Graphs are append-only versions. Saving never overwrites: it writes
// version N+1. An event pins the version it started with, so an author
// editing during a live event cannot move the ground under a team that
// is already playing — the edit takes effect for the next event.
//
// Every save is validated first and refused if the graph has errors.
// That is the point of validating statically: a broken graph never
// reaches storage, so it can never reach a player.
package graphstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// Querier is the subset of *sql.DB and *sql.Tx this package needs, so a
// caller can save inside its own transaction.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrNotFound means the adventure has no saved graph yet.
var ErrNotFound = errors.New("adventure has no graph")

// InvalidError carries the validation report for a refused save, so the
// HTTP layer can hand the author every problem at once.
type InvalidError struct {
	Report valid.Report
}

func (e *InvalidError) Error() string {
	errs := e.Report.Errors()
	if len(errs) == 0 {
		return "graph is invalid"
	}
	return fmt.Sprintf("graph is invalid: %s (and %d more)", errs[0], len(errs)-1)
}

// Stored is one saved version.
type Stored struct {
	ID          int64
	AdventureID int64
	Version     int
	Graph       *graph.Graph
}

// Save validates g and, if it has no errors, writes it as the next
// version for the adventure. Warnings do not block: an author mid-edit
// should be able to save a node they have not wired up yet.
//
// The next version number is computed inside the INSERT so that two
// concurrent saves cannot both claim the same number; the unique
// constraint turns the loser into an error rather than a silent
// overwrite.
func Save(ctx context.Context, q Querier, adventureID, userID int64, g *graph.Graph) (Stored, valid.Report, error) {
	report := valid.Check(g)
	if report.HasErrors() {
		return Stored{}, report, &InvalidError{Report: report}
	}

	doc, err := json.Marshal(g)
	if err != nil {
		return Stored{}, report, fmt.Errorf("encoding graph: %w", err)
	}

	var user any
	if userID > 0 {
		user = userID
	}

	s := Stored{AdventureID: adventureID, Graph: g}
	err = q.QueryRowContext(ctx, `
		INSERT INTO adventure_graphs (adventure_id, version, graph, created_by_user_id)
		VALUES ($1,
		        COALESCE((SELECT MAX(version) FROM adventure_graphs WHERE adventure_id = $1), 0) + 1,
		        $2, $3)
		RETURNING id, version
	`, adventureID, doc, user).Scan(&s.ID, &s.Version)
	if err != nil {
		return Stored{}, report, fmt.Errorf("saving graph: %w", err)
	}
	return s, report, nil
}

// Latest returns the newest saved version.
func Latest(ctx context.Context, q Querier, adventureID int64) (Stored, error) {
	return scan(q.QueryRowContext(ctx, `
		SELECT id, adventure_id, version, graph FROM adventure_graphs
		WHERE adventure_id = $1
		ORDER BY version DESC
		LIMIT 1
	`, adventureID))
}

// ByID returns one specific version, as pinned by an event.
func ByID(ctx context.Context, q Querier, id int64) (Stored, error) {
	return scan(q.QueryRowContext(ctx, `
		SELECT id, adventure_id, version, graph FROM adventure_graphs WHERE id = $1
	`, id))
}

func scan(row *sql.Row) (Stored, error) {
	var (
		s   Stored
		doc []byte
	)
	if err := row.Scan(&s.ID, &s.AdventureID, &s.Version, &doc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Stored{}, ErrNotFound
		}
		return Stored{}, err
	}
	var g graph.Graph
	if err := json.Unmarshal(doc, &g); err != nil {
		return Stored{}, fmt.Errorf("decoding stored graph %d: %w", s.ID, err)
	}
	s.Graph = &g
	return s, nil
}
