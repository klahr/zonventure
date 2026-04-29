package iofxml

import (
	"strings"
	"testing"
)

const sampleEntryList = `<?xml version="1.0" encoding="UTF-8"?>
<EntryList xmlns="http://www.orienteering.org/datastandard/3.0">
  <TeamEntry>
    <Name>Stockholm OK</Name>
    <TeamEntryPerson>
      <Person>
        <Name>
          <Family>Andersson</Family>
          <Given>Alice</Given>
        </Name>
      </Person>
    </TeamEntryPerson>
    <TeamEntryPerson>
      <Person>
        <Name>
          <Family>Bergström</Family>
          <Given>Bo</Given>
        </Name>
      </Person>
    </TeamEntryPerson>
  </TeamEntry>
  <TeamEntry>
    <Name>OK Linné</Name>
    <TeamEntryPerson>
      <Person>
        <Name>
          <Family>Carlsson</Family>
          <Given>Carla</Given>
        </Name>
      </Person>
    </TeamEntryPerson>
  </TeamEntry>
</EntryList>`

func TestIOFXML_EntryListParsesTeamsAndMembers(t *testing.T) {
	got, err := New().Parse(strings.NewReader(sampleEntryList))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("teams = %d, want 2", len(got))
	}
	if got[0].Name != "Stockholm OK" {
		t.Errorf("team[0].Name = %q", got[0].Name)
	}
	want0 := []string{"Alice Andersson", "Bo Bergström"}
	if !equalStrings(got[0].MemberNames, want0) {
		t.Errorf("team[0].Members = %v, want %v", got[0].MemberNames, want0)
	}
	if got[1].Name != "OK Linné" {
		t.Errorf("team[1].Name = %q", got[1].Name)
	}
	if !equalStrings(got[1].MemberNames, []string{"Carla Carlsson"}) {
		t.Errorf("team[1].Members = %v", got[1].MemberNames)
	}
}

const sampleStartList = `<?xml version="1.0" encoding="UTF-8"?>
<StartList>
  <TeamStart>
    <Name>Relay Team Sweden</Name>
    <TeamMemberStart>
      <Person>
        <Name>
          <Family>Eriksson</Family>
          <Given>Erik</Given>
        </Name>
      </Person>
    </TeamMemberStart>
  </TeamStart>
</StartList>`

func TestIOFXML_StartListAccepted(t *testing.T) {
	got, err := New().Parse(strings.NewReader(sampleStartList))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Relay Team Sweden" {
		t.Fatalf("got %+v", got)
	}
	if !equalStrings(got[0].MemberNames, []string{"Erik Eriksson"}) {
		t.Errorf("members = %v", got[0].MemberNames)
	}
}

func TestIOFXML_GivenOnlyOrFamilyOnly(t *testing.T) {
	// An entry with only one of Given/Family still yields a name —
	// some federations write their juniors as Family-only or
	// Given-only depending on how the registration form was set up.
	xml := `<EntryList>
		<TeamEntry>
			<Name>Half-named</Name>
			<TeamEntryPerson><Person><Name><Given>Solo</Given></Name></Person></TeamEntryPerson>
			<TeamEntryPerson><Person><Name><Family>Lone</Family></Name></Person></TeamEntryPerson>
		</TeamEntry>
	</EntryList>`
	got, _ := New().Parse(strings.NewReader(xml))
	if len(got) != 1 {
		t.Fatalf("teams = %d", len(got))
	}
	if !equalStrings(got[0].MemberNames, []string{"Solo", "Lone"}) {
		t.Errorf("members = %v", got[0].MemberNames)
	}
}

func TestIOFXML_EmptyBodyError(t *testing.T) {
	_, err := New().Parse(strings.NewReader(""))
	if err == nil {
		t.Fatalf("expected error on empty body")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
