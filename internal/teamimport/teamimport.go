// Package teamimport defines a small, format-agnostic surface for
// turning an uploaded file into a list of (team-name, members)
// records — exactly the shape the existing /teams/bulk endpoint
// already accepts. Each file format is implemented by a separate
// sub-package so an operator can compose only the formats they
// actually want to expose; no init-time side effects, no implicit
// global registry.
//
// Wiring shape at the call site:
//
//	reg := teamimport.New()
//	reg.Register(csv.New())
//	reg.Register(tsv.New())
//	reg.Register(jsonimp.New())
//	reg.Register(text.New())
//	// optional, requires excelize:
//	// reg.Register(xlsx.New())
//
// The HTTP handler then dispatches a multipart upload through
// reg.ForFilename(hdr.Filename) (with a MIME-type fallback) and
// returns the parsed Team list as a preview. Actual team creation
// stays on /teams/bulk so we don't fork the validation / dedup /
// audit path.
package teamimport

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

// Team is a single parsed entry from an import. MemberNames is in
// authoring order — the bulk endpoint mints one slot per name in
// that order, so the importer's job is to preserve whatever the
// source file expressed without re-ordering.
type Team struct {
	Name        string   `json:"name"`
	MemberNames []string `json:"member_names"`
	// Email is the team's email, from the sheet's email column.
	Email string `json:"email,omitempty"`
}

// SanitizeCellValue defangs a single string against CSV-injection.
// A cell whose first non-whitespace character is one of `=`, `+`,
// `-`, `@`, `\t`, `\r`, `|` is treated as a formula by Excel /
// Sheets / Numbers when re-exported via the clipboard or
// downloaded as CSV — and our team names round-trip through
// arranger CSV exports / leaderboard copy-paste flows. Prefix
// such cells with a single quote so the resulting SQL row
// preserves the original characters but Excel renders them as
// text instead of evaluating.
//
// Applies once at import time. Empty / safe values pass through
// unchanged so a normal "Alpha, alice, bob" row stays exactly as
// the operator typed it. The single-quote prefix is the OWASP-
// recommended mitigation: it doesn't change the visible content
// in spreadsheet clients but disables formula evaluation.
func SanitizeCellValue(s string) string {
	trimmed := strings.TrimLeft(s, " \t")
	if trimmed == "" {
		return s
	}
	switch trimmed[0] {
	case '=', '+', '-', '@', '\t', '\r', '|':
		return "'" + s
	}
	return s
}

// headerLabels are column labels a sheet's header row starts with, in
// English and Swedish: a team or group column, or a sign-up sheet's
// player columns.
var headerLabels = map[string]bool{
	"team": true, "team name": true, "name": true, "group": true,
	"first name": true, "firstname": true, "last name": true, "surname": true,
	"lag": true, "lagnamn": true, "grupp": true, "namn": true,
	"förnamn": true, "efternamn": true,
}

// IsHeader reports whether the first row of a sheet is a column-label
// row that should be skipped: its first cell is a known label, or a
// cell names an email column ("E-post", "Förälders e-post", "Email")
// without being an address itself. Conservative — a real first-cell
// team name like "Team" is vanishingly unlikely; if it ever happens the
// user can rename the team after import.
func IsHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	if headerLabels[strings.ToLower(strings.TrimSpace(row[0]))] {
		return true
	}
	for _, c := range row {
		c = strings.ToLower(c)
		if !strings.Contains(c, "@") && (strings.Contains(c, "e-post") || strings.Contains(c, "email") || strings.Contains(c, "e-mail")) {
			return true
		}
	}
	return false
}

// SkippedRows is the error a sheet importer's Parse returns along with
// the teams it did read when some rows with content gave no team: no
// team name, such as a formula cell the file holds no value for. Rows
// are the sheet's 1-based row numbers, for the import preview to name.
type SkippedRows struct{ Rows []int }

func (e *SkippedRows) Error() string {
	return fmt.Sprintf("%d rows without a team name were skipped", len(e.Rows))
}

// groupLabels name a class list's team column; firstLabels, lastLabels
// and nameLabels its player-name columns.
var (
	groupLabels = map[string]bool{"group": true, "grupp": true, "team": true, "team name": true, "lag": true, "lagnamn": true}
	firstLabels = map[string]bool{"first name": true, "firstname": true, "förnamn": true}
	lastLabels  = map[string]bool{"last name": true, "lastname": true, "surname": true, "efternamn": true}
	nameLabels  = map[string]bool{"name": true, "namn": true}
)

// classList finds a class list's columns in a header row: a group (team)
// column plus a first and/or last name column, or a single name column.
// ok is false for any other sheet, such as the team-per-row format.
func classList(header []string) (group, first, last, name int, ok bool) {
	group, first, last, name = -1, -1, -1, -1
	names := 0
	for i, c := range header {
		switch c = strings.ToLower(strings.TrimSpace(c)); {
		case groupLabels[c] && group < 0:
			group = i
		case firstLabels[c] && first < 0:
			first = i
		case lastLabels[c] && last < 0:
			last = i
		case nameLabels[c]:
			name = i
			names++
		}
	}
	return group, first, last, name, group >= 0 && (first >= 0 || last >= 0 || names == 1)
}

