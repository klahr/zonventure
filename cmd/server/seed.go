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

// seedDemo installs a self-contained dataset:
//   - a global admin (admin/admin)
//   - an organization "Demo Arrangers"
//   - boss (org_admin) and jane (org_member) inside that org
//   - one used assignment (5 teams) linked to a running demo run
//   - one spare assignment (3 teams)
//   - three teams with 3 slots each (9 slots total), backfilled with
//     realistic Stockholm-centre tracks and an event log.
func seedDemo(ctx context.Context, d *sql.DB, advDir string) error {
	lib, err := adventure.Load(advDir)
	if err != nil {
		return fmt.Errorf("load adventures: %w", err)
	}
	adv := lib.Get("demo")
	if adv == nil {
		return errors.New(`adventure "demo" not found — expected under adventures/demo/`)
	}

	// Wipe the mutable part of the dataset.
	if _, err := d.Exec(`DELETE FROM users WHERE role IN ('org_admin','org_member')`); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM organizations`); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM adventure_runs`); err != nil {
		return err
	}

	if _, err := upsertUser(d, "admin", "admin", auth.RoleAdmin, nil); err != nil {
		return err
	}

	orgRes, err := d.Exec(`INSERT INTO organizations(name) VALUES (?)`, "Demo Arrangers")
	if err != nil {
		return err
	}
	orgID, _ := orgRes.LastInsertId()

	bossID, err := upsertUser(d, "boss", "s3cret", auth.RoleOrgAdmin, &orgID)
	if err != nil {
		return err
	}
	if _, err := upsertUser(d, "jane", "jane", auth.RoleOrgMember, &orgID); err != nil {
		return err
	}

	assignRes, err := d.Exec(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES (?, ?, ?)
	`, orgID, "demo", 5)
	if err != nil {
		return err
	}
	assignmentID, _ := assignRes.LastInsertId()
	if _, err := d.Exec(`
		INSERT INTO adventure_assignments(org_id, adventure_slug, max_teams)
		VALUES (?, ?, ?)
	`, orgID, "demo", 3); err != nil {
		return err
	}

	startedAt := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	res, err := d.Exec(`
		INSERT INTO adventure_runs(adventure_slug, org_id, created_by_user_id, state, started_at, created_at, assignment_id)
		VALUES (?, ?, ?, 'running', ?, ?, ?)
	`, "demo", orgID, bossID, startedAt, startedAt, assignmentID)
	if err != nil {
		return err
	}
	runID, _ := res.LastInsertId()
	runevents.Record(ctx, d, runID, nil, runevents.UserActor(bossID), runevents.RunCreated,
		map[string]any{"adventure_slug": "demo"})
	runevents.Record(ctx, d, runID, nil, runevents.UserActor(bossID), runevents.RunStarted, nil)

	type teamPlan struct {
		Name     string
		Slots    []string
		PageIdx  int
		Progress float64
	}
	plans := []teamPlan{
		{"Team Alfa", []string{"alice", "bob", "celine"}, 2, 0.55},
		{"Team Beta", []string{"dmitri", "erika", "fredrik"}, 1, 0.70},
		{"Team Gamma", []string{"greta", "henrik", "ingrid"}, 0, 0.25},
	}

	rng := rand.New(rand.NewSource(1))
	for _, plan := range plans {
		tRes, err := d.Exec(`INSERT INTO teams(run_id, name, current_page_index) VALUES (?, ?, ?)`,
			runID, plan.Name, plan.PageIdx)
		if err != nil {
			return err
		}
		teamID, _ := tRes.LastInsertId()

		slotIDs := make([]int64, 0, len(plan.Slots))
		for _, name := range plan.Slots {
			token, err := auth.NewSlotToken()
			if err != nil {
				return err
			}
			sRes, err := d.Exec(
				`INSERT INTO player_slots(team_id, name, token) VALUES (?, ?, ?)`,
				teamID, name, token)
			if err != nil {
				return err
			}
			id, _ := sRes.LastInsertId()
			slotIDs = append(slotIDs, id)
		}
		runevents.Record(ctx, d, runID, &teamID, runevents.UserActor(bossID), runevents.TeamCreated,
			map[string]any{"name": plan.Name, "members": plan.Slots})

		if err := seedTrackAndEvents(ctx, d, rng, adv, runID, teamID, slotIDs, plan.PageIdx, plan.Progress); err != nil {
			return err
		}
	}
	return nil
}

// upsertUser inserts or updates a staff user by username. Pass orgID=nil
// for admins.
func upsertUser(d *sql.DB, username, password, role string, orgID *int64) (int64, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, err
	}
	var orgVal any
	if orgID != nil {
		orgVal = *orgID
	}
	res, err := d.Exec(`
		INSERT INTO users(username, password_hash, role, org_id) VALUES (?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			password_hash = excluded.password_hash,
			role = excluded.role,
			org_id = excluded.org_id
	`, username, hash, role, orgVal)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		if err := d.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// seedTrackAndEvents writes plausible walking pings + answer events for a
// team, using slot ids as the authors of the pings/events.
func seedTrackAndEvents(ctx context.Context, d *sql.DB, rng *rand.Rand,
	adv *adventure.Adventure, runID, teamID int64,
	slotIDs []int64, currentPage int, partialProgress float64) error {

	type leg struct{ from, to adventure.Location }
	legs := []leg{}
	for i := 0; i < currentPage && i+1 < len(adv.Pages); i++ {
		legs = append(legs, leg{adv.Pages[i].Location, adv.Pages[i+1].Location})
	}
	if currentPage+1 < len(adv.Pages) {
		legs = append(legs, leg{adv.Pages[currentPage].Location, adv.Pages[currentPage+1].Location})
	}
	legDists := make([]float64, len(legs))
	total := 0.0
	for i, l := range legs {
		legDists[i] = haversine(l.from.Lat, l.from.Lng, l.to.Lat, l.to.Lng)
		if i == len(legs)-1 && partialProgress < 1 {
			legDists[i] *= partialProgress
		}
		total += legDists[i]
	}
	if total == 0 || len(slotIDs) == 0 {
		return nil
	}
	runDuration := 30 * time.Minute
	startTS := time.Now().UTC().Add(-runDuration)

	legStart := startTS
	for li, l := range legs {
		frac := legDists[li] / total
		legDuration := time.Duration(float64(runDuration) * frac)
		legEnd := legStart.Add(legDuration)

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
			sid := slotIDs[s%len(slotIDs)]
			if _, err := d.ExecContext(ctx, `
				INSERT INTO location_pings(slot_id, team_id, run_id, lat, lng, accuracy, server_ts)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, sid, teamID, runID, lat, lng, 12.0+rng.Float64()*6, ts.Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if li < currentPage {
			payload, _ := json.Marshal(map[string]any{"page_index": li})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO run_events(run_id, team_id, actor_slot_id, kind, payload, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, runID, teamID, slotIDs[0], runevents.AnswerRight, string(payload),
				legEnd.Add(-2*time.Minute).Format(time.RFC3339))
			payload, _ = json.Marshal(map[string]any{"page_index": li, "submitted": pickWrongGuess(rng)})
			_, _ = d.ExecContext(ctx, `
				INSERT INTO run_events(run_id, team_id, actor_slot_id, kind, payload, created_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, runID, teamID, slotIDs[0], runevents.AnswerWrong, string(payload),
				legEnd.Add(-3*time.Minute).Format(time.RFC3339))
		}
		legStart = legEnd
	}
	return nil
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

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
