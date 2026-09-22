// Package lanes wires the lane progression system: assigning per-team
// permutations on run start, resolving the next page a team should
// see, and detecting lane / event completion.
//
// The package is the bridge between the static adventure model (lanes
// and pages) loaded by package adventure and the runtime team state
// (current_page_id, current_lane_id, team_lane_orderings) stored in
// Postgres. Player and admin handlers share these helpers so the
// answer-submit, manual-advance, and auto-promote code paths agree
// on what "next page" means.
package lanes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klahr/zonventure/internal/adventure"
)

// Execer is the read+write subset of *sql.DB / *sql.Tx that the lane
// helpers need. Callers pass a transaction when they want lane writes
// to commit atomically with the rest of their work (the typical
// applyAnswer pattern).
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Initialize assigns per-team permutations and initial cursors for
// every team in the event. Idempotent at the row level: orderings
// already in place are left alone (ON CONFLICT DO NOTHING) so a
// re-run after a partial failure or a "back to scheduled" admin
// action doesn't clobber a team mid-game.
//
// Call this exactly when an event transitions scheduled → running
// for the first time. Pause → resume must NOT call it; the existing
// orderings and cursors are still valid for the resumed run.
func Initialize(ctx context.Context, db Execer, lib *adventure.Library, eventID int64) error {
	var advSlug string
	if err := db.QueryRowContext(ctx,
		`SELECT adventure_slug FROM events WHERE id = $1`, eventID).Scan(&advSlug); err != nil {
		return fmt.Errorf("load event: %w", err)
	}
	adv, err := lib.GetContext(ctx, advSlug)
	if err != nil {
		return fmt.Errorf("load adventure: %w", err)
	}
	if adv == nil {
		return fmt.Errorf("adventure %q missing", advSlug)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM teams WHERE event_id = $1 ORDER BY id`, eventID)
	if err != nil {
		return fmt.Errorf("load teams: %w", err)
	}
	var teamIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan team: %w", err)
		}
		teamIDs = append(teamIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for slot, teamID := range teamIDs {
		if err := initializeTeam(ctx, db, adv, teamID, slot); err != nil {
			return fmt.Errorf("init team %d: %w", teamID, err)
		}
	}
	return nil
}

func initializeTeam(ctx context.Context, db Execer, adv *adventure.Adventure, teamID int64, slot int) error {
	for _, lane := range adv.Lanes {
		canonical := adventure.CanonicalLanePageIDs(adv, lane.ID)
		if len(canonical) == 0 {
			continue
		}
		// Synthesized lanes have no real DB row to point at, so we
		// skip the persist step. The runtime fallback in
		// PermutationFor regenerates the same cyclic shift on demand,
		// so progression still works.
		if adventure.IsSynthesizedLane(lane.ID) {
			continue
		}
		perm := adventure.LanePermutation(lane.Mode, canonical, slot)
		js, err := json.Marshal(perm)
		if err != nil {
			return fmt.Errorf("marshal permutation: %w", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO team_lane_orderings (team_id, lane_id, page_ids)
			VALUES ($1, $2, $3::jsonb)
			ON CONFLICT (team_id, lane_id) DO NOTHING
		`, teamID, lane.ID, string(js)); err != nil {
			return fmt.Errorf("insert ordering: %w", err)
		}
	}
	// Plant the initial cursor on the first lane's first page in this
	// team's permutation. Only do so when the team hasn't already
	// advanced — preserves any in-flight cursor a backfill or a prior
	// partial Initialize set up.
	first := adv.FirstLane()
	if first == nil {
		return nil
	}
	perm, err := PermutationFor(ctx, db, adv, teamID, first.ID)
	if err != nil {
		return err
	}
	if len(perm) == 0 {
		return nil
	}
	var laneArg any
	if adventure.IsSynthesizedLane(first.ID) {
		laneArg = sql.NullInt64{}
	} else {
		laneArg = first.ID
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE teams
		SET current_lane_id = $1,
		    current_page_id = $2,
		    current_page_started_at = COALESCE(current_page_started_at, now_text())
		WHERE id = $3
		  AND current_page_id IS NULL
		  AND completed_at IS NULL
	`, laneArg, perm[0], teamID); err != nil {
		return fmt.Errorf("plant cursor: %w", err)
	}
	return nil
}

// PermutationFor returns the team's stored permutation of pages for
// the given lane. Falls back to a freshly computed permutation when
// no row exists in team_lane_orderings — keeps the system robust to
// migration backfills, lanes added after run start, and any path
// where Initialize hasn't run.
//
// Synthesized lanes (negative id, used for in-memory implicit lanes
// the loader builds for pre-lanes adventures) skip the DB read and
// always derive the permutation on the fly — the row could never
// exist anyway because the lane has no real BIGSERIAL id.
func PermutationFor(ctx context.Context, db Execer, adv *adventure.Adventure, teamID, laneID int64) ([]int64, error) {
	if !adventure.IsSynthesizedLane(laneID) {
		var raw []byte
		err := db.QueryRowContext(ctx, `
			SELECT page_ids FROM team_lane_orderings
			WHERE team_id = $1 AND lane_id = $2
		`, teamID, laneID).Scan(&raw)
		if err == nil {
			var ids []int64
			if err := json.Unmarshal(raw, &ids); err != nil {
				return nil, fmt.Errorf("unmarshal ordering: %w", err)
			}
			return ids, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	// Fallback: derive the permutation from the team's slot. This
	// keeps progression working when the stored row is missing — the
	// derivation is deterministic, so a later Initialize that fills
	// the row in produces the same answer.
	slot, err := teamSlot(ctx, db, teamID)
	if err != nil {
		return nil, err
	}
	lane := adv.LaneByID(laneID)
	if lane == nil {
		return nil, nil
	}
	canonical := adventure.CanonicalLanePageIDs(adv, laneID)
	return adventure.LanePermutation(lane.Mode, canonical, slot), nil
}

// teamSlot returns the team's 0-based position when ordered by id
// within its event — the deterministic key the cyclic-shift
// permutation uses.
func teamSlot(ctx context.Context, db Execer, teamID int64) (int, error) {
	var slot int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM teams
		WHERE event_id = (SELECT event_id FROM teams WHERE id = $1)
		  AND id < $1
	`, teamID).Scan(&slot)
	return slot, err
}

