package graph

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klahr/zonventure/internal/graph/match"
)

// HostValue is one word a party's host fills in: see Graph.HostValues.
type HostValue struct {
	Slug string `json:"slug"`
	// Label is what the host page asks: "The birthday child's name".
	Label   string `json:"label"`
	Default string `json:"default,omitempty"`
}

// HostValue returns the value a {{host:<slug>}} shows, and whether the
// graph has one by that slug.
func (g *Graph) HostValue(slug string) (string, bool) {
	for _, v := range g.HostValues {
		if v.Slug == slug {
			return v.Default, true
		}
	}
	return "", false
}

// HostEditable is something a party's host may rewrite: a block marked
// HostEdit, or a puzzle's accepted answer marked HostAnswer.
type HostEditable struct {
	// Key addresses it in PartyOverrides: "<node>/<block>" for a block,
	// "<node>" for a puzzle's answer.
	Key   string    `json:"key"`
	Node  string    `json:"node"`
	Step  string    `json:"step"`
	Kind  BlockKind `json:"kind,omitempty"` // a block's; empty for an answer
	Text  string    `json:"text,omitempty"`
	Opts  []string  `json:"options,omitempty"`
	Right string    `json:"answer,omitempty"` // a quiz's right choice, 1/X/2
	// Accepts is a puzzle answer's: the exact answers its first answer
	// output takes.
	Accepts []string `json:"accepts,omitempty"`
}

// HostEditables lists, in the graph's order, what a party's host may
// rewrite.
func (g *Graph) HostEditables() []HostEditable {
	var out []HostEditable
	for _, n := range g.Nodes {
		step := n.Title
		if step == "" {
			step = n.Slug
		}
		for _, b := range n.Blocks() {
			if !b.HostEdit || (b.Kind != BlockText && b.Kind != BlockQuiz) {
				continue
			}
			e := HostEditable{Key: n.Slug + "/" + b.ID, Node: n.Slug, Step: step, Kind: b.Kind, Text: b.Text}
			if b.Kind == BlockQuiz {
				e.Opts, e.Right = append([]string(nil), b.Options...), b.Answer
			}
			out = append(out, e)
		}
		if p := n.PuzzleCfg; p != nil && p.HostAnswer && p.UsesMatchers() {
			if o := firstAnswerOutput(p); o != nil {
				e := HostEditable{Key: n.Slug, Node: n.Slug, Step: step}
				for _, m := range o.Matchers {
					if m.Kind == match.Exact {
						e.Accepts = append(e.Accepts, m.Value)
					}
				}
				out = append(out, e)
			}
		}
	}
	return out
}

func firstAnswerOutput(p *PuzzleConfig) *Output {
	for i := range p.Outputs {
		if p.Outputs[i].Trigger == OnAnswer {
			return &p.Outputs[i]
		}
	}
	return nil
}

// PartyOverrides is what a party's host changed of its adventure.
type PartyOverrides struct {
	// Values fill the graph's HostValues, by slug.
	Values map[string]string `json:"values,omitempty"`
	// Blocks rewrite blocks marked HostEdit, by "<node>/<block>".
	Blocks map[string]BlockOverride `json:"blocks,omitempty"`
	// Answers replace the exact answers a HostAnswer puzzle accepts, by
	// node.
	Answers map[string][]string `json:"answers,omitempty"`
	// Zones move zones marked Movable, by slug, for a party in a park.
	Zones map[string]ZonePlace `json:"zones,omitempty"`
}

// BlockOverride is a host's version of a block: a text's words, or a
// quiz question's text, options and right choice.
type BlockOverride struct {
	Text    string   `json:"text,omitempty"`
	Options []string `json:"options,omitempty"`
	Answer  string   `json:"answer,omitempty"`
}

// ZonePlace is where a host put a movable zone.
type ZonePlace struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Limits on what a host writes.
const (
	MaxHostText   = 2000
	MaxHostValue  = 80
	MaxHostAnswer = 200
)

