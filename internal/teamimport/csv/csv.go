// Package csv is the CSV (.csv, comma-separated) importer for the
// teamimport registry. Self-contained — depends only on the
// stdlib's encoding/csv plus the parent package's Team /
// Importer types.
//
// # Format
//
// CSV is informally specified by RFC 4180:
//
//	https://datatracker.ietf.org/doc/html/rfc4180
//
// In practice every spreadsheet (Excel, Google Sheets, Numbers,
// LibreOffice Calc) and every event-registration platform exports
// to it. Comma-separated, UTF-8, double-quote escaping for cells
// containing commas / quotes / newlines — encoding/csv covers
// the entire grammar.
//
// # Wire shape
//
//	Alpha,alice,bob,carol
//	Beta,dave,ezra
//
// First column is the team name, every column after it becomes a
// player slot. Empty rows are skipped silently. A first row whose
// first cell is a header keyword ("team", "name", and Swedish
// counterparts) is treated as a header and ignored — that lets
// people export from Excel / Google Sheets without stripping
// their header row first.
package csv

import (
	"encoding/csv"
	"io"
	"strings"

	"github.com/klahr/zonventure/internal/teamimport"
)

// New returns the CSV importer. No knobs — Excel / Sheets / numbers
// all default to comma-separated UTF-8, which is what
// encoding/csv reads out of the box.
func New() teamimport.Importer { return &impl{} }

type impl struct{}

func (impl) Name() string         { return "CSV" }
func (impl) Extensions() []string { return []string{".csv"} }
func (impl) MIMETypes() []string {
	// text/csv is the RFC 4180 type. application/csv shows up from
	// some Microsoft exports; application/vnd.ms-excel is what
	// older Excel writes when you save-as-CSV. Claim all three so
	// the dispatch picks us up regardless of which tab the user
	// went through.
	return []string{
		"text/csv",
		"application/csv",
		"application/vnd.ms-excel",
	}
}

func (impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // ragged rows: each team can have a different player count
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	return parseRows(rows), nil
}

// parseRows is shared between CSV and TSV-shaped inputs (an
// internal helper would let us avoid duplicating it across the
// two packages, but the two-line saving wasn't worth the cross-
// package coupling — see the package doc on independence).
//
// Skips fully empty rows, drops a leading header row when its
// first cell looks like a column label rather than a team name.
func parseRows(rows [][]string) []teamimport.Team {
	if len(rows) == 0 {
		return nil
	}
	if isHeader(rows[0]) {
		rows = rows[1:]
	}
	out := make([]teamimport.Team, 0, len(rows))
	for _, row := range rows {
		// Drop trimmed-empty cells from the right so a trailing
		// comma doesn't manifest as an empty player slot.
		for i := len(row) - 1; i >= 0; i-- {
			if strings.TrimSpace(row[i]) == "" {
				row = row[:i]
			} else {
				break
			}
		}
		if len(row) == 0 {
			continue
		}
		name := strings.TrimSpace(row[0])
		if name == "" {
			continue
		}
		// CSV-injection defang: names + member names round-trip through
		// arranger CSV exports / clipboard flows; a leading =/+/-/@ is
		// a formula in Excel/Sheets/Numbers when the spreadsheet sniffs
		// the cell. Prefix with ' so the value renders as text. See
		// teamimport.SanitizeCellValue for the OWASP-shaped helper.
		name = teamimport.SanitizeCellValue(name)
		members := make([]string, 0, len(row)-1)
		for _, m := range row[1:] {
			m = strings.TrimSpace(m)
			if m != "" {
				members = append(members, teamimport.SanitizeCellValue(m))
			}
		}
		out = append(out, teamimport.Team{Name: name, MemberNames: members})
	}
	return out
}

// isHeader reports whether the first row of a sheet is a column-
// label row that should be skipped. Conservative — only the
// obvious cases. A real first-cell team name like "Headers" is
// vanishingly unlikely; if it ever happens the user can
// rename the team after import.
func isHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(row[0]))
	switch first {
	case "team", "team name", "name", "lag", "lagnamn":
		return true
	}
	return false
}
