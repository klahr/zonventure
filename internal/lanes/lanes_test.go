package lanes_test

import (
	"context"
	"database/sql"
	"testing"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/dbtest"
	"git.sr.ht/~klahr/zv/internal/lanes"
)

// TestLanes_ShuffledProgression seeds a 4-page shuffled lane, makes
// 4 teams, and walks each team through their permutation. Verifies
// the cyclic-shift Latin-square spread (each team starts on a
// different page) and that the run ends after the lane is exhausted.
//
// Skipped when no Postgres is reachable — the lanes package is purely
// SQL-driven, and faking the shape with sqlmock would just re-encode
// the same queries with no fidelity gain.
func TestLanes_ShuffledProgression(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	lib := adventure.New(db)

	advID := seedAdventure(t, db, "shuf", "Shuffled demo")
	laneID := seedLane(t, db, advID, 0, "shuffled")
	pages := []int64{
		seedPage(t, db, advID, 0, "P0", `[{"text":"a"}]`, laneID),
		seedPage(t, db, advID, 1, "P1", `[{"text":"b"}]`, laneID),
		seedPage(t, db, advID, 2, "P2", `[{"text":"c"}]`, laneID),
		seedPage(t, db, advID, 3, "P3", `[{"text":"d"}]`, laneID),
	}

	eventID := seedEvent(t, db, "shuf")
	teamIDs := make([]int64, 4)
	for i := range teamIDs {
		teamIDs[i] = seedTeam(t, db, eventID, byte('a'+i))
	}

	if err := lanes.Initialize(ctx, db, lib, eventID); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	adv, err := lib.GetContext(ctx, "shuf")
	if err != nil {
		t.Fatal(err)
	}
	if adv == nil || len(adv.Lanes) != 1 || adv.Lanes[0].Mode != adventure.LaneModeShuffled {
		t.Fatalf("adventure load: lanes = %+v", adv.Lanes)
	}

	// Each team should get a different first page (Latin-square spread).
	firstPages := make(map[int64]bool)
	for slot, tid := range teamIDs {
		_, pid, err := lanes.FirstPageFor(ctx, db, adv, tid)
		if err != nil {
			t.Fatalf("first page (slot %d): %v", slot, err)
		}
		if pid == 0 {
			t.Fatalf("slot %d: no first page", slot)
		}
		firstPages[pid] = true
	}
	if len(firstPages) != 4 {
		t.Fatalf("expected 4 distinct first pages (Latin-square), got %d", len(firstPages))
	}

	// Walking each team through the lane should visit all 4 pages
	// then end (no more pages, no finale concept).
	for slot, tid := range teamIDs {
		_, cur, err := lanes.FirstPageFor(ctx, db, adv, tid)
		if err != nil {
			t.Fatalf("first (slot %d): %v", slot, err)
		}
		visited := map[int64]bool{}
		for step := 0; step < 4; step++ {
			visited[cur] = true
			_, next, err := lanes.NextPageAfter(ctx, db, adv, tid, cur)
			if err != nil {
				t.Fatalf("next (slot %d step %d): %v", slot, step, err)
			}
			if step < 3 {
				if next == 0 {
					t.Fatalf("slot %d ran out of pages at step %d", slot, step)
				}
				cur = next
				continue
			}
			// Step 3 is the last solve in the lane — next must be 0.
			if next != 0 {
				t.Fatalf("slot %d: expected end of run after last lane page, got %d", slot, next)
			}
		}
		if len(visited) != 4 {
			t.Fatalf("slot %d: expected to visit 4 distinct pages, got %d", slot, len(visited))
		}
		for _, want := range pages {
			if !visited[want] {
				t.Fatalf("slot %d: missed page %d", slot, want)
			}
		}
	}
}

