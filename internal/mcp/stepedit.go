package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/klahr/zonventure/internal/graph"
)

// Small edits to one step, for an author watching the draft on a dev
// phone: change a text by quoting it, or patch a few fields, without
// reading the whole graph or resending the whole step. A step not named
// is the one the author's dev phone shows.

func (s *Server) registerStepTools() {
	s.tools.register(ToolDescriptor{
		Name:     "get_step",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "One step of the draft as JSON — the shape put_node and patch_node take — with where its ports lead. " +
			"Without node, the step the author's dev phone shows. Much smaller than get_adventure_graph: read this before editing one step.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
				"node": map[string]any{"type": "string", "description": "Step slug; omit for the dev phone's step."},
			},
		},
	}, getStep)
}

// devNode is the step the caller's dev phone shows.
func (s *Server) devNode(ctx context.Context, slug string) (string, *ToolCallResult) {
	status, body, err := callAdmin(ctx, s.Admin.DevScreen, "GET", "/api/admin/adventures/"+slug+"/dev-screen", map[string]string{"slug": slug}, nil, nil)
	if err != nil {
		r := errorResult("call: " + err.Error())
		return "", &r
	}
	if status != http.StatusOK {
		r := adminCallResult(status, body)
		return "", &r
	}
	var resp struct {
		View struct {
			Node string `json:"node"`
		} `json:"view"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.View.Node == "" {
		r := errorResult("the dev phone is not on a step: name the step to edit")
		return "", &r
	}
	return resp.View.Node, nil
}

func getStep(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug string `json:"slug"`
		Node string `json:"node"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	if a.Node == "" {
		n, bad := s.devNode(ctx, a.Slug)
		if bad != nil {
			return *bad, nil
		}
		a.Node = n
	}
	g, bad := s.loadDraftForEdit(ctx, a.Slug)
	if bad != nil {
		return *bad, nil
	}
	i := nodeIndex(g, a.Node)
	if i < 0 {
		return errorResult(fmt.Sprintf("no step %q in the draft", a.Node)), nil
	}
	out := map[string]any{"node": g.Nodes[i], "next": map[string]string{}, "from": []string{}}
	from := []string{}
	for _, e := range g.Edges {
		if e.From == a.Node {
			out["next"].(map[string]string)[e.FromPort] = e.To
		}
		if e.To == a.Node {
			from = append(from, e.From+"."+e.FromPort)
		}
	}
	out["from"] = from
	b, _ := json.Marshal(out)
	return textResult(string(b)), nil
}

// asTree turns a node into plain JSON values, numbers kept exact.
func asTree(n graph.Node) (any, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	return v, dec.Decode(&v)
}

func fromTree(v any) (graph.Node, error) {
	var n graph.Node
	b, err := json.Marshal(v)
	if err != nil {
		return n, err
	}
	return n, decodeStrict(b, &n)
}

// textKeys are a step's names for things, not its words: set_text
// leaves them alone, so a quoted word cannot rename a slug or a kind.
var textKeys = map[string]bool{"slug": true, "id": true, "kind": true, "type": true, "zone": true, "theme": true, "font": true, "heading_font": true, "media": true,
	// Names of other things — ports, roles, posts, items, counters, notes,
	// the puzzle a crossroads reads and its answers' references — and the
	// settings spelled as words: changing words never renames them.
	"port": true, "role": true, "by": true, "post_roles": true, "posts": true, "item": true, "counter": true,
	"journal_entry": true, "unlocks": true, "from": true, "answers": true, "group": true,
	"policy": true, "trigger": true, "outcome": true, "op": true, "keyboard": true, "missing": true, "method": true,
	// A sabotage's note, a status's counters, a webhook's address, and the
	// words a style, a cipher, an animation or a colour is chosen by.
	"journal": true, "counters": true, "url": true, "style": true, "cipher": true,
	"entrance": true, "attention": true, "repeat": true, "color": true,
	// A condition's steps, blocks, ports and comparisons.
	"node": true, "block": true, "is": true, "cmp": true}

// condKeys hold conditions — a block's show.when, a branch's and an
// output's condition — which are all names and settings: their members
// are zones and weekdays where a set answer's are words, so set_text
// leaves the whole condition alone.
var condKeys = map[string]bool{"when": true, "condition": true}

// setText replaces find with replace in the step's texts: once, unless all.
func setText(g *graph.Graph, slug, find, replace string, all bool) error {
	if find == "" {
		return fmt.Errorf("find is required: the text to change, quoted exactly")
	}
	i := nodeIndex(g, slug)
	if i < 0 {
		return fmt.Errorf("no step %q", slug)
	}
	tree, err := asTree(g.Nodes[i])
	if err != nil {
		return err
	}
	var found, texts []string
	count := 0
	var walk func(v any, path string, key string) any
	walk = func(v any, path, key string) any {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if !condKeys[k] {
					x[k] = walk(c, path+"."+k, k)
				}
			}
		case []any:
			for j, c := range x {
				x[j] = walk(c, fmt.Sprintf("%s[%d]", path, j), key)
			}
		case string:
			if textKeys[key] || x == "" {
				return x
			}
			texts = append(texts, path+": "+clip(x, 120))
			if n := strings.Count(x, find); n > 0 {
				count += n
				found = append(found, path)
				return strings.ReplaceAll(x, find, replace)
			}
		}
		return v
	}
	tree = walk(tree, "", "")
	switch {
	case count == 0:
		sort.Strings(texts)
		return fmt.Errorf("%q is not in step %q. Its texts:\n%s", find, slug, strings.Join(texts, "\n"))
	case count > 1 && !all:
		sort.Strings(found)
		return fmt.Errorf("%q appears %d times in step %q (%s): quote more of the text to pick one, or set all: true", find, count, slug, strings.Join(found, ", "))
	}
	n, err := fromTree(tree)
	if err != nil {
		return err
	}
	g.Nodes[i] = n
	return nil
}

