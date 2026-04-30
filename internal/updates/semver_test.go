package updates_test

import (
	"testing"

	"git.sr.ht/~klahr/zv/internal/updates"
)

func TestParseSemverShapes(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		want   string // MAJOR.MINOR.PATCH form
		suffix string
	}{
		{"0.1.0", true, "0.1.0", ""},
		{"v1.2.3", true, "1.2.3", ""},
		{"0.1.0-3-gabcd123", true, "0.1.0", "-3-gabcd123"},
		{"0.1.0-3-gabcd123-dirty", true, "0.1.0", "-3-gabcd123-dirty"},
		{"v1.2.3+build.5", true, "1.2.3", "+build.5"},
		{"dev", false, "", ""},
		{"", false, "", ""},
		{"1.2", false, "", ""},
		{"a.b.c", false, "", ""},
	}
	for _, tc := range cases {
		got, ok := updates.ParseSemver(tc.in)
		if ok != tc.ok {
			t.Errorf("ParseSemver(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.String() != tc.want {
			t.Errorf("ParseSemver(%q).String() = %q, want %q", tc.in, got.String(), tc.want)
		}
		if got.Suffix != tc.suffix {
			t.Errorf("ParseSemver(%q).Suffix = %q, want %q", tc.in, got.Suffix, tc.suffix)
		}
	}
}

func TestSemverCompare(t *testing.T) {
	mk := func(s string) updates.Semver {
		v, ok := updates.ParseSemver(s)
		if !ok {
			t.Fatalf("ParseSemver(%q)", s)
		}
		return v
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"0.1.0", "0.1.0-3-gabcd", 0}, // suffix ignored for ordering
		{"0.2.0", "0.1.9-99-gabcd", 1},
	}
	for _, tc := range cases {
		got := mk(tc.a).Compare(mk(tc.b))
		if got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSemverHasSuffix(t *testing.T) {
	clean, _ := updates.ParseSemver("0.1.0")
	if clean.HasSuffix() {
		t.Error("clean tag should not have suffix")
	}
	dirty, _ := updates.ParseSemver("0.1.0-3-gabcd")
	if !dirty.HasSuffix() {
		t.Error("git-describe trailer should count as suffix")
	}
}
