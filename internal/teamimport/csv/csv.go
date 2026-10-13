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
// player slot. Empty rows are skipped silently. A first row of
// column labels (teamimport.IsHeader: "team", "name", an email
// column, and Swedish counterparts) is treated as a header and
// ignored — that lets
// people export from Excel / Google Sheets without stripping
// their header row first.
//
// A class list (header First name / Last name / Group, or Swedish
// Förnamn / Efternamn / Grupp / Lag) is grouped by its group column
// instead, one player per row; see teamimport.Rows.
package csv

import (
	"encoding/csv"
	"io"

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

func (i impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	rows, err := i.ReadRows(r)
	if err != nil {
		return nil, err
	}
	return teamimport.Rows(rows)
}

// ReadRows reads the file's rows.
func (impl) ReadRows(r io.Reader) ([][]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // ragged rows: each team can have a different player count
	cr.TrimLeadingSpace = true
	return cr.ReadAll()
}
