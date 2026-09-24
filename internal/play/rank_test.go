package play

import (
	"math"
	"testing"
	"time"
)

func TestRankingRule(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	inf := math.Inf(1)
	rs := []Ranked{
		{Name: "Slow", Score: 5, Done: true, CompletedAt: t0.Add(2 * time.Hour), Miss: 1},
		{Name: "Close", Score: 5, Done: true, CompletedAt: t0.Add(3 * time.Hour), Miss: 0.5},
		{Name: "Far", Score: 5, Done: true, CompletedAt: t0.Add(time.Hour), Miss: 9},
		{Name: "Nothing", Score: 5, Done: true, CompletedAt: t0, Miss: inf},
		{Name: "Leader", Score: 8},
		{Name: "Walking", Score: 5, Miss: 0},
		{Name: "Behind", Score: 1, Done: true, Miss: 0},
	}
	SortRanking(rs)
	var got []string
	for _, r := range rs {
		got = append(got, r.Name)
	}
	// Points first, finished or not; among the finished on 5, the closer
	// guess, and no guess last; a team still walking is level with them
	// all, so it keeps its place by name.
	want := []string{"Leader", "Close", "Slow", "Far", "Nothing", "Walking", "Behind"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
	a := Ranked{Score: 5, Done: true, Miss: 2, CompletedAt: t0}
	b := Ranked{Score: 5, Done: true, Miss: 2, CompletedAt: t0.Add(time.Minute)}
	if !Ahead(a, b) || Ahead(b, a) {
		t.Error("the same guess falls back to the earlier finish")
	}
	if Ahead(Ranked{Score: 5, Miss: 0}, Ranked{Score: 5, Done: true, Miss: 9}) {
		t.Error("a tie-break only counts between finished teams")
	}
}
