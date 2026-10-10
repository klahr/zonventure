package play

import (
	"context"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/activity"
	"github.com/klahr/zonventure/internal/results"
)

// Play writes the event's results record as it goes: every answer, right
// or wrong, with the team's standing after it; a finished team gets a
// receipt; ending the event seals it. The record recomputes, and an
// edit to it or to the results tables shows — against the receipt too.
func TestResultsRecordPlay(t *testing.T) {
	t.Parallel()
	f := setup(t, linear("red", "blue"), 2)
	ctx := context.Background()
	done, playing := f.teams[0], f.teams[1]

	f.answer(done, "red")
	f.answer(done, "green")
	f.answer(done, "blue")
	f.answer(playing, "red")
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AdjustScore(ctx, tx, playing, 5); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if ok, err := activity.CompleteEvent(ctx, f.db, f.eventID, activity.Actor{}, nil); err != nil || !ok {
		t.Fatalf("complete: %v %v", ok, err)
	}

	rep, err := results.Verify(ctx, f.db, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	// 3 answers and a finish, 1 answer and a score change, the end.
	if !rep.OK || rep.Entries != 7 || !rep.Sealed || rep.Fingerprint == "" || rep.Fingerprint != rep.Head {
		t.Fatalf("report = %+v", rep)
	}
	var wrong string
	mustScan(t, f.db.QueryRow(`SELECT payload FROM event_result_log WHERE event_id = $1 AND payload LIKE '%"input":"green"%'`, f.eventID), &wrong)
	if !strings.Contains(wrong, `"attempts":1`) || !strings.Contains(wrong, `"points":10`) {
		t.Errorf("wrong answer entry = %s", wrong)
	}
	for _, c := range rep.Teams {
		if !c.Matches || c.Recorded == nil {
			t.Errorf("team %d: %+v", c.TeamID, c)
		}
		switch c.TeamID {
		case done:
			if c.Receipt == "" || c.Current.Points != 20 || c.Current.CompletedAt == "" {
				t.Errorf("finished team = %+v", c)
			}
		case playing:
			if c.Receipt != "" || c.Current.Points != 15 {
				t.Errorf("playing team = %+v", c)
			}
		}
	}
	receipt, err := results.ReceiptOf(ctx, f.db, done)
	if err != nil || !receipt.Matches || receipt.Fingerprint != rep.Fingerprint {
		t.Fatalf("receipt = %+v (%v)", receipt, err)
	}

	// The record refuses edits and deletes.
	if _, err := f.db.Exec(`UPDATE event_result_log SET payload = '{}' WHERE event_id = $1`, f.eventID); err == nil {
		t.Fatal("the record took an edit")
	}
	if _, err := f.db.Exec(`DELETE FROM event_result_log WHERE event_id = $1`, f.eventID); err == nil {
		t.Fatal("the record took a delete")
	}

	// A score changed in the results tables, past the record, shows.
	if _, err := f.db.Exec(`UPDATE team_graph_state SET score = 99 WHERE team_id = $1`, playing); err != nil {
		t.Fatal(err)
	}
	if rep, _ := results.Verify(ctx, f.db, f.eventID); rep.OK || !hasProblem(rep, "result now differs") {
		t.Fatalf("after a score edit: %+v", rep.Problems)
	}

	// Someone with the database rewrites the wrong answer: the record,
	// and the finished team's receipt, no longer add up.
	for _, q := range []string{
		`ALTER TABLE event_result_log DISABLE TRIGGER event_result_log_append_only`,
		`UPDATE event_result_log SET payload = replace(payload, '"input":"green"', '"input":"blue"') WHERE payload LIKE '%"input":"green"%'`,
		`ALTER TABLE event_result_log ENABLE TRIGGER event_result_log_append_only`,
	} {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if rep, _ := results.Verify(ctx, f.db, f.eventID); rep.OK || !hasProblem(rep, "changed after it was recorded") {
		t.Fatalf("after rewriting an answer: %+v", rep.Problems)
	}
	if receipt, err := results.ReceiptOf(ctx, f.db, done); err != nil || receipt.Matches {
		t.Fatalf("receipt after rewriting an answer = %+v (%v)", receipt, err)
	}

	// The record goes with its event.
	if _, err := f.db.Exec(`DELETE FROM events WHERE id = $1`, f.eventID); err != nil {
		t.Fatalf("deleting the event: %v", err)
	}
	var left int
	mustScan(t, f.db.QueryRow(`SELECT count(*) FROM event_result_log WHERE event_id = $1`, f.eventID), &left)
	if left != 0 {
		t.Fatalf("%d entries outlived their event", left)
	}
}

func hasProblem(rep results.Report, s string) bool {
	for _, p := range rep.Problems {
		if strings.Contains(p.Problem, s) {
			return true
		}
	}
	return false
}
