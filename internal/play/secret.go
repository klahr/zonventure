package play

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/session"
)

// loadSecrets reads who holds the team's secret roles and whom it has
// accused, for the session's rules (see session/secret.go).
func (s *teamState) loadSecrets(ctx context.Context, q Querier) error {
	if s.G == nil || len(s.G.SecretRoles()) == 0 {
		return nil
	}
	s.Secrets = map[string]int64{}
	if err := scanRows(ctx, q, `SELECT role, COALESCE(slot_id, 0) FROM team_secrets WHERE team_id = $1`, s.teamID,
		func(rows *sql.Rows) error {
			var role string
			var slot int64
			if err := rows.Scan(&role, &slot); err != nil {
				return err
			}
			s.Secrets[role] = slot
			return nil
		}); err != nil {
		return err
	}

	// Whom most of the team named at each accusation: the one slot with
	// the most votes; a tie names no one.
	accuse := map[string]bool{}
	for _, n := range s.G.Nodes {
		for _, b := range n.Blocks() {
			if b.Kind == graph.BlockAccuse {
				accuse[session.PickKey(n.Slug, b.ID)] = true
			}
		}
	}
	type tally struct {
		best int64
		n    int
		ties bool
	}
	tallies := map[string]*tally{}
	if err := scanRows(ctx, q, `
		SELECT node, block_id, choice, COUNT(*) FROM team_block_votes WHERE team_id = $1
		GROUP BY node, block_id, choice`, s.teamID,
		func(rows *sql.Rows) error {
			var node, block, choice string
			var n int
			if err := rows.Scan(&node, &block, &choice, &n); err != nil {
				return err
			}
			key := session.PickKey(node, block)
			slot, err := strconv.ParseInt(choice, 10, 64)
			if !accuse[key] || err != nil {
				return nil
			}
			t := tallies[key]
			switch {
			case t == nil:
				tallies[key] = &tally{best: slot, n: n}
			case n > t.n:
				t.best, t.n, t.ties = slot, n, false
			case n == t.n:
				t.ties = true
			}
			return nil
		}); err != nil {
		return err
	}
	s.Accused = map[string]int64{}
	for key, t := range tallies {
		if !t.ties {
			s.Accused[key] = t.best
		}
	}
	return nil
}

// dealSecrets deals the team's secret roles, once, as it makes its first
// move: each at random to one of the players then playing, if there are
// at least graph.MinSecretPlayers and the event allows secret roles; to
// no one otherwise, for good. Runs under the team's lock, and reports
// whether it dealt anything.
func (e *Engine) dealSecrets(ctx context.Context, tx *sql.Tx, s *teamState) (bool, error) {
	secret := s.G.SecretRoles()
	if len(secret) == 0 || s.Players == nil || len(s.Secrets) == len(secret) {
		return false, nil
	}
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT secret_roles FROM events WHERE id = $1`, s.eventID).Scan(&allowed); err != nil {
		return false, fmt.Errorf("reading event: %w", err)
	}
	for _, role := range secret {
		if _, dealt := s.Secrets[role]; dealt {
			continue
		}
		var slot any
		if allowed && len(s.Players) >= graph.MinSecretPlayers {
			i, err := rand.Int(rand.Reader, big.NewInt(int64(len(s.Players))))
			if err != nil {
				return false, err
			}
			slot = s.Players[i.Int64()]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO team_secrets (team_id, role, slot_id) VALUES ($1, $2, $3)
			ON CONFLICT (team_id, role) DO NOTHING`, s.teamID, role, slot); err != nil {
			return false, err
		}
		if id, ok := slot.(int64); ok {
			s.Secrets[role] = id
		} else {
			s.Secrets[role] = 0
		}
	}
	return true, nil
}

func scanRows(ctx context.Context, q Querier, query string, arg int64, each func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	for rows.Next() {
		if err := each(rows); err != nil {
			rows.Close()
			return err
		}
	}
	return closeRows(rows)
}
