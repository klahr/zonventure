package session

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// In a graph played as stations a team is at the zone whose card it
// scanned last, wherever its phones are — and the phone is told where
// nothing is.
func TestStations_AtTheScannedCard(t *testing.T) {
	g := &graph.Graph{
		Stations: true,
		Zones:    []graph.Zone{{Slug: "oak", Name: "The old oak", Lat: 59.33, Lng: 18.06, RadiusM: 30}, {Slug: "pond", Lat: 59.34, Lng: 18.07, RadiusM: 30}},
	}
	tm := &Team{G: g, Now: t0, Size: 3}
	tm.Init()
	// Standing right in the oak's circle counts for nothing.
	tm.Position = &Position{Lat: 59.33, Lng: 18.06}
	if tm.InZone("oak") {
		t.Error("in the zone by GPS, with no card scanned")
	}
	tm.Station = "oak"
	if !tm.InZone("oak") || tm.InZone("pond") {
		t.Errorf("after scanning the oak: oak %v, pond %v", tm.InZone("oak"), tm.InZone("pond"))
	}
	if n := tm.PlayersInZone("oak"); n != 3 {
		t.Errorf("players at the card = %d, want the whole team", n)
	}
	tm.Viewer = &Viewer{Slot: 1}
	if !tm.Here("oak") {
		t.Error("a phone of the team is not here at its card")
	}
	if !tm.ZonesHeld([]string{"oak"}) || tm.ZonesHeld([]string{"oak", "pond"}) {
		t.Error("zones held at one card")
	}
	tm.StartPlace = &graph.Zone{Slug: StartPlaceSlug, Lat: 0, Lng: 0, RadiusM: 10}
	if !tm.AtStartPlace() {
		t.Error("a party has no starting point to reach")
	}

	v := View{Stations: true, Zone: &g.Zones[0]}
	v.HidePlaces()
	if v.Zone.Name != "The old oak" || v.Zone.Lat != 0 || v.Zone.RadiusM != 0 {
		t.Errorf("phone's zone = %+v, want the name only", v.Zone)
	}
}