// TestLanes_LinearProgression confirms that linear lanes hand every
// team the same canonical order and that lane-to-lane transitions
// work across multiple sequenced lanes.
func TestLanes_LinearProgression(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	lib := adventure.New(db)

	advID := seedAdventure(t, db, "lin", "Linear demo")
	lane1 := seedLane(t, db, advID, 0, "linear")
	lane2 := seedLane(t, db, advID, 1, "linear")
	p1 := seedPage(t, db, advID, 0, "L1-A", `[{"text":"a"}]`, lane1)
	p2 := seedPage(t, db, advID, 1, "L1-B", `[{"text":"b"}]`, lane1)
	p3 := seedPage(t, db, advID, 2, "L2-A", `[{"text":"c"}]`, lane2)
	eventID := seedEvent(t, db, "lin")
	tid := seedTeam(t, db, eventID, 'a')

	if err := lanes.Initialize(ctx, db, lib, eventID); err != nil {
		t.Fatal(err)
	}
	adv, err := lib.GetContext(ctx, "lin")
	if err != nil {
		t.Fatal(err)
	}

	want := []int64{p1, p2, p3}
	_, cur, _ := lanes.FirstPageFor(ctx, db, adv, tid)
	for i, expect := range want {
		if cur != expect {
			t.Fatalf("step %d: got %d, want %d", i, cur, expect)
		}
		_, cur, _ = lanes.NextPageAfter(ctx, db, adv, tid, cur)
	}
	if cur != 0 {
		t.Fatalf("expected end of run after last linear page, got %d", cur)
	}
}

// TestLanes_PermutationsArePersisted verifies Initialize writes
// team_lane_orderings rows so a later disabling of a page mid-run
// doesn't reshuffle a team's ordering on the fly.
func TestLanes_PermutationsArePersisted(t *testing.T) {
	db, cleanup := dbtest.Open(t)
	defer cleanup()
	ctx := context.Background()
	lib := adventure.New(db)

	advID := seedAdventure(t, db, "persist", "Persisted demo")
	laneID := seedLane(t, db, advID, 0, "shuffled")
	for i := 0; i < 3; i++ {
		seedPage(t, db, advID, i, "P", `[{"text":"x"}]`, laneID)
	}
	eventID := seedEvent(t, db, "persist")
	tid := seedTeam(t, db, eventID, 'a')

	if err := lanes.Initialize(ctx, db, lib, eventID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_lane_orderings WHERE team_id = $1 AND lane_id = $2`,
		tid, laneID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one ordering row, got %d", n)
	}
}

// --- helpers ---

func seedAdventure(t *testing.T, db *sql.DB, slug, title string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO adventures(slug, title, description, author) VALUES ($1, $2, '', '') RETURNING id`,
		slug, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedLane(t *testing.T, db *sql.DB, advID int64, idx int, mode string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO adventure_lanes(adventure_id, idx, mode) VALUES ($1, $2, $3) RETURNING id`,
		advID, idx, mode).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedPage(t *testing.T, db *sql.DB, advID int64, idx int, title, accept string, laneID int64) int64 {
	t.Helper()
	var laneArg any = laneID
	if laneID == 0 {
		laneArg = sql.NullInt64{}
	}
	var id int64
	if err := db.QueryRow(`
		INSERT INTO adventure_pages(adventure_id, idx, title, prompt, accept, lat, lng, lane_id)
		VALUES ($1, $2, $3, '', $4, 0, 0, $5)
		RETURNING id
	`, advID, idx, title, accept, laneArg).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedEvent(t *testing.T, db *sql.DB, slug string) int64 {
	t.Helper()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations(name) VALUES ($1) RETURNING id`, "lanes-test-"+slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`
		INSERT INTO events(adventure_slug, org_id, state, started_at, live_token, name, show_map)
		VALUES ($1, $2, 'running', now_text(), $3, $4, 0)
		RETURNING id
	`, slug, orgID, "tok-"+slug, slug+"-event").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedTeam(t *testing.T, db *sql.DB, eventID int64, suffix byte) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO teams(event_id, name, token) VALUES ($1, $2, $3) RETURNING id`,
		eventID, "team-"+string(suffix), "tok-"+string(suffix)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
