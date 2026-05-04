package adventure

import (
	"reflect"
	"testing"
)

func TestLanePermutation_Linear(t *testing.T) {
	canonical := []int64{10, 20, 30, 40}
	for slot := 0; slot < 8; slot++ {
		got := LanePermutation(LaneModeLinear, canonical, slot)
		if !reflect.DeepEqual(got, canonical) {
			t.Fatalf("slot %d: got %v, want %v", slot, got, canonical)
		}
	}
}

func TestLanePermutation_Shuffled_LatinSquare(t *testing.T) {
	canonical := []int64{1, 2, 3, 4}
	// With N=4 teams and N=4 pages, every page should appear in every
	// slot position exactly once across the four teams.
	got := make([][]int64, 4)
	for slot := 0; slot < 4; slot++ {
		got[slot] = LanePermutation(LaneModeShuffled, canonical, slot)
	}
	// Every team's permutation should be a rotation of the canonical
	// list.
	wantSlot := [][]int64{
		{1, 2, 3, 4},
		{2, 3, 4, 1},
		{3, 4, 1, 2},
		{4, 1, 2, 3},
	}
	if !reflect.DeepEqual(got, wantSlot) {
		t.Fatalf("rotations: got %v, want %v", got, wantSlot)
	}
	// Latin-square property: each column has each value exactly once.
	for col := 0; col < 4; col++ {
		seen := map[int64]bool{}
		for slot := 0; slot < 4; slot++ {
			seen[got[slot][col]] = true
		}
		if len(seen) != 4 {
			t.Fatalf("column %d not a full set: %v", col, seen)
		}
	}
}

func TestLanePermutation_Shuffled_MoreTeamsThanPages(t *testing.T) {
	canonical := []int64{1, 2, 3}
	// Slot 3 should wrap back to the same permutation as slot 0.
	if !reflect.DeepEqual(
		LanePermutation(LaneModeShuffled, canonical, 0),
		LanePermutation(LaneModeShuffled, canonical, 3),
	) {
		t.Fatalf("slot 3 should equal slot 0 (cycle of N=3)")
	}
}

func TestLanePermutation_Empty(t *testing.T) {
	if got := LanePermutation(LaneModeShuffled, nil, 0); got != nil {
		t.Fatalf("empty lane: got %v, want nil", got)
	}
}

func TestLanePermutation_DoesNotAliasInput(t *testing.T) {
	canonical := []int64{1, 2, 3}
	got := LanePermutation(LaneModeShuffled, canonical, 1)
	got[0] = 999
	if canonical[0] != 1 {
		t.Fatalf("permutation aliased the input slice")
	}
}