// FirstPageFor returns the first page a team should see when their
// run starts — the first enabled page in their permutation of the
// first lane that has any. Returns 0 when the adventure has nothing
// to show, which the caller surfaces as a "no current page" error.
//
// The returned lane id is always a real DB id or 0 — synthesized
// in-memory lanes collapse to 0 so callers can pass the value
// straight to a teams.current_lane_id UPDATE without tripping the
// FK constraint.
func FirstPageFor(ctx context.Context, db Execer, adv *adventure.Adventure, teamID int64) (int64, int64, error) {
	for _, lane := range adv.Lanes {
		perm, err := PermutationFor(ctx, db, adv, teamID, lane.ID)
		if err != nil {
			return 0, 0, err
		}
		if pid := firstEnabled(adv, perm); pid != 0 {
			return externalLaneID(lane.ID), pid, nil
		}
	}
	return 0, 0, nil
}

// externalLaneID strips the loader's synthesized-lane sentinel from
// values that flow back to callers, so writes to teams.current_lane_id
// (a real FK column) never see a non-existent id. Real lanes pass
// through unchanged.
func externalLaneID(laneID int64) int64 {
	if adventure.IsSynthesizedLane(laneID) {
		return 0
	}
	return laneID
}

// NextPageAfter resolves the page a team should see *after* solving
// currentPageID. Returns:
//
//   - (laneID, pageID, nil) when there is a next page within the
//     same or a later lane.
//   - (0, 0, nil) when the team has just solved the last page in
//     the last lane and should be marked completed. The caller
//     stamps completed_at.
//
// The function walks the team's permutation for the current lane
// past currentPageID, skips disabled pages, and rolls into the next
// lane on exhaustion. When the last lane is also exhausted the
// team is done.
func NextPageAfter(ctx context.Context, db Execer, adv *adventure.Adventure, teamID, currentPageID int64) (int64, int64, error) {
	curPage := adv.PageByID(currentPageID)
	var startLane *adventure.Lane
	if curPage != nil && curPage.LaneID != 0 {
		startLane = adv.LaneByID(curPage.LaneID)
	}
	// Walk forward within the current lane's permutation.
	if startLane != nil {
		perm, err := PermutationFor(ctx, db, adv, teamID, startLane.ID)
		if err != nil {
			return 0, 0, err
		}
		pos := indexOf(perm, currentPageID)
		if pos >= 0 {
			if pid := firstEnabledFrom(adv, perm, pos+1); pid != 0 {
				return externalLaneID(startLane.ID), pid, nil
			}
		}
	}
	// Lane exhausted — try each subsequent lane in idx order.
	for _, lane := range adv.Lanes {
		if startLane != nil && lane.Index <= startLane.Index {
			continue
		}
		perm, err := PermutationFor(ctx, db, adv, teamID, lane.ID)
		if err != nil {
			return 0, 0, err
		}
		if pid := firstEnabled(adv, perm); pid != 0 {
			return externalLaneID(lane.ID), pid, nil
		}
	}
	return 0, 0, nil
}

func firstEnabled(adv *adventure.Adventure, ids []int64) int64 {
	return firstEnabledFrom(adv, ids, 0)
}

func firstEnabledFrom(adv *adventure.Adventure, ids []int64, from int) int64 {
	for i := from; i < len(ids); i++ {
		p := adv.PageByID(ids[i])
		if p == nil || p.Disabled {
			continue
		}
		return ids[i]
	}
	return 0
}

func indexOf(ids []int64, target int64) int {
	for i, id := range ids {
		if id == target {
			return i
		}
	}
	return -1
}
