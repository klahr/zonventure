// Package ods is the OpenDocument Spreadsheet importer for the
// teamimport registry. ODS files are zip archives whose
// content.xml carries the cell data — same conceptual model as
// xlsx but with a simpler XML schema, no shared-strings
// indirection, no styles to dereference. Stdlib only: archive/zip
// + encoding/xml.
//
// # Format
//
// OpenDocument Format for Office Applications, version 1.3
// (OASIS standard, ratified as ISO/IEC 26300):
//
//	https://docs.oasis-open.org/office/OpenDocument/v1.3/os/OpenDocument-v1.3-os.html
//
// LibreOffice Calc and OpenOffice Calc are the reference writers.
// Apple Numbers and Excel both export to ODS as a non-default
// option. The spreadsheet schema we read here is the
// `urn:oasis:names:tc:opendocument:xmlns:table:1.0` namespace:
//
//	https://docs.oasis-open.org/office/OpenDocument/v1.3/os/part3-schema/OpenDocument-v1.3-os-part3-schema.html
//
// # Wire shape
//
// Only the first table (sheet) is read; other tables are
// ignored. Within that table the row shape mirrors the CSV
// importer: first column = team name, every column after it
// becomes a player slot. Empty rows skipped, header-row keyword
// detected and dropped.
package ods

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"

	"git.sr.ht/~klahr/zv/internal/teamimport"
)

// New returns the ODS importer.
func New() teamimport.Importer { return &impl{} }

type impl struct{}

func (impl) Name() string         { return "ODS" }
func (impl) Extensions() []string { return []string{".ods"} }
func (impl) MIMETypes() []string {
	return []string{
		"application/vnd.oasis.opendocument.spreadsheet",
		// Some browsers / proxies write the .fods (flat ODS) MIME but
		// any zip-wrapped ODS lands here too — we accept the same
		// type and the unzip step will fail cleanly on a flat-XML
		// payload.
		"application/x-vnd.oasis.opendocument.spreadsheet",
	}
}

// odsRow is the deserialized shape of one <table:table-row> from
// content.xml. table:number-rows-repeated lets a writer collapse N
// identical empty rows into a single element with a count
// attribute; we read the count so a sheet with a sparse first
// table doesn't blow memory expanding rows we'd skip anyway.
//
// We match by local name only and ignore the OASIS namespace
// prefix — encoding/xml does the right thing when the tag has no
// namespace URL, and writing the full
// urn:oasis:names:tc:opendocument:xmlns:table:1.0 in every tag
// just clutters the struct.
type odsRow struct {
	RepeatedCount int       `xml:"number-rows-repeated,attr"`
	Cells         []odsCell `xml:"table-cell"`
}

// odsCell carries the cell value via either the value-type
// attribute (numbers, dates) or the contained <text:p> children
// (string cells). table:number-columns-repeated handles the same
// run-length collapse as on rows.
type odsCell struct {
	RepeatedCount int      `xml:"number-columns-repeated,attr"`
	ValueType     string   `xml:"value-type,attr"`
	StringValue   string   `xml:"string-value,attr"`
	NumberValue   string   `xml:"value,attr"`
	Texts         []string `xml:"p"`
}

func (impl) Parse(r io.Reader) ([]teamimport.Team, error) {
	// archive/zip needs a ReaderAt + size, so buffer the whole
	// upload first. ODS rosters are tiny — a 5 MiB cap upstream in
	// the handler is the size budget we live within here.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var content *zip.File
	for _, f := range zr.File {
		if f.Name == "content.xml" {
			content = f
			break
		}
	}
	if content == nil {
		return nil, errors.New("teamimport/ods: no content.xml in archive")
	}
	rc, err := content.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return parseContent(rc)
}

// parseContent walks content.xml as a stream — easier on memory
// than full Unmarshal, and lets us bail at end-of-first-table
// without parsing the rest of the document.
func parseContent(r io.Reader) ([]teamimport.Team, error) {
	dec := xml.NewDecoder(r)
	var (
		rows         [][]string
		insideTable  bool
		seenAnyTable bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "table": // table:table — one sheet
			if seenAnyTable {
				// Skip subsequent sheets without parsing them.
				_ = dec.Skip()
				continue
			}
			seenAnyTable = true
			insideTable = true
		case "table-row":
			if !insideTable {
				_ = dec.Skip()
				continue
			}
			var row odsRow
			if err := dec.DecodeElement(&row, &start); err != nil {
				return nil, err
			}
			cells := flattenCells(row.Cells)
			repeats := row.RepeatedCount
			if repeats <= 0 {
				repeats = 1
			}
			// Sparse-row collapse: a writer might encode "rows 5–999
			// are empty" as a single repeated row with no cells.
			// Replaying that 995 times would balloon the parsed
			// rows slice for nothing — skip empty repeats.
			if isAllEmpty(cells) {
				continue
			}
			for i := 0; i < repeats; i++ {
				rows = append(rows, cells)
			}
		}
	}
	return shapeRows(rows), nil
}

// flattenCells expands repeated-cell run lengths into a flat slice
// of strings. Non-string cells (numbers, dates) fall back to the
// office:value attribute so a numeric ID column parses as a name
// rather than disappearing.
func flattenCells(cells []odsCell) []string {
	var out []string
	for _, c := range cells {
		text := cellText(c)
		repeats := c.RepeatedCount
		if repeats <= 0 {
			repeats = 1
		}
		// A run of empty repeated cells at the row-end is a writer
		// convention for "this row has no more data" — preserving
		// it would just produce trailing empty member names. Trim
		// at row-shape time, but leave interior repeats so a row
		// with [name, "", "", member3] keeps its column alignment.
		for i := 0; i < repeats; i++ {
			out = append(out, text)
		}
	}
	return out
}

func cellText(c odsCell) string {
	if c.StringValue != "" {
		return c.StringValue
	}
	if len(c.Texts) > 0 {
		return strings.TrimSpace(strings.Join(c.Texts, " "))
	}
	if c.NumberValue != "" {
		// Strip the trailing ".0" you get when a writer encodes a
		// whole number as "42.0" — the trimmed form reads as an ID.
		if f, err := strconv.ParseFloat(c.NumberValue, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return c.NumberValue
	}
	return ""
}

func isAllEmpty(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// shapeRows is the same row-to-Team transform CSV uses — kept
// inline rather than imported across packages so the importer is
// truly self-contained, the cost being a few duplicated lines.
func shapeRows(rows [][]string) []teamimport.Team {
	if len(rows) == 0 {
		return nil
	}
	if isHeader(rows[0]) {
		rows = rows[1:]
	}
	out := make([]teamimport.Team, 0, len(rows))
	for _, row := range rows {
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
		members := make([]string, 0, len(row)-1)
		for _, m := range row[1:] {
			m = strings.TrimSpace(m)
			if m != "" {
				members = append(members, m)
			}
		}
		out = append(out, teamimport.Team{Name: name, MemberNames: members})
	}
	return out
}

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
