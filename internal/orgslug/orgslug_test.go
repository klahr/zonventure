package orgslug

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Mindrace", "mindrace"},
		{"Mindrace AB", "mindrace-ab"},
		{"  Spaces  ", "spaces"},
		{"Mårten & Co.", "marten-co"},
		{"Resolution Games AB", "resolution-games-ab"},
		{"!!!", "org"}, // pure punctuation falls back to "org"
		{"a", "a-org"}, // too-short pads
		{"ab", "ab-org"},
		{strings.Repeat("longname-", 10), "longname-longname-longname-lon"}, // 30-char cap, no trailing hyphen
		{"Café Société", "cafe-societe"},
		{"日本語", "org"}, // CJK collapses entirely → fallback
	}
	for _, c := range cases {
		got := Slugify(c.in)
		if got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
		if err := Validate(got); err != nil {
			t.Errorf("Slugify(%q) produced invalid slug %q: %v", c.in, got, err)
		}
	}
}

func TestValidate(t *testing.T) {
	good := []string{"mindrace", "team-alpha", "abc", "x99", "a-b-c"}
	for _, s := range good {
		if err := Validate(s); err != nil {
			t.Errorf("Validate(%q) unexpectedly failed: %v", s, err)
		}
	}
	bad := []string{"", "ab", "AB", "team_alpha", "-leading", "trailing-", "double--dash", "Spaces in"}
	for _, s := range bad {
		if err := Validate(s); err == nil {
			t.Errorf("Validate(%q) unexpectedly passed", s)
		}
	}
	for r := range Reserved {
		if err := Validate(r); err == nil {
			t.Errorf("Validate(%q) accepted a reserved word", r)
		}
	}
}
