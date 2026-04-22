package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/runevents"
)

// seedDemo wipes any prior run of slug "demo" and installs a self-contained
// demo dataset: a root, an arranger, three teams of players, a running
// adventure run, realistic walking tracks through Stockholm centre, and a
// populated event log so the arranger UI has plenty to look at.
func seedDemo(ctx context.Context, d *sql.DB, advDir string) error {
	lib, err := adventure.Load(advDir)
	if err != nil {
		return fmt.Errorf("load adventures: %w", err)
	}
	adv := lib.Get("demo")
	if adv == nil {
		return errors.New(`adventure "demo" not found — expected under adventures/demo/`)
	}

	// --- Upsert accounts ---
	if _, err := upsertUser(d, "owner", "owner", auth.RoleRoot); err != nil {
		return err
	}
	arrID, err := upsertUser(d, "boss", "s3cret", auth.RoleArranger)
	if err != nil {
		return err
	}
	if _, err := d.Exec(`INSERT OR IGNORE INTO arranger_adventures(arranger_user_id, adventure_slug) VALUES (?, ?)`, arrID, "demo"); err != nil {
		return err
	}

	// --- Reset previous demo runs so the dataset stays deterministic ---
	if _, err := d.Exec(`DELETE FROM adventure_runs WHERE adventure_slug = ? AND arranger_user_id = ?`, "demo", arrID); err != nil {
		return err
	}

	// --- Players (owned by boss, so a different arranger wouldn't see them) ---
	playerNames := []string{
		"alice", "bob", "celine", "dmitri", "erika", "fredrik", "greta", "henrik", "ingrid",
	}
	// Demo players get a stock "pw" password pre-set so the demo is usable
	// out of the box. In production the arranger creates players without
	// passwords and the player picks one on first login.
	playerIDs := map[string]int64{}
	for _, name := range playerNames {
		id, err := upsertUser(d, name, "pw", auth.RolePlayer)
		if err != nil {
			return err
		}
		if _, err := d.Exec(`UPDATE users SET created_by_user_id = ? WHERE id = ?`, arrID, id); err != nil {
			return err
		}
		playerIDs[name] = id
	}

	// --- Run ---
	// Backdate started_at ~30 min so the event feed has a natural span.
	startedAt := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	res, err := d.Exec(`
		INSERT INTO adventure_runs(adventure_slug, arranger_user_id, state, started_at, created_at)
		VALUES (?, ?, 'running', ?, ?)
	`, "demo", arrID, startedAt, startedAt)
	if err != nil {
		return err
	}
	runID, _ := res.LastInsertId()
	runevents.Record(ctx, d, runID, nil, &arrID, runevents.RunCreated,
		map[string]any{"adventure_slug": "demo"})
	runevents.Record(ctx, d, runID, nil, &arrID, runevents.RunStarted, nil)

	// --- Teams ---
	type teamPlan struct {
		Name     string
		Members  []string
		PageIdx  int      // current puzzle index (0-based)
		Progress float64  // 0..1 along the leg from PageIdx's location to PageIdx+1's
	}
	plans := []teamPlan{
		{"Team Alfa", []string{"alice", "bob", "celine"}, 2, 0.55},  // on last leg
		{"Team Beta", []string{"dmitri", "erika", "fredrik"}, 1, 0.70}, // nearly at puzzle 2
		{"Team Gamma", []string{"greta", "henrik", "ingrid"}, 0, 0.25},  // still early
	}

	rng := rand.New(rand.NewSource(1))
	for _, plan := range plans {
		tRes, err := d.Exec(`INSERT INTO teams(run_id, name, current_page_index) VALUES (?, ?, ?)`,
			runID, plan.Name, plan.PageIdx)
		if err != nil {
			return err
		}
		teamID, _ := tRes.LastInsertId()
		for _, m := range plan.Members {
			if _, err := d.Exec(`INSERT INTO team_members(team_id, user_id) VALUES (?, ?)`, teamID, playerIDs[m]); err != nil {
				return err
			}
		}
		members := make([]string, 0, len(plan.Members))
		members = append(members, plan.Members...)
		runevents.Record(ctx, d, runID, &teamID, &arrID, runevents.TeamCreated,
			map[string]any{"name": plan.Name, "members": members})

		// Synthesise tracks: start at page 0's location, walk to each completed
		// page's location, then part-way to the current page's next waypoint.
		if err := seedTrackAndEvents(ctx, d, rng, adv, runID, teamID, plan.Members, playerIDs, plan.PageIdx, plan.Progress); err != nil {
			return err
		}
	}

	return nil
}

