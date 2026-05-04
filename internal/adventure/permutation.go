package adventure

// LanePermutation returns the order in which a team should visit a
// lane's pages, given the lane's mode and the team's slot within the
// run (the team's 0-based position when teams are ordered by id —
// cheap, deterministic, and stable for the lifetime of the run).
//
// Linear lanes return canonicalIDs unchanged: every team visits the
// pages in the same idx order.
//
// Shuffled lanes return a cyclic shift of canonicalIDs by
// teamSlot % len(canonicalIDs). With N teams and N pages this gives
// a Latin square — every page is the lane's first stop for exactly
// one team, the second stop for exactly one team, and so on, which
// is the strongest anti-congestion distribution we can get with a
// pure rotation. With more teams than pages the rotation cycles and
// some pages get two teams in the same slot; with fewer teams than
// pages, some pages skip a team but every team's path still covers
// the whole lane.
//
// The choice of cyclic shifts (rather than independent random
// permutations per team) is deliberate. It's deterministic, easy to
// reason about ("team 3 starts at page 3"), and never produces the
// pathological "two teams roll the same first page" outcome that
// random sampling occasionally does.
func LanePermutation(mode LaneMode, canonicalIDs []int64, teamSlot int) []int64 {
	n := len(canonicalIDs)
	if n == 0 {
		return nil
	}
	out := make([]int64, n)
	if mode != LaneModeShuffled {
		copy(out, canonicalIDs)
		return out
	}
	shift := ((teamSlot % n) + n) % n
	for i := 0; i < n; i++ {
		out[i] = canonicalIDs[(shift+i)%n]
	}
	return out
}

// CanonicalLanePageIDs returns the enabled-page ids in lane laneID,
// in idx order — the input every permutation builds on. Pulled out
// as its own helper so callers (run-start permutation generation,
// runtime fallback when an ordering row is missing) share the same
// definition of "the canonical order of a lane".
func CanonicalLanePageIDs(adv *Adventure, laneID int64) []int64 {
	pages := adv.LanePages(laneID)
	out := make([]int64, len(pages))
	for i, p := range pages {
		out[i] = p.ID
	}
	return out
}
