package csv

import (
	"reflect"
	"strings"
	"testing"

	"git.sr.ht/~klahr/zv/internal/teamimport"
)

func TestCSV_Basic(t *testing.T) {
	in := "Alpha,alice,bob,carol\nBeta,dave,ezra\n"
	got, err := New().Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []teamimport.Team{
		{Name: "Alpha", MemberNames: []string{"alice", "bob", "carol"}},
		{Name: "Beta", MemberNames: []string{"dave", "ezra"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("teams = %+v, want %+v", got, want)
	}
}

func TestCSV_HeaderRowSkipped(t *testing.T) {
	// "Team" in the first cell triggers the header heuristic and the
	// row is dropped. Without skipping, the importer would mint a
	// team literally named "Team".
	in := "Team,Member 1,Member 2\nAlpha,alice,bob\n"
	got, _ := New().Parse(strings.NewReader(in))
	if len(got) != 1 || got[0].Name != "Alpha" {
		t.Fatalf("teams = %+v, want one Alpha team", got)
	}
}

func TestCSV_TrailingCommaIgnored(t *testing.T) {
	// Excel "save as CSV" sometimes emits trailing commas to pad
	// short rows up to the widest row. Empty trailing cells must
	// not turn into empty player slots.
	in := "Alpha,alice,bob,,,\nBeta,carol,,\n"
	got, _ := New().Parse(strings.NewReader(in))
	if len(got) != 2 {
		t.Fatalf("teams = %+v, want 2", got)
	}
	if !reflect.DeepEqual(got[0].MemberNames, []string{"alice", "bob"}) {
		t.Errorf("Alpha members = %v, want [alice bob]", got[0].MemberNames)
	}
	if !reflect.DeepEqual(got[1].MemberNames, []string{"carol"}) {
		t.Errorf("Beta members = %v, want [carol]", got[1].MemberNames)
	}
}

func TestCSV_SkipsBlankAndNamelessRows(t *testing.T) {
	// A bare comma is "no team name" — silently dropped rather
	// than yielding a Team{Name: ""} that BulkCreateTeams would
	// reject downstream.
	in := "Alpha,alice\n\n,bob,carol\nBeta,dave\n"
	got, _ := New().Parse(strings.NewReader(in))
	names := make([]string, len(got))
	for i, tm := range got {
		names[i] = tm.Name
	}
	if !reflect.DeepEqual(names, []string{"Alpha", "Beta"}) {
		t.Errorf("names = %v, want [Alpha Beta]", names)
	}
}

func TestCSV_ImporterMetadata(t *testing.T) {
	imp := New()
	if imp.Name() != "CSV" {
		t.Errorf("Name = %q", imp.Name())
	}
	if !contains(imp.Extensions(), ".csv") {
		t.Errorf("Extensions missing .csv: %v", imp.Extensions())
	}
	if !contains(imp.MIMETypes(), "text/csv") {
		t.Errorf("MIMETypes missing text/csv: %v", imp.MIMETypes())
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
