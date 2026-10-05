package graph

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The player draws exactly the effects the server accepts: its lists
// (web/packages/graph/src/types.ts) must match Entrances and Attentions.
func TestAnimationNamesMatchThePlayer(t *testing.T) {
	src, err := os.ReadFile("../../web/packages/graph/src/types.ts")
	if err != nil {
		t.Skipf("no web sources: %v", err)
	}
	for name, want := range map[string][]string{"ENTRANCES": Entrances, "ATTENTIONS": Attentions} {
		m := regexp.MustCompile(`export const ` + name + ` = \[([^\]]*)\]`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("%s not found in types.ts", name)
		}
		got := strings.Fields(strings.NewReplacer(`"`, " ", ",", " ").Replace(string(m[1])))
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: player has %v, server %v", name, got, want)
		}
	}
}
