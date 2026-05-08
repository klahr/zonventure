package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStoreI18nJSON_EscapesAttacks is the FC5 regression guard. A
// translator-supplied locale value containing a double quote, a
// backslash, or the literal "</script" substring must not be able
// to break out of either the JSON literal or the surrounding inline
// `<script type="application/json">` element.
func TestStoreI18nJSON_EscapesAttacks(t *testing.T) {
	s := landingStrings{
		StoreToastPaid:      `quote " inside`,
		StoreToastCancelled: "back\\slash",
		StoreEmptyHeading:   "</script><script>alert(1)</script>",
		StoreTeamSizeSolo:   "ok",
		StoreTeamSizePlural: "ok",
		StoreMeetsAt:        "ok",
		StoreCardCTA:        "ok",
		StorePerTicket:      "ok",
		StorePerTeam:        "ok",
		StoreEmptyBody:      "ok",
		StoreErrorHeading:   "ok",
		StoreErrorBody:      "ok",
		StoreHandoffPreparing:     "ok",
		StoreHandoffEmailFallback: "ok",
		StoreHandoffFailed:        "ok",
	}

	out := storeI18nJSON(s)

	// 1. The output must JSON.parse cleanly.
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	// 2. Round-trip preserves the strings (the JS will see them
	//    intact after JSON.parse).
	if got["toastPaid"] != s.StoreToastPaid {
		t.Errorf("toastPaid round-trip: %q vs %q", got["toastPaid"], s.StoreToastPaid)
	}
	if got["toastCancelled"] != s.StoreToastCancelled {
		t.Errorf("toastCancelled round-trip: %q vs %q", got["toastCancelled"], s.StoreToastCancelled)
	}
	if got["emptyHeading"] != s.StoreEmptyHeading {
		t.Errorf("emptyHeading round-trip: %q vs %q", got["emptyHeading"], s.StoreEmptyHeading)
	}
	// 3. The literal "</script" substring must NOT appear in the
	//    rendered text — that's the script-escape vector.
	if strings.Contains(strings.ToLower(out), "</script") {
		t.Errorf("output contains literal </script close-tag: %s", out)
	}
}