func upsertUser(d *sql.DB, username, password, role string) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	res, err := d.Exec(`
		INSERT INTO users(username, password_hash, role) VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash, role = excluded.role
	`, username, hash, role)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// ON CONFLICT path: fetch by username.
		if err := d.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// seedTrackAndEvents writes a plausible walking path for one team plus the
// events that record their progress so far.
func seedTrackAndEvents(ctx context.Context, d *sql.DB, rng *rand.Rand,
	adv *adventure.Adventure, runID, teamID int64,
	memberNames []string, playerIDs map[string]int64,
	currentPage int, partialProgress float64) error {

	// Ordered waypoints the team is travelling through so far.
	type leg struct{ from, to adventure.Location }
	legs := []leg{}
	for i := 0; i < currentPage && i+1 < len(adv.Pages); i++ {
		legs = append(legs, leg{adv.Pages[i].Location, adv.Pages[i+1].Location})
	}
	// In-progress leg from currentPage toward the next page (if any).
	if currentPage+1 < len(adv.Pages) {
		legs = append(legs, leg{adv.Pages[currentPage].Location, adv.Pages[currentPage+1].Location})
	}

	// Timestamps: total track covers the run so far (~30 min). Distribute
	// legs proportionally to their length.
	legDists := make([]float64, len(legs))
	total := 0.0
	for i, l := range legs {
		legDists[i] = haversine(l.from.Lat, l.from.Lng, l.to.Lat, l.to.Lng)
		if i == len(legs)-1 && partialProgress < 1 {
			legDists[i] *= partialProgress
		}
		total += legDists[i]
	}
	if total == 0 {
		return nil
	}
	runDuration := 30 * time.Minute
	now := time.Now().UTC()
	startTS := now.Add(-runDuration)

	// Pick a user per ping round-robin so the team looks lively.
	memberUserIDs := make([]int64, 0, len(memberNames))
	for _, n := range memberNames {
		memberUserIDs = append(memberUserIDs, playerIDs[n])
	}
	if len(memberUserIDs) == 0 {
		return nil
	}

	legStart := startTS
	for li, l := range legs {
		frac := legDists[li] / total
		legDuration := time.Duration(float64(runDuration) * frac)
		legEnd := legStart.Add(legDuration)

		// ~1 ping every 20 seconds, minimum 3 points.
		steps := int(legDuration.Seconds() / 20)
		if steps < 3 {
			steps = 3
		}
		progressEnd := 1.0
		if li == len(legs)-1 && partialProgress < 1 {
			progressEnd = partialProgress
		}

		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			lat := lerp(l.from.Lat, l.to.Lat, t*progressEnd) + (rng.Float64()-0.5)*0.00008
			lng := lerp(l.from.Lng, l.to.Lng, t*progressEnd) + (rng.Float64()-0.5)*0.00012
			ts := legStart.Add(time.Duration(float64(legDuration) * t))
			uid := memberUserIDs[s%len(memberUserIDs)]
			if _, err := d.ExecContext(ctx, `
				INSERT INTO location_pings(user_id, team_id, run_id, lat, lng, accuracy, server_ts)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, uid, teamID, runID, lat, lng, 12.0+rng.Float64()*6, ts.Format(time.RFC3339)); err != nil {
				return err
			}
		}

		// When a leg completes (i.e. the team reached the next puzzle), log
		// the corresponding right-answer event at legEnd. Also a couple of
		// wrong guesses for flavour on the current page.
		if li < currentPage {
			// Arriving at puzzle (li+1) means they solved puzzle li and moved on.
			payload, _ := json.Marshal(map[string]any{"page_index": li})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO run_events(run_id, team_id, actor_id, kind, payload, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, runID, teamID, memberUserIDs[0], runevents.AnswerRight, string(payload), legEnd.Add(-2*time.Minute).Format(time.RFC3339))
			// One wrong guess a minute before the right one.
			payload, _ = json.Marshal(map[string]any{"page_index": li, "submitted": pickWrongGuess(rng)})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO run_events(run_id, team_id, actor_id, kind, payload, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, runID, teamID, memberUserIDs[0], runevents.AnswerWrong, string(payload), legEnd.Add(-3*time.Minute).Format(time.RFC3339))
		}

		legStart = legEnd
	}

	return nil
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// Duplicated from the player package so the seed command can use it without
// pulling that package in. Keeps main.go's dependency footprint small.
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

var wrongGuesses = []string{"hej", "goodbye", "green", "red", "adventure", "final"}

func pickWrongGuess(rng *rand.Rand) string {
	return wrongGuesses[rng.Intn(len(wrongGuesses))]
}
