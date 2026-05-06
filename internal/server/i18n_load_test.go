package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderI18nJS_PlaceholderSubstituted is the end-to-end check
// that the runtime swap actually wires up: the embedded i18n.js
// template carries the __ZV_I18N_DICT__ marker, the rendered output
// has the DICT injected as valid JSON, and at least one known
// English→Swedish translation survives the round-trip.
func TestRenderI18nJS_PlaceholderSubstituted(t *testing.T) {
	out, err := renderI18nJS()
	if err != nil {
		t.Fatalf("renderI18nJS: %v", err)
	}
	if bytes.Contains(out, []byte("__ZV_I18N_DICT__")) {
		t.Fatal("rendered i18n.js still contains placeholder")
	}
	// The DICT object should appear with the literal keys we've shipped
	// since the original i18n.js — picking a stable one with no special
	// characters keeps this assertion robust against translation churn.
	if !bytes.Contains(out, []byte(`"Log in":"Logga in"`)) {
		t.Errorf("rendered i18n.js missing Log in→Logga in mapping")
	}
}

// TestLoadWebDict_ENIsEmpty enforces the size optimisation: the
// English source file is identity-shaped on disk for Crowdin's
// benefit, but at runtime the empty `en: {}` is enough — t() falls
// through to the key when no entry is found. Inlining 150 KB of
// redundant English-source-as-translation would bloat every page
// load for no behavioural gain.
func TestLoadWebDict_ENIsEmpty(t *testing.T) {
	dict, err := loadWebDict()
	if err != nil {
		t.Fatalf("loadWebDict: %v", err)
	}
	if got := dict["en"]; len(got) != 0 {
		t.Errorf("en dict has %d entries, want 0 (runtime drops them)", len(got))
	}
	if got := dict["sv"]; len(got) == 0 {
		t.Error("sv dict is empty — sv.json failed to load?")
	}
}

// TestMarshalDict_DeterministicOrder makes sure the rendered DICT is
// stable across runs so `/i18n.js` bytes are reproducible — needed
// for content-hashed CDN headers (if/when added) and just a sanity
// check that JSON output isn't randomised.
func TestMarshalDict_DeterministicOrder(t *testing.T) {
	dict := map[string]map[string]string{
		"sv": {"b": "B", "a": "A"},
		"en": {},
	}
	a, _ := marshalDict(dict)
	for i := 0; i < 5; i++ {
		b, _ := marshalDict(dict)
		if !bytes.Equal(a, b) {
			t.Fatalf("marshalDict not stable across runs: %s vs %s", a, b)
		}
	}
	// Round-trip through json to confirm the bytes are valid JSON.
	var parsed map[string]map[string]string
	if err := json.Unmarshal(a, &parsed); err != nil {
		t.Fatalf("rendered JSON failed to parse: %v body=%s", err, a)
	}
	if got, want := parsed["sv"]["b"], "B"; got != want {
		t.Errorf("round-trip lost sv.b: got %q", got)
	}
}

// TestLandingLocalesPopulated confirms the package init wired itself
// up — every direct landingLocales consumer (resolveLang, the
// landing renderer, the topbar substitution helpers) trusts the map
// to be non-empty at request time, and an empty map silently
// renders blank pages.
func TestLandingLocalesPopulated(t *testing.T) {
	if got := len(landingLocales); got == 0 {
		t.Fatal("landingLocales empty — package init failed to load JSON")
	}
	for _, lang := range []string{"en", "sv"} {
		s, ok := landingLocales[lang]
		if !ok {
			t.Errorf("locale %q missing", lang)
			continue
		}
		// Title is a load-bearing field — used in the <title> on
		// every landing render — so a zero value here would ship a
		// blank tab title in production.
		if strings.TrimSpace(s.Title) == "" {
			t.Errorf("locale %q has empty Title (JSON didn't populate?)", lang)
		}
	}
}