// Check reports the first thing in o that the graph does not let a host
// change, or that is out of bounds; nil when all of it applies.
func (o PartyOverrides) Check(g *Graph) error {
	editable := map[string]HostEditable{}
	for _, e := range g.HostEditables() {
		editable[e.Key] = e
	}
	for slug, v := range o.Values {
		if _, ok := g.HostValue(slug); !ok {
			return fmt.Errorf("the adventure has no value %q", slug)
		}
		if len([]rune(v)) > MaxHostValue {
			return fmt.Errorf("%q is too long (%d characters max)", slug, MaxHostValue)
		}
	}
	for key, b := range o.Blocks {
		e, ok := editable[key]
		if !ok || e.Kind == "" {
			return fmt.Errorf("%q cannot be changed", key)
		}
		if len([]rune(b.Text)) > MaxHostText {
			return fmt.Errorf("the text is too long (%d characters max)", MaxHostText)
		}
		if e.Kind == BlockQuiz {
			if len(b.Options) != len(QuizChoices) {
				return fmt.Errorf("a quiz question has %d options", len(QuizChoices))
			}
			valid := false
			for _, c := range QuizChoices {
				valid = valid || b.Answer == c
			}
			if !valid {
				return fmt.Errorf("pick the right option of the question")
			}
			for _, opt := range b.Options {
				if strings.TrimSpace(opt) == "" || len([]rune(opt)) > MaxHostAnswer {
					return fmt.Errorf("every option needs words, up to %d characters", MaxHostAnswer)
				}
			}
		}
	}
	for node, as := range o.Answers {
		e, ok := editable[node]
		if !ok || e.Kind != "" {
			return fmt.Errorf("the answer of %q cannot be changed", node)
		}
		n := 0
		for _, a := range as {
			if strings.TrimSpace(a) == "" {
				continue
			}
			if len([]rune(a)) > MaxHostAnswer {
				return fmt.Errorf("an answer is too long (%d characters max)", MaxHostAnswer)
			}
			n++
		}
		if n == 0 {
			return fmt.Errorf("the step %q needs an answer", e.Step)
		}
	}
	for slug, p := range o.Zones {
		z := g.zone(slug)
		if z == nil || !z.Movable {
			return fmt.Errorf("the place %q cannot be moved", slug)
		}
		if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
			return fmt.Errorf("the place %q is off the map", slug)
		}
	}
	return nil
}

func (g *Graph) zone(slug string) *Zone {
	for i := range g.Zones {
		if g.Zones[i].Slug == slug {
			return &g.Zones[i]
		}
	}
	return nil
}

// MinPartyZoneRadius is the least radius a zone a host placed has: a
// phone's position outdoors can be that far off, and a child must not be
// kept waiting at the right tree.
const MinPartyZoneRadius = 25

// WithOverrides returns a copy of g as a party's host adapted it. What
// they changed replaces the author's words in every language, so a team
// that switches language still reads the host's version. g is left as
// it was; o must have passed Check.
func (g *Graph) WithOverrides(o PartyOverrides) (*Graph, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("copying graph: %w", err)
	}
	var c Graph
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("copying graph: %w", err)
	}
	for i := range c.HostValues {
		if v := strings.TrimSpace(o.Values[c.HostValues[i].Slug]); v != "" {
			c.HostValues[i].Default = v
		}
	}
	forget := func(prefix string) {
		for _, tr := range c.Translations {
			for k := range tr {
				if k == prefix || strings.HasPrefix(k, prefix+":") {
					delete(tr, k)
				}
			}
		}
	}
	for i := range c.Nodes {
		n := &c.Nodes[i]
		// AllBlocks shares the node's slice: changing its elements
		// changes the copy's blocks.
		bs := n.AllBlocks()
		for j := range bs {
			b := &bs[j]
			ov, ok := o.Blocks[n.Slug+"/"+b.ID]
			if !ok || !b.HostEdit {
				continue
			}
			if t := strings.TrimSpace(ov.Text); t != "" {
				b.Text = t
			}
			if b.Kind == BlockQuiz && len(ov.Options) == len(QuizChoices) {
				b.Options = append([]string(nil), ov.Options...)
				b.Answer = ov.Answer
			}
			forget(BlockKey(n.Slug, b.ID))
		}
		if as, ok := o.Answers[n.Slug]; ok && n.PuzzleCfg != nil && n.PuzzleCfg.HostAnswer {
			if out := firstAnswerOutput(n.PuzzleCfg); out != nil {
				var ms []match.Matcher
				for _, a := range as {
					if a = strings.TrimSpace(a); a != "" {
						ms = append(ms, match.Matcher{Kind: match.Exact, Value: a})
					}
				}
				out.Matchers = ms
			}
		}
	}
	for i := range c.Zones {
		z := &c.Zones[i]
		p, ok := o.Zones[z.Slug]
		if !ok || !z.Movable {
			continue
		}
		z.Lat, z.Lng, z.Polygon = p.Lat, p.Lng, nil
		z.RadiusM = max(z.RadiusM, MinPartyZoneRadius)
	}
	return &c, nil
}
