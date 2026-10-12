package printmat

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

func TestParse(t *testing.T) {
	o := Parse([]byte(`{"letter": "emailed", "map": "courier"}`))
	if len(o) != 1 || o["letter"] != graph.DeliveryEmailed {
		t.Errorf("Parse = %v, want only the valid letter override", o)
	}
	if o := Parse([]byte(`not json`)); len(o) != 0 {
		t.Errorf("Parse of a bad row = %v, want none", o)
	}
}

func TestEffective(t *testing.T) {
	g := &graph.Graph{PrintMaterials: []graph.PrintMaterial{
		{ID: "letter", Delivery: graph.DeliveryPosted},
		{ID: "key", Delivery: graph.DeliveryOrganiser},
	}}
	eff := Effective(g, Overrides{"letter": graph.DeliveryEmailed})
	if eff[0].Delivery != graph.DeliveryEmailed || eff[1].Delivery != graph.DeliveryOrganiser {
		t.Errorf("Effective = %+v", eff)
	}
	if g.PrintMaterials[0].Delivery != graph.DeliveryPosted {
		t.Error("Effective changed the graph")
	}
	if Effective(nil, nil) != nil {
		t.Error("no version, no materials")
	}
}

func TestFile(t *testing.T) {
	pm := graph.PrintMaterial{Files: map[string]int64{"en": 1, "sv": 2, "de": 3}}
	for _, c := range []struct {
		lang, main, want string
		id               int64
	}{
		{"sv", "en", "sv", 2},
		{"fi", "en", "en", 1},
		{"", "fi", "de", 3},
	} {
		if l, id := File(pm, c.lang, c.main); l != c.want || id != c.id {
			t.Errorf("File(%q, %q) = %q %d, want %q %d", c.lang, c.main, l, id, c.want, c.id)
		}
	}
	if l, id := File(graph.PrintMaterial{}, "sv", "sv"); l != "" || id != 0 {
		t.Errorf("no files = %q %d", l, id)
	}
}
