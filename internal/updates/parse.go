// Package updates fetches and parses the project's CHANGELOG.md so
// the running binary can detect newer releases and surface release
// notes in the admin UI.
//
// The format follows Keep a Changelog 1.1.0
// (https://keepachangelog.com/en/1.1.0/). Versions live under H2
// headings shaped like `## [X.Y.Z] - YYYY-MM-DD`. The `[Unreleased]`
// section, when present, is recognised but excluded from the
// ordered Release list — operators only care about cut releases.
package updates

import (
	"bufio"
	"strings"
	"time"
)

// Release is one section of the changelog: a version with the date
// it was released and the markdown notes that appeared underneath
// the H2 heading (everything up to the next H2). Notes are kept as
// raw markdown so the UI can render them with the same goldmark
// pipeline that handles puzzle prompts.
type Release struct {
	Version Semver
	Date    time.Time // zero when the heading omitted the date
	NotesMD string
}

// Changelog is the parsed view of a CHANGELOG.md file. Releases are
// ordered newest-first by their parsed Version — this is the
// presentation order both for the API response and for the UI.
type Changelog struct {
	Releases []Release
}

// Latest returns the highest-versioned Release, or ok=false on an
// empty changelog. Convenience around Releases[0] for callers that
// don't want to special-case the empty slice.
func (c Changelog) Latest() (Release, bool) {
	if len(c.Releases) == 0 {
		return Release{}, false
	}
	return c.Releases[0], true
}

// SectionsNewerThan returns the releases strictly greater than
// current, preserving the newest-first ordering. Used to build the
// "what changed since you upgraded" list shown in the UI.
func (c Changelog) SectionsNewerThan(current Semver) []Release {
	out := make([]Release, 0, len(c.Releases))
	for _, r := range c.Releases {
		if r.Version.Compare(current) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Parse reads a CHANGELOG.md body and extracts version sections.
// Anything outside a recognised version heading (the file preamble,
// the `## [Unreleased]` block, link-reference definitions at the
// bottom) is silently dropped. Malformed headings are skipped, not
// fatal — partial recovery beats failing the whole feed because one
// future entry has a typo.
func Parse(src string) Changelog {
	var (
		out      Changelog
		cur      *Release
		curNotes strings.Builder
	)
	flush := func() {
		if cur == nil {
			return
		}
		cur.NotesMD = strings.TrimSpace(curNotes.String())
		out.Releases = append(out.Releases, *cur)
		cur = nil
		curNotes.Reset()
	}

	sc := bufio.NewScanner(strings.NewReader(src))
	// 1 MiB buffer — the largest plausible changelog is well under this
	// and the parser is bounded by the fetcher's response cap anyway.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if v, date, ok := parseVersionHeading(line); ok {
			flush()
			cur = &Release{Version: v, Date: date}
			continue
		}
		if isUnreleasedHeading(line) {
			// Skip the in-progress section. We don't preserve its
			// notes — release-notes UIs that show "what's coming" are
			// a separate feature; today's surface is "what changed in
			// versions newer than mine".
			flush()
			cur = nil
			continue
		}
		if cur != nil {
			curNotes.WriteString(line)
			curNotes.WriteByte('\n')
		}
	}
	flush()

	// Sort newest-first. The keep-a-changelog convention is to write
	// in this order already, but a malformed file might not — we'd
	// rather sort defensively than display 0.1.0 above 1.0.0.
	sortReleasesDesc(out.Releases)
	return out
}

func sortReleasesDesc(rs []Release) {
	// Simple insertion sort — release lists are tiny (single digits
	// to low double digits) so the import-graph cost of pulling in
	// "sort" isn't worth it.
	for i := 1; i < len(rs); i++ {
		j := i
		for j > 0 && rs[j-1].Version.Compare(rs[j].Version) < 0 {
			rs[j-1], rs[j] = rs[j], rs[j-1]
			j--
		}
	}
}

// parseVersionHeading matches `## [X.Y.Z] - YYYY-MM-DD` (the date
// portion is optional; some keep-a-changelog authors omit it for
// not-yet-tagged drafts). Returns ok=false on anything else,
// including the `## [Unreleased]` heading which is_unreleased's job.
func parseVersionHeading(line string) (Semver, time.Time, bool) {
	rest, ok := strings.CutPrefix(line, "## ")
	if !ok {
		return Semver{}, time.Time{}, false
	}
	rest = strings.TrimSpace(rest)
	// Bracketed version: `[1.2.3]` or `[v1.2.3]`. Optional, so a
	// changelog using `## 1.2.3 - 2026-01-01` (rare but seen in the
	// wild) parses too.
	var versionTok, dateTok string
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return Semver{}, time.Time{}, false
		}
		versionTok = rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
	} else {
		// Bare-version form: split on first whitespace.
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			versionTok = rest[:i]
			rest = strings.TrimSpace(rest[i:])
		} else {
			versionTok = rest
			rest = ""
		}
	}
	v, ok := ParseSemver(versionTok)
	if !ok {
		return Semver{}, time.Time{}, false
	}
	// Optional " - YYYY-MM-DD" trailer. Tolerate em / en dashes too —
	// keep-a-changelog uses ASCII hyphen, but the doc has copy-pasted
	// in fancier dashes before.
	rest = strings.TrimLeft(rest, "-–— \t")
	dateTok = strings.TrimSpace(rest)
	var date time.Time
	if dateTok != "" {
		// Drop trailing parenthetical notes ("(YANKED)" etc.) — keep
		// the leading date token only.
		if i := strings.IndexAny(dateTok, " \t"); i >= 0 {
			dateTok = dateTok[:i]
		}
		if d, err := time.Parse("2006-01-02", dateTok); err == nil {
			date = d
		}
	}
	return v, date, true
}

// isUnreleasedHeading recognises the `## [Unreleased]` (and bare
// `## Unreleased`) heading regardless of trailing whitespace.
func isUnreleasedHeading(line string) bool {
	rest, ok := strings.CutPrefix(line, "## ")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "[")
	rest = strings.TrimSuffix(rest, "]")
	return strings.EqualFold(strings.TrimSpace(rest), "Unreleased")
}
