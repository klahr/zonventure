package session

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// The shared vectors @zonventure/graph's tests read too
// (web/packages/graph/testdata/translations.json): every text an author
// can translate, each starting with MAIN, and its key. The designer lists
// exactly those keys; here a team playing in a translation sees every
// one of them translated, on whichever step shows it, and none in the
// main language. A text the engine forgets, or reads by another key than
// the designer writes, fails one or the other.
func TestEveryTranslatableTextIsTranslated(t *testing.T) {
	raw, err := os.ReadFile("../../../web/packages/graph/testdata/translations.json")
	if err != nil {
		t.Fatalf("reading shared vectors: %v", err)
	}
	var vectors struct {
		Graph    graph.Graph       `json:"graph"`
		Keys     []string          `json:"keys"`
		NotShown map[string]string `json:"not_shown"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("decoding shared vectors: %v", err)
	}
	g := &vectors.Graph
	g.Translations = map[string]map[string]string{"xx": {}}
	for _, k := range vectors.Keys {
		g.Translations["xx"][k] = "XX " + k
	}

	// Every view of every step a team can stand on, holding everything
	// that shows more: the items (but the lamp a slot asks for), the
	// journal note, the secret role, every hint and the card a draw
	// dealt; at the end the game is over, so the roles are revealed, and
	// once more with time run out.
	var views []string
	team := func(slug string) *Team {
		tm := &Team{G: g, Now: t0}
		tm.Init()
		tm.Lang = "xx"
		tm.Current = slug
		tm.NodeEnteredAt, tm.StartedAt = t0, t0
		tm.Items = map[string]int{"key": 1}
		tm.Journal = map[string]bool{"note": true}
		tm.Nodes = map[string]*NodeState{slug: {FirstSeen: t0, HintsRevealed: 10}}
		tm.Secrets = map[string]int64{"spy": 1}
		tm.Picks = map[string]string{PickKey("s", "draw"): "0"}
		tm.Viewer = &Viewer{Slot: 1, Roles: []string{"spy"}}
		tm.Completed = slug == "end"
		return tm
	}
	show := func(tm *Team) {
		v, err := json.Marshal(tm.View())
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, string(v))
	}
	for _, n := range g.Nodes {
		if n.Type != graph.Start && n.Type != graph.Effect {
			show(team(n.Slug))
		}
	}
	timedOut := team("end")
	timedOut.TimedOut = true
	show(timedOut)
	all := strings.Join(views, "\n")

	// A wrong answer's reply comes with the answer, not the view.
	res, _, err := team("typed").Answer("typed", "wrong")
	if err != nil {
		t.Fatal(err)
	}
	all += "\n" + res.Response

	// The main language must not show through anywhere.
	var left []string
	for _, v := range views {
		var doc any
		if err := json.Unmarshal([]byte(v), &doc); err != nil {
			t.Fatal(err)
		}
		walkStrings(doc, func(s string) {
			// A main text at the start of a string or of a line or word
			// in one; not inside a key, as a section's heading is.
			if mainText.MatchString(s) {
				left = append(left, s)
			}
		})
	}
	sort.Strings(left)
	for _, s := range compact(left) {
		t.Errorf("shown untranslated: %q", s)
	}

	// Every key must be read, by the key the designer writes. Accepted
	// answers are matched, not shown; an email is sent, not shown.
	for _, k := range vectors.Keys {
		if strings.HasSuffix(k, ":answers") || strings.HasSuffix(k, ":contains") {
			continue
		}
		if strings.HasPrefix(k, "n:mail:") || vectors.NotShown[k] != "" {
			continue
		}
		if !strings.Contains(all, "XX "+k) {
			t.Errorf("never shown translated: %s", k)
		}
	}

	n, _ := g.Node("mail")
	subject, body, step := g.Email("xx", n)
	if subject != "XX n:mail:email:subject" || body != "XX n:mail:email:body" || step != "XX n:mail:title" {
		t.Errorf("email in xx = %q, %q, %q", subject, body, step)
	}
}

var mainText = regexp.MustCompile(`(^|\s)MAIN `)

func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		f(x)
	case []any:
		for _, e := range x {
			walkStrings(e, f)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, f)
		}
	}
}

func compact(s []string) []string {
	var out []string
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}
