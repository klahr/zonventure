package adventure

import "testing"

func TestSynthesizeDefaultLane_NoLanesNoPages(t *testing.T) {
	adv := &Adventure{}
	synthesizeDefaultLane(adv)
	if len(adv.Lanes) != 0 {
		t.Fatalf("empty adventure should not synthesise a lane, got %v", adv.Lanes)
	}
}

func TestSynthesizeDefaultLane_NoLanesWithPages(t *testing.T) {
	adv := &Adventure{
		Pages: []Page{
			{ID: 1, Index: 0},
			{ID: 2, Index: 1},
			{ID: 3, Index: 2, Disabled: true},
		},
	}
	synthesizeDefaultLane(adv)
	if len(adv.Lanes) != 1 {
		t.Fatalf("expected one synthesised lane, got %d", len(adv.Lanes))
	}
	ln := adv.Lanes[0]
	if !IsSynthesizedLane(ln.ID) {
		t.Fatalf("synthesised lane id should be < 0, got %d", ln.ID)
	}
	if ln.Mode != LaneModeLinear {
		t.Fatalf("synthesised lane mode = %q, want linear", ln.Mode)
	}
	for i, p := range adv.Pages {
		if !IsSynthesizedLane(p.LaneID) {
			t.Fatalf("page %d lane id = %d, want synthesised", i, p.LaneID)
		}
	}
}

func TestSynthesizeDefaultLane_SkipsWhenLanesAlreadyPresent(t *testing.T) {
	adv := &Adventure{
		Lanes: []Lane{{ID: 7, Index: 0, Mode: LaneModeShuffled}},
		Pages: []Page{{ID: 1, Index: 0, LaneID: 7}},
	}
	synthesizeDefaultLane(adv)
	if len(adv.Lanes) != 1 || adv.Lanes[0].ID != 7 {
		t.Fatalf("existing lanes should be left alone, got %v", adv.Lanes)
	}
}

func TestLanePages_FiltersByLaneAndDisabled(t *testing.T) {
	adv := &Adventure{
		Lanes: []Lane{{ID: 5, Index: 0, Mode: LaneModeLinear}},
		Pages: []Page{
			{ID: 1, Index: 0, LaneID: 5},
			{ID: 2, Index: 1, LaneID: 5, Disabled: true},
			{ID: 3, Index: 2, LaneID: 9},
			{ID: 4, Index: 3, LaneID: 5},
		},
	}
	got := adv.LanePages(5)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 4 {
		t.Fatalf("LanePages = %v, want IDs [1, 4]", got)
	}
}

func TestNextLane(t *testing.T) {
	adv := &Adventure{
		Lanes: []Lane{
			{ID: 10, Index: 0, Mode: LaneModeLinear},
			{ID: 20, Index: 1, Mode: LaneModeShuffled},
			{ID: 30, Index: 2, Mode: LaneModeLinear},
		},
	}
	cur := adv.LaneByID(10)
	next := adv.NextLane(cur)
	if next == nil || next.ID != 20 {
		t.Fatalf("next after lane 10 = %v, want lane 20", next)
	}
	last := adv.NextLane(adv.LaneByID(30))
	if last != nil {
		t.Fatalf("next after last lane should be nil, got %v", last)
	}
}
