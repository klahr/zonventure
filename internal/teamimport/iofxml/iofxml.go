// Package iofxml is the IOF XML 3.0 importer for the teamimport
// registry. The International Orienteering Federation's data
// standard is what Eventor and similar registration systems
// export, so an arranger running an orienteering-style adventure
// can drop their existing relay startlist straight into Zonventure
// without re-typing rosters.
//
// # Format
//
// IOF Data Standard 3.0, maintained by the International
// Orienteering Federation. Schemas, examples, and changelog:
//
//	https://github.com/international-orienteering-federation/datastandard-v3
//	https://orienteering.sport/iof/it/data-standard-3-0/
//
// Eventor (the IOF's reference event-management system) exports
// in this format, as do most of the country-level orienteering
// registration platforms (Sweden's Eventor, Norway's Eventor,
// the UK's Routegadget pipeline, etc.).
//
// Two top-level shapes from the spec are accepted:
//
//	<EntryList>      — registration data (TeamEntry → Person...)
//	<StartList>      — the actual start grid (TeamStart → Person...)
//
// Both encode the same conceptual model: a TeamEntry / TeamStart
// container with a <Name>, plus N <TeamEntryPerson> /
// <TeamMemberStart> children each carrying a <Person> with a
// <Name><Given>+<Family></Name>. The decoder picks whichever
// container the document uses; mixed documents are accepted (we
// concatenate the two lists).
//
// Stdlib only — encoding/xml.
package iofxml

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"git.sr.ht/~klahr/zv/internal/teamimport"
)

// New returns the IOF XML importer.
func New() teamimport.Importer { return &impl{} }

type impl struct{}

func (impl) Name() string         { return "IOF XML" }
func (impl) Extensions() []string { return []string{".xml"} }
func (impl) MIMETypes() []string {
	// application/xml is the IANA-preferred type; text/xml shows up
	// from older exporters. Both legitimate for IOF documents.
	return []string{"application/xml", "text/xml"}
}

// document is the top-level wrapper. We accept either of the two
// IOF root elements; whichever one carries data wins. EntryList
// and StartList are mutually exclusive in practice but we don't
// enforce that.
type document struct {
	XMLName     xml.Name
	TeamEntries []teamContainer `xml:"TeamEntry"`
	TeamStarts  []teamContainer `xml:"TeamStart"`
}

// teamContainer covers both <TeamEntry> and <TeamStart>: same
// inner shape (a Name, a list of Person-bearing rows) so one
// struct serves both. The XML element names for the per-person
// rows differ — TeamEntryPerson vs TeamMemberStart — so we look
// for both and merge.
type teamContainer struct {
	Name             string         `xml:"Name"`
	TeamEntryPersons []personHolder `xml:"TeamEntryPerson"`
	TeamMemberStarts []personHolder `xml:"TeamMemberStart"`
}

func (t teamContainer) members() []string {
	rows := append([]personHolder{}, t.TeamEntryPersons...)
	rows = append(rows, t.TeamMemberStarts...)
	out := make([]string, 0, len(rows))
	for _, p := range rows {
		if name := p.fullName(); name != "" {
			// CSV-injection defang on the round-trip path — same
			// rationale as the other importers; see
			// teamimport.SanitizeCellValue.
			out = append(out, teamimport.SanitizeCellValue(name))
		}
	}
	return out
}

// personHolder unwraps the <Person> nesting that both
// TeamEntryPerson and TeamMemberStart share.
type personHolder struct {
	Person personRow `xml:"Person"`
}

func (p personHolder) fullName() string { return p.Person.fullName() }

// personRow is the IOF Person element. <Name> is itself an
// element with <Given> and <Family> children — Family is the
// surname. We render "Given Family" so the player slot reads as
// the orienteer's everyday name.
type personRow struct {
	Name struct {
		Given  string `xml:"Given"`
		Family string `xml:"Family"`
	} `xml:"Name"`
}

func (p personRow) fullName() string {
	g := strings.TrimSpace(p.Name.Given)
	f := strings.TrimSpace(p.Name.Family)
	switch {
	case g != "" && f != "":
		return g + " " + f
	case g != "":
		return g
	case f != "":
		return f
	}
	return ""
}

func (impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("teamimport/iofxml: empty body")
	}
	var doc document
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	containers := append([]teamContainer{}, doc.TeamEntries...)
	containers = append(containers, doc.TeamStarts...)
	out := make([]teamimport.Team, 0, len(containers))
	for _, tc := range containers {
		name := strings.TrimSpace(tc.Name)
		if name == "" {
			continue
		}
		out = append(out, teamimport.Team{
			Name:        teamimport.SanitizeCellValue(name),
			MemberNames: tc.members(),
		})
	}
	return out, nil
}