func clip(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// patchNode applies a JSON merge patch to the step, with lists of things
// that have ids — blocks, choices, matchers — merged by id, and a
// puzzle's outputs by port: an item with a known key is patched, a new
// one is added, and {"id": …, "_delete": true} goes.
func patchNode(g *graph.Graph, slug string, patch json.RawMessage) error {
	i := nodeIndex(g, slug)
	if i < 0 {
		return fmt.Errorf("no step %q", slug)
	}
	dec := json.NewDecoder(bytes.NewReader(patch))
	dec.UseNumber()
	var p any
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("patch: %w", err)
	}
	pm, ok := p.(map[string]any)
	if !ok {
		return fmt.Errorf("patch must be an object")
	}
	if s, ok := pm["slug"]; ok && s != slug {
		return fmt.Errorf("patch cannot rename a step")
	}
	tree, err := asTree(g.Nodes[i])
	if err != nil {
		return err
	}
	n, err := fromTree(mergePatch(tree, pm))
	if err != nil {
		return fmt.Errorf("patched step: %w", err)
	}
	g.Nodes[i] = n
	return nil
}

func mergePatch(target, patch any) any {
	switch p := patch.(type) {
	case map[string]any:
		t, ok := target.(map[string]any)
		if !ok {
			t = map[string]any{}
		}
		for k, v := range p {
			if v == nil {
				delete(t, k)
			} else {
				t[k] = mergePatch(t[k], v)
			}
		}
		return t
	case []any:
		t, ok := target.([]any)
		key := listKey(t, p)
		if !ok || key == "" {
			return p
		}
		for _, item := range p {
			pi := item.(map[string]any)
			at := -1
			for j, c := range t {
				if c.(map[string]any)[key] == pi[key] {
					at = j
					break
				}
			}
			switch {
			case pi["_delete"] == true:
				if at >= 0 {
					t = append(t[:at], t[at+1:]...)
				}
			case at >= 0:
				t[at] = mergePatch(t[at], pi)
			default:
				t = append(t, mergePatch(nil, pi))
			}
		}
		return t
	}
	return patch
}

// listKey names the field the items of both lists are merged by — id,
// else port — or "" when some item lacks it, and the patch's list
// replaces the step's.
func listKey(lists ...[]any) string {
	for _, key := range []string{"id", "port"} {
		if keyed(key, lists...) {
			return key
		}
	}
	return ""
}

func keyed(key string, lists ...[]any) bool {
	for _, list := range lists {
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return false
			}
			if _, ok := m[key].(string); !ok {
				return false
			}
		}
	}
	return true
}