// Column roles: what an import made of a sheet's column. A class list's
// first, last and name columns are all RolePlayer, together the
// player's name.
const (
	RoleTeam    = "team"
	RolePlayer  = "player"
	RoleEmail   = "email"
	RoleIgnored = "ignored"
)

// Column is one of a sheet's columns as an import read it: its header
// label ("" when the sheet has no header row) and its role.
type Column struct {
	Label string `json:"label"`
	Role  string `json:"role"`
}

// Sheet is what Read makes of a sheet: its teams and its columns.
type Sheet struct {
	Teams   []Team
	Columns []Column
}

// emailLabel reports whether a header cell names an email column.
func emailLabel(c string) bool {
	c = strings.ToLower(c)
	return !strings.Contains(c, "@") && (strings.Contains(c, "e-post") || strings.Contains(c, "epost") || strings.Contains(c, "email") || strings.Contains(c, "e-mail"))
}

// parentLabel reports whether a header cell names a parent's or
// guardian's column. Those addresses are never imported: a team's email
// gets the game's invitations, and a child's parent has not agreed to
// that.
func parentLabel(c string) bool {
	c = strings.ToLower(c)
	for _, w := range []string{"förälder", "föräldr", "vårdnadshavare", "målsman", "parent", "guardian"} {
		if strings.Contains(c, w) {
			return true
		}
	}
	return false
}

// Rows turns a sheet's rows into teams; see Read.
func Rows(rows [][]string) ([]Team, error) {
	s, err := Read(rows)
	return s.Teams, err
}

// Read turns a sheet's rows into teams. A class list (a header row with
// a Group/Lag/Team column and First/Last name columns) groups its rows
// by the group column, each row a player named first + last. Any other
// sheet is one team per row: the first column is the team name, the
// rest its players. Either way a header's email column ("E-post",
// "Email") gives the team's email, a class list's team the first address
// among its rows; a parent's email column is never read. A header row
// is skipped, empty rows too; rows with content but no team come back
// as *SkippedRows with the teams read. Cells are defused with
// SanitizeCellValue. Columns says what became of each column, so the
// preview can show those it ignored.
func Read(rows [][]string) (Sheet, error) {
	if len(rows) == 0 {
		return Sheet{}, nil
	}
	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	var (
		out     []Team
		skipped []int
		cols    []Column
	)
	// email finds a header's email column, -1 for none.
	email := func(header []string, taken ...int) int {
		for i, c := range header {
			if emailLabel(c) && !parentLabel(c) && !slices.Contains(taken, i) {
				return i
			}
		}
		return -1
	}
	if group, first, last, name, ok := classList(rows[0]); ok {
		mail := email(rows[0], group, first, last, name)
		for i, c := range rows[0] {
			role := RoleIgnored
			switch {
			case i == group:
				role = RoleTeam
			case i == first || i == last || i == name:
				role = RolePlayer
			case i == mail:
				role = RoleEmail
			}
			cols = append(cols, Column{Label: strings.TrimSpace(c), Role: role})
		}
		at := map[string]int{}
		for n, row := range rows[1:] {
			team := cell(row, group)
			var parts []string
			for _, i := range []int{first, last, name} {
				if v := cell(row, i); v != "" {
					parts = append(parts, v)
				}
			}
			if team == "" || len(parts) == 0 {
				if strings.TrimSpace(strings.Join(row, "")) != "" {
					skipped = append(skipped, n+2)
				}
				continue
			}
			team = SanitizeCellValue(team)
			i, seen := at[team]
			if !seen {
				i = len(out)
				at[team] = i
				out = append(out, Team{Name: team, MemberNames: []string{}})
			}
			out[i].MemberNames = append(out[i].MemberNames, SanitizeCellValue(strings.Join(parts, " ")))
			if out[i].Email == "" {
				out[i].Email = cell(row, mail)
			}
		}
	} else {
		start, mail, width := 0, -1, 0
		ignored := map[int]bool{}
		if IsHeader(rows[0]) {
			start, width = 1, len(rows[0])
			mail = email(rows[0], 0)
			// A parent's address, or a second email column, is no player.
			for i, c := range rows[0] {
				if i > 0 && i != mail && (parentLabel(c) || emailLabel(c)) {
					ignored[i] = true
				}
			}
		}
		for n, row := range rows[start:] {
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
			width = max(width, len(row))
			name := strings.TrimSpace(row[0])
			if name == "" {
				skipped = append(skipped, start+n+1)
				continue
			}
			members := make([]string, 0, len(row)-1)
			for i, m := range row {
				if i == 0 || i == mail || ignored[i] {
					continue
				}
				if m = strings.TrimSpace(m); m != "" {
					members = append(members, SanitizeCellValue(m))
				}
			}
			out = append(out, Team{Name: SanitizeCellValue(name), MemberNames: members, Email: cell(row, mail)})
		}
		for i := range width {
			var label string
			if start == 1 {
				label = cell(rows[0], i)
			}
			role := RolePlayer
			switch {
			case i == 0:
				role = RoleTeam
			case i == mail:
				role = RoleEmail
			case ignored[i]:
				role = RoleIgnored
			}
			cols = append(cols, Column{Label: label, Role: role})
		}
	}
	if len(skipped) > 0 {
		return Sheet{Teams: out, Columns: cols}, &SkippedRows{Rows: skipped}
	}
	return Sheet{Teams: out, Columns: cols}, nil
}

