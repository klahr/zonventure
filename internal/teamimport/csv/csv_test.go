package csv

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/teamimport"
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

func TestCSV_SwedishSignupHeaderSkipped(t *testing.T) {
	// A sign-up sheet's header names a person and an email column, not
	// a team; it must not become a team "Förnamn".
	got, _ := New().Parse(strings.NewReader("Elev,Klass,E-post\nMärta,7B,marta@lindholm.test\n"))
	if len(got) != 1 || got[0].Name != "Märta" {
		t.Errorf("teams = %+v, want one team Märta", got)
	}
	// A first row of real names and an address stays a team.
	got, _ = New().Parse(strings.NewReader("Alpha,alice,alice@example.org\n"))
	if len(got) != 1 || got[0].Name != "Alpha" {
		t.Errorf("teams = %+v, want one Alpha team", got)
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

func TestCSV_ClassListGroupedByGroup(t *testing.T) {
	// A class list: rows are students, the Group column their team.
	for _, in := range []string{
		"First name,Last name,Group\nAda,Lind,Red\nBo,Ek,Blue\nCi,Ås,Red\n",
		"Förnamn,Efternamn,Grupp,Förälders e-post\nAda,Lind,Red,a@x.test\nBo,Ek,Blue,b@x.test\nCi,Ås,Red,c@x.test\n",
	} {
		got, err := New().Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		want := []teamimport.Team{
			{Name: "Red", MemberNames: []string{"Ada Lind", "Ci Ås"}},
			{Name: "Blue", MemberNames: []string{"Bo Ek"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: teams = %+v, want %+v", in, got, want)
		}
	}
	// The team-per-row format with a header keeps its shape.
	got, _ := New().Parse(strings.NewReader("Team,Name,Name\nAlpha,alice,bob\n"))
	if len(got) != 1 || got[0].Name != "Alpha" || len(got[0].MemberNames) != 2 {
		t.Errorf("teams = %+v, want Alpha with two players", got)
	}
}

func TestCSV_ReportsRowsWithoutTeam(t *testing.T) {
	got, err := New().Parse(strings.NewReader("Alpha,alice\n,bob\nBeta,dave\n"))
	var skipped *teamimport.SkippedRows
	if !errors.As(err, &skipped) || !reflect.DeepEqual(skipped.Rows, []int{2}) {
		t.Fatalf("err = %v, want row 2 skipped", err)
	}
	if len(got) != 2 {
		t.Errorf("teams = %+v, want the two named ones", got)
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
