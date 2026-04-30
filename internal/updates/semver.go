package updates

import (
	"strconv"
	"strings"
)

// Semver is the minimal MAJOR.MINOR.PATCH triple we need for ordering
// changelog entries. Pre-release tags and build metadata (the bits
// after `-` and `+` in full SemVer) are recorded as a Suffix string
// for display but never participate in the ordering comparison.
//
// The package isn't a full SemVer implementation — we only need to
// answer "is the latest changelog version greater than the version
// the running binary reports?" and to surface a sensible label when
// the running binary is past a tag (`git describe` produces formats
// like `0.1.0-3-gabcd123` for commits past a tag, which we want to
// treat as "ahead of 0.1.0", not "0.1.0 with pre-release tag").
type Semver struct {
	Major, Minor, Patch int
	// Suffix preserves anything after MAJOR.MINOR.PATCH (everything
	// from the first '-' or '+' onward, including the separator).
	// Empty for a clean tag like "0.1.0".
	Suffix string
}

// ParseSemver accepts strings in the shapes the project actually
// emits: clean tags ("0.1.0", "v1.2.3"), `git describe` output past
// a tag ("0.1.0-3-gabcd123"), and the optional `-dirty` suffix on
// uncommitted builds. Returns ok=false for anything else (including
// the literal "dev").
func ParseSemver(s string) (Semver, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return Semver{}, false
	}
	// Snip off the suffix (pre-release / build metadata / git-describe
	// trailer) on the first separator. The ordering only needs the
	// numeric triple in front.
	core, suffix := s, ""
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		core, suffix = s[:i], s[i:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}
	out := Semver{Suffix: suffix}
	var err error
	if out.Major, err = strconv.Atoi(parts[0]); err != nil {
		return Semver{}, false
	}
	if out.Minor, err = strconv.Atoi(parts[1]); err != nil {
		return Semver{}, false
	}
	if out.Patch, err = strconv.Atoi(parts[2]); err != nil {
		return Semver{}, false
	}
	return out, true
}

// Compare returns -1, 0, or 1 by comparing only the MAJOR.MINOR.PATCH
// triple. Suffixes are ignored for ordering — a build past a tag
// ("0.1.0-3-gabcd") is treated as equal to the tag itself; the
// caller is expected to use HasSuffix() to label it as a development
// build separately.
func (a Semver) Compare(b Semver) int {
	switch {
	case a.Major != b.Major:
		if a.Major < b.Major {
			return -1
		}
		return 1
	case a.Minor != b.Minor:
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	case a.Patch != b.Patch:
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// HasSuffix reports whether the parsed version carried anything past
// MAJOR.MINOR.PATCH — typically a `git describe` trailer indicating
// commits past the tag, or a `-dirty` flag for uncommitted changes.
func (a Semver) HasSuffix() bool { return a.Suffix != "" }

// String renders MAJOR.MINOR.PATCH (no suffix). Used in the JSON
// payload so the UI can show a clean version label even when the
// running binary's raw version string includes a `git describe`
// trailer.
func (a Semver) String() string {
	return strconv.Itoa(a.Major) + "." + strconv.Itoa(a.Minor) + "." + strconv.Itoa(a.Patch)
}