// SheetImporter is an Importer of a spreadsheet format that can hand
// over the sheet's rows, for Read to say what became of each column.
type SheetImporter interface {
	Importer
	ReadRows(r io.Reader) ([][]string, error)
}

// Importer is the contract every format implements. It's small on
// purpose: callers only need to identify (Name + Extensions +
// MIMETypes) and parse. There is no "validate" or "preview" step —
// Parse is allowed to return a partial list along with an error
// when some rows are bad and others are usable, but the simple
// happy path is to return (teams, nil) and let the bulk endpoint
// do per-team validation when the user confirms.
type Importer interface {
	// Name is the human-readable label shown in the upload UI.
	// Translatable on the front-end via the existing t() pipeline.
	Name() string
	// Extensions are the filename extensions this importer claims
	// (each leading-dot, lowercase, e.g. ".csv"). The first entry
	// is the canonical / preferred form for documentation.
	Extensions() []string
	// MIMETypes are the Content-Type values this importer claims.
	// Used as a fallback when the filename has no extension or
	// the browser sent a generic application/octet-stream.
	MIMETypes() []string
	// Parse reads from r and returns parsed teams. The reader is
	// owned by the caller — implementations should not close it.
	Parse(r io.Reader) ([]Team, error)
}

// Registry resolves a filename or MIME type to an Importer. There
// is no implicit global instance: every server creates its own
// Registry and registers exactly the importers it wants — this is
// the "plugin-like, independent" property. Registration is
// expected to happen at startup; the Registry is goroutine-safe
// for concurrent reads but Register is not — call it before
// handing the registry to HTTP handlers.
type Registry struct {
	byExt  map[string]Importer
	byMIME map[string]Importer
	all    []Importer
}

// New returns an empty Registry. Register importers before use.
func New() *Registry {
	return &Registry{
		byExt:  map[string]Importer{},
		byMIME: map[string]Importer{},
	}
}

// ErrUnknownFormat is returned by ForFilename / ForMIME when no
// registered importer claims the given identifier. The handler
// translates this to a 415 Unsupported Media Type so the UI can
// surface "we don't know how to read .foo files" cleanly.
var ErrUnknownFormat = errors.New("teamimport: no importer registered for this format")

// Register adds an importer. The same importer can claim multiple
// extensions and MIME types — the dispatch tables keep one entry
// per claim. A second importer claiming an already-registered
// extension overwrites the first (last-registered wins) so a
// caller can deliberately substitute, e.g. swap the stdlib CSV
// importer for a stricter one.
func (r *Registry) Register(imp Importer) {
	r.all = append(r.all, imp)
	for _, ext := range imp.Extensions() {
		r.byExt[strings.ToLower(ext)] = imp
	}
	for _, mt := range imp.MIMETypes() {
		r.byMIME[strings.ToLower(mt)] = imp
	}
}

// All returns every registered importer in registration order. The
// front-end uses this to render the "supported formats" hint
// alongside the file picker.
func (r *Registry) All() []Importer {
	out := make([]Importer, len(r.all))
	copy(out, r.all)
	return out
}

// ForFilename resolves an importer by filename extension. Empty
// filenames and unknown extensions return ErrUnknownFormat — the
// caller should fall back to ForMIME before giving up.
func (r *Registry) ForFilename(name string) (Importer, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return nil, ErrUnknownFormat
	}
	if imp, ok := r.byExt[ext]; ok {
		return imp, nil
	}
	return nil, ErrUnknownFormat
}

// ForMIME resolves an importer by Content-Type. The lookup is
// case-insensitive on the type (which is what RFC 2045 specifies)
// and ignores any ;charset= tail — text/csv;charset=utf-8 still
// matches a registered text/csv. Returns ErrUnknownFormat on miss.
func (r *Registry) ForMIME(mime string) (Importer, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == "" {
		return nil, ErrUnknownFormat
	}
	if imp, ok := r.byMIME[mime]; ok {
		return imp, nil
	}
	return nil, ErrUnknownFormat
}

// Resolve combines ForFilename and ForMIME — extension first
// (browsers reliably send Content-Type but it's frequently the
// generic application/octet-stream for non-Office files; the
// filename extension is more specific). Falls back to MIME on
// extension miss. Returns ErrUnknownFormat only when neither
// matches.
func (r *Registry) Resolve(filename, mime string) (Importer, error) {
	if imp, err := r.ForFilename(filename); err == nil {
		return imp, nil
	}
	return r.ForMIME(mime)
}
