package teamimport

import (
	"reflect"
	"testing"
)

func TestRead_ClassListEmailAndIgnoredColumns(t *testing.T) {
	// A class list with the pupil's own email, a parent's email and a
	// class column: the pupil's address is the team's (its first row's),
	// the parent's is never read, and both left-out columns are named.
	s, err := Read([][]string{
		{"Förnamn", "Efternamn", "Grupp", "Förälders e-post", "E-post", "Klass"},
		{"Ada", "Lind", "Röd", "mor@x.test", "", "7B"},
		{"Bo", "Ek", "Blå", "far@x.test", "bo@x.test", "7B"},
		{"Ci", "Ås", "Röd", "mor2@x.test", "ci@x.test", "7B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTeams := []Team{
		{Name: "Röd", MemberNames: []string{"Ada Lind", "Ci Ås"}, Email: "ci@x.test"},
		{Name: "Blå", MemberNames: []string{"Bo Ek"}, Email: "bo@x.test"},
	}
	if !reflect.DeepEqual(s.Teams, wantTeams) {
		t.Errorf("teams = %+v, want %+v", s.Teams, wantTeams)
	}
	wantCols := []Column{
		{"Förnamn", RolePlayer}, {"Efternamn", RolePlayer}, {"Grupp", RoleTeam},
		{"Förälders e-post", RoleIgnored}, {"E-post", RoleEmail}, {"Klass", RoleIgnored},
	}
	if !reflect.DeepEqual(s.Columns, wantCols) {
		t.Errorf("columns = %+v, want %+v", s.Columns, wantCols)
	}
}

func TestRead_TeamPerRowEmail(t *testing.T) {
	// The team-per-row format: an email column is the team's, a parent's
	// is no player, the rest are players.
	s, err := Read([][]string{
		{"Lag", "Spelare", "Spelare", "E-post", "Parent email"},
		{"Alpha", "alice", "bob", "alpha@x.test", "p@x.test"},
		{"Beta", "carol", "", "", "q@x.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTeams := []Team{
		{Name: "Alpha", MemberNames: []string{"alice", "bob"}, Email: "alpha@x.test"},
		{Name: "Beta", MemberNames: []string{"carol"}},
	}
	if !reflect.DeepEqual(s.Teams, wantTeams) {
		t.Errorf("teams = %+v, want %+v", s.Teams, wantTeams)
	}
	wantCols := []Column{
		{"Lag", RoleTeam}, {"Spelare", RolePlayer}, {"Spelare", RolePlayer}, {"E-post", RoleEmail}, {"Parent email", RoleIgnored},
	}
	if !reflect.DeepEqual(s.Columns, wantCols) {
		t.Errorf("columns = %+v, want %+v", s.Columns, wantCols)
	}

	// Without a header every column is the team or a player, unnamed.
	s, _ = Read([][]string{{"Alpha", "alice", "bob"}, {"Beta", "carol"}})
	wantCols = []Column{{"", RoleTeam}, {"", RolePlayer}, {"", RolePlayer}}
	if !reflect.DeepEqual(s.Columns, wantCols) || s.Teams[0].Email != "" {
		t.Errorf("headerless = %+v, want columns %+v and no email", s, wantCols)
	}
}
