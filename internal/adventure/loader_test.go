package adventure

import (
	"encoding/json"
	"testing"
)

func TestCheckAnswer_RegexAlternation(t *testing.T) {
	p := Page{
		Accept: []AcceptEntry{
			{Text: "(6|sex)", Regex: true},
		},
	}
	cases := map[string]bool{
		"6":     true,
		"SEX":   true,
		"sex":   true,
		" Six ": false, // not in the alternation
		"":      false,
	}
	for input, want := range cases {
		got := p.CheckAnswer(input)
		if got != want {
			t.Errorf("CheckAnswer(%q) = %v, want %v", input, got, want)
		}
	}
}

// Pre-regex schema: rows authored before the "Add regex answers"
// commit stored accept as a JSON array of strings (["foo", "bar"]),
// not the current [{text:..., regex:...}] object shape. Loader must
// accept both so old data keeps working.
func TestParseAccept_LegacyStringArray(t *testing.T) {
	got, err := ParseAcceptJSON([]byte(`["foo", "(6|sex)", "  PIN  "]`))
	if err != nil {
		t.Fatalf("legacy parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []string{"foo", "(6|sex)", "  PIN  "} {
		if got[i].Text != want {
			t.Errorf("entry %d text = %q, want %q", i, got[i].Text, want)
		}
		if got[i].Regex {
			t.Errorf("entry %d Regex = true, want false (legacy strings are literal)", i)
		}
	}
}

func TestParseAccept_NewObjectArray(t *testing.T) {
	got, err := ParseAcceptJSON([]byte(`[{"text":"(6|sex)","regex":true},{"text":"PIN"}]`))
	if err != nil {
		t.Fatalf("new parse: %v", err)
	}
	if len(got) != 2 || got[0].Text != "(6|sex)" || !got[0].Regex {
		t.Fatalf("first entry wrong: %+v", got[0])
	}
	if got[1].Text != "PIN" || got[1].Regex {
		t.Fatalf("second entry wrong: %+v", got[1])
	}
}

func TestParseAccept_Empty(t *testing.T) {
	got, err := ParseAcceptJSON([]byte(`[]`))
	if err != nil || len(got) != 0 {
		t.Fatalf("empty parse: got %d entries, err %v", len(got), err)
	}
	got, err = ParseAcceptJSON(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil parse: got %d entries, err %v", len(got), err)
	}
}

// Ensure the legacy parser still produces functioning CheckAnswer
// behaviour: a pre-regex row's literal entries must literal-match,
// and the "(6|sex)" pattern stays a literal in legacy mode (the
// arranger never had the chance to flag it as regex back then).
func TestCheckAnswer_LegacyDataStillWorks(t *testing.T) {
	entries, err := ParseAcceptJSON([]byte(`["yes", "(6|sex)"]`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := Page{Accept: entries}
	if !p.CheckAnswer("yes") {
		t.Errorf("legacy literal should match")
	}
	// Legacy entries are literal, so "(6|sex)" only matches if typed
	// verbatim. If the arranger wants alternation, they re-save with
	// the regex box ticked.
	if p.CheckAnswer("6") {
		t.Errorf("legacy literal should NOT alternation-match")
	}
	if !p.CheckAnswer("(6|sex)") {
		t.Errorf("legacy literal should match the verbatim string")
	}
}

// Marshal-roundtrip: a marshaled []AcceptEntry value should re-parse
// to itself, so we don't regress the new shape while supporting the
// legacy one.
func TestParseAccept_Roundtrip(t *testing.T) {
	in := []AcceptEntry{
		{Text: "yes", Regex: false},
		{Text: "(6|sex)", Regex: true},
	}
	bs, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := ParseAcceptJSON(bs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(out) != 2 || out[0].Text != "yes" || out[0].Regex || out[1].Text != "(6|sex)" || !out[1].Regex {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestCheckAnswer_RegexFlagFalse(t *testing.T) {
	// When Regex is false, the literal string must equal the input.
	// "(6|sex)" should NOT match "6" — it would only match if the
	// player literally typed "(6|sex)".
	p := Page{
		Accept: []AcceptEntry{
			{Text: "(6|sex)", Regex: false},
		},
	}
	if p.CheckAnswer("6") {
		t.Errorf("Regex=false should NOT alternation-match")
	}
	if !p.CheckAnswer("(6|sex)") {
		t.Errorf("Regex=false should literal-match the exact text")
	}
}
