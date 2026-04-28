package roster

import (
	"bufio"
	"encoding/csv"
	"io"
	"strings"
)

// CSVImporter accepts the spreadsheet shape arrangers actually have:
// one row per team, first cell = team name, remaining cells = member
// names. Empty cells and trailing blank rows are skipped silently so
// a roster pasted out of Excel still works.
//
// A leading UTF-8 BOM (Excel's default for "Save As CSV UTF-8") is
// trimmed; otherwise it would land as part of the first team's name
// and break uniqueness checks across imports.
type CSVImporter struct{}

// Parse reads RFC 4180 records with ragged-row tolerance. Field
// counts vary by team size, so FieldsPerRecord = -1 is required;
// fixing it would force every team to pad to the longest roster.
func (CSVImporter) Parse(r io.Reader) ([]TeamInput, error) {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && len(b) == 3 &&
		b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	var out []TeamInput
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) == 0 {
			continue
		}
		name := strings.TrimSpace(rec[0])
		if name == "" {
			continue
		}
		members := make([]string, 0, len(rec)-1)
		for _, m := range rec[1:] {
			m = strings.TrimSpace(m)
			if m != "" {
				members = append(members, m)
			}
		}
		out = append(out, TeamInput{Name: name, MemberNames: members})
	}
	return out, nil
}
