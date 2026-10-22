package mcp

// edit_adventure_graph: small, targeted edits to the draft instead of
// resending the whole envelope. An assistant authoring on site adds one
// step at a time; making it round-trip tens of kilobytes of graph for
// each is slow, costly and invites it to drop something by accident.
//
// The edit is a read-modify-write through the same admin handlers as
// get/save_adventure_graph, so authorisation, strict parsing and
// validation are unchanged. Operations apply in order and all-or-nothing:
// if one fails, nothing is saved.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

func (s *Server) registerGraphEditTools() {
	s.tools.register(ToolDescriptor{
		Name:     "edit_adventure_graph",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Apply a list of small edits to an adventure's draft graph and save it, without sending the whole graph. " +
			"Operations apply in order; if any fails nothing is saved. Returns the validation report and the draft's node and zone slugs. " +
			"Ops: put_node {node} adds or replaces the node with that slug (same shape as nodes in get_adventure_graph); " +
			"delete_node {slug} also removes its edges; put_zone {zone} adds or replaces a zone — with here: true it is centred where the author's phone last reported standing " +
			"(lat/lng are taken from there, radius_m defaults from the fix's accuracy; refused if the fix is older than two minutes); delete_zone {slug}; " +
			"connect {from, from_port, to} points an output port at a node, replacing where it pointed before; disconnect {from, from_port}; " +
			"put_theme {theme, main?} adds or replaces a saved theme {slug, name, look} (look: accent, accent_secondary, background, text, submit_bg as #rrggbb, font, heading_font, corners, cards, card_outline, backdrop, style); main: true makes it the main theme, the look players see wherever no step changes it, which reaches players and the store on publish; delete_theme {slug}, refused while a theme effect names it or it is the main theme. " +
			"put_item {item} adds or replaces an item {slug, name, icon, blocks?}: blocks are what a player sees opening it from the bag — text, note (a letter), image, audio and video blocks, as on a step, with no rule for when they show; an effect grant_item gives it. " +
			"set_option {options} turns adventure-wide settings on or off: hide_score keeps the running score off the team's phone (points still count at the end), journal_by_unlock lists the journal in the order the team found its notes, quiz_puzzles adds every typed-answer puzzle to the end screen's quiz walk, tour makes it a walk rather than a game (the phone speaks of the walk and of \"you\", not of a game and its players, and the end has no trophy), min_players and max_players bound a team (it waits until the least have joined, no one beyond the most joins; 0 is no bound); send only the ones to change. " +
			"set_text {slug?, find, replace, all?} changes a text of one step by quoting it exactly — the fastest way to change words; it fails, listing the step's texts, if find is not there, and asks for more of the text if it appears more than once (or all: true to change every one); " +
			"patch_node {slug?, patch} merges a JSON merge patch into one step (null removes a field; lists of items with ids — blocks, matchers — merge by id, and outputs by port: a known one is patched, a new one added, {id or port, _delete: true} removed; other lists, like hints, are replaced whole) — e.g. add an accepted answer with {\"puzzle\":{\"outputs\":[{\"port\":\"right\",\"matchers\":[{\"id\":\"seven\",\"kind\":\"exact\",\"value\":\"seven\"}]}]}}, without resending the step. " +
			"Without slug, set_text and patch_node edit the step the author's dev phone shows; the phone shows the change within a second. " +
			"An adventure without a graph starts from an empty one.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "ops"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
				"note": map[string]any{"type": "string", "description": noteDescription},
				"ops": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items": map[string]any{
						"type":     "object",
						"required": []string{"op"},
						"properties": map[string]any{
							"op":             map[string]any{"type": "string", "enum": []string{"put_node", "delete_node", "set_text", "patch_node", "put_zone", "delete_zone", "put_item", "put_theme", "delete_theme", "put_print_material", "delete_print_material", "connect", "disconnect", "set_option"}},
							"node":           map[string]any{"type": "object", "description": "put_node: the full node."},
							"zone":           map[string]any{"type": "object", "description": "put_zone: {slug, name, lat, lng, radius_m, polygon?, group?}."},
							"item":           map[string]any{"type": "object", "description": "put_item: {slug, name, icon, blocks?}."},
							"theme":          map[string]any{"type": "object", "description": "put_theme: {slug, name, look}."},
							"print_material": map[string]any{"type": "object", "description": `put_print_material: {id, name, delivery: "posted"|"emailed"|"organiser", files: {<language>: <pdf media id>}}, a PDF per language the adventure is played in.`},
							"slug":           map[string]any{"type": "string", "description": "delete_node / delete_zone / delete_theme / delete_print_material: what to delete; set_text / patch_node: the step (omit for the dev phone's)."},
							"find":           map[string]any{"type": "string", "description": "set_text: the text to change, quoted exactly as it is."},
							"replace":        map[string]any{"type": "string", "description": "set_text: what it becomes."},
							"all":            map[string]any{"type": "boolean", "description": "set_text: change every occurrence in the step."},
							"patch":          map[string]any{"type": "object", "description": "patch_node: the fields to change."},
							"options": map[string]any{
								"type":                 "object",
								"description":          "set_option: the settings to change.",
								"additionalProperties": false,
								"properties": map[string]any{
									"hide_score":        map[string]any{"type": "boolean"},
									"journal_by_unlock": map[string]any{"type": "boolean"},
									"quiz_puzzles":      map[string]any{"type": "boolean"},
									"tour":              map[string]any{"type": "boolean"},
									"min_players":       map[string]any{"type": "integer", "minimum": 0, "maximum": 50},
									"max_players":       map[string]any{"type": "integer", "minimum": 0, "maximum": 50},
								},
							},
							"from":      map[string]any{"type": "string"},
							"from_port": map[string]any{"type": "string"},
							"to":        map[string]any{"type": "string"},
							"here":      map[string]any{"type": "boolean", "description": "put_zone: centre the zone on the author's current position."},
							"main":      map[string]any{"type": "boolean", "description": "put_theme: make it the main theme."},
						},
					},
				},
			},
		},
	}, editAdventureGraph)

	s.tools.register(ToolDescriptor{
		Name:     "undo_adventure_graph_edit",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: fmt.Sprintf("Put the draft back as it was before the latest edit_adventure_graph call, and forget that edit. "+
			"Repeat to go further back, up to %d edits. Edits made in the designer or with save_adventure_graph are not undone by this.", maxUndo),
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug"},
			"properties": map[string]any{
				"slug": map[string]any{"type": "string", "description": "Adventure slug."},
			},
		},
	}, undoAdventureGraphEdit)

	s.tools.register(ToolDescriptor{
		Name:     "create_adventure",
		Audience: audienceAuthor,
		Scope:    scopeWrite,
		Description: "Create a new, empty adventure authored as a graph, with an optional brief. Its draft starts as a start node joined to an end node " +
			"(slugs start and end, start's port out); build on it with edit_adventure_graph. Nothing is published.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"slug", "title"},
			"properties": map[string]any{
				"slug":        map[string]any{"type": "string", "description": "URL name: a-z, 0-9 and hyphens."},
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"language":    map[string]any{"type": "string", "description": `BCP 47 tag of the content, e.g. "en" or "sv".`},
				"brief":       map[string]any{"type": "object", "description": "As update_adventure_meta's brief."},
			},
		},
	}, createGraphAdventure)

	s.tools.register(ToolDescriptor{
		Name:     "get_creator_position",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "Where the author's phone last reported standing: {lat, lng, accuracy_m, at, age_seconds}. " +
			"The phone reports while Field mode is open in the arranger. 404 if it never has. " +
			"To put a zone where the author stands, prefer edit_adventure_graph put_zone with here: true.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, getCreatorPosition)
}

type graphOp struct {
	Op       string          `json:"op"`
	Node     json.RawMessage `json:"node,omitempty"`
	Zone     json.RawMessage `json:"zone,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Theme    json.RawMessage `json:"theme,omitempty"`
	Print    json.RawMessage `json:"print_material,omitempty"`
	Slug     string          `json:"slug,omitempty"`
	From     string          `json:"from,omitempty"`
	FromPort string          `json:"from_port,omitempty"`
	To       string          `json:"to,omitempty"`
	Here     bool            `json:"here,omitempty"`
	Patch    json.RawMessage `json:"patch,omitempty"`
	Options  json.RawMessage `json:"options,omitempty"`
	Find     string          `json:"find,omitempty"`
	Replace  string          `json:"replace,omitempty"`
	All      bool            `json:"all,omitempty"`
	Main     bool            `json:"main,omitempty"`
}

type editArgs struct {
	Slug string    `json:"slug"`
	Ops  []graphOp `json:"ops"`
	Note string    `json:"note,omitempty"`
}

// editResult is what the model gets back: enough to know what the
// draft now holds and what is wrong with it, without the whole graph.
type editResult struct {
	Report valid.Report `json:"report"`
	Dirty  bool         `json:"dirty"`
	Nodes  []string     `json:"nodes"`
	Zones  []string     `json:"zones"`
}

func editAdventureGraph(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a editArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if len(a.Ops) == 0 {
		return errorResult("ops is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}

	g, base, bad := s.loadDraft(ctx, a.Slug)
	if bad != nil {
		return *bad, nil
	}
	before, err := json.Marshal(g)
	if err != nil {
		return errorResult("encoding draft: " + err.Error()), nil
	}
	var here *position
	for _, op := range a.Ops {
		if op.Here {
			p, bad := s.creatorPosition(ctx)
			if bad != nil {
				return *bad, nil
			}
			if p.AgeSeconds > maxHereAgeSeconds {
				return errorResult(fmt.Sprintf("the author's last position is %d seconds old; ask them to open Field mode on their phone and try again", p.AgeSeconds)), nil
			}
			here = &p
			break
		}
	}
	// set_text and patch_node without a step edit the dev phone's.
	dev := ""
	for i, op := range a.Ops {
		if (op.Op == "set_text" || op.Op == "patch_node") && op.Slug == "" {
			if dev == "" {
				n, bad := s.devNode(ctx, a.Slug)
				if bad != nil {
					return *bad, nil
				}
				dev = n
			}
			a.Ops[i].Slug = dev
		}
	}
	for i, op := range a.Ops {
		if err := applyGraphOp(g, op, here); err != nil {
			return errorResult(fmt.Sprintf("op %d (%s): %v — nothing was saved", i, op.Op, err)), nil
		}
	}

	var headers map[string]string
	if base != "" {
		headers = map[string]string{"X-Draft-Base": base}
	}
	status, body, err := callAdmin(ctx, s.Admin.PutAdventureGraph, "PUT", "/api/admin/adventures/"+a.Slug+"/graph",
		map[string]string{"slug": a.Slug}, noteHeader(headers, a.Note), graph.Envelope{Format: graph.FormatV2, Graph: *g})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status == http.StatusConflict {
		return errorResult("someone saved this adventure in the designer while you were editing; nothing was saved. Read it again and redo the edit."), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	if err := s.pushUndo(ctx, a.Slug, before); err != nil {
		return errorResult("the edit was saved, but recording it for undo failed: " + err.Error()), nil
	}
	var saved struct {
		Envelope graph.Envelope `json:"envelope"`
		Report   valid.Report   `json:"report"`
		Dirty    bool           `json:"dirty"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		return errorResult("decoding saved draft: " + err.Error()), nil
	}
	out := editResult{Report: saved.Report, Dirty: saved.Dirty, Nodes: []string{}, Zones: []string{}}
	for _, n := range saved.Envelope.Graph.Nodes {
		out.Nodes = append(out.Nodes, n.Slug)
	}
	for _, z := range saved.Envelope.Graph.Zones {
		out.Zones = append(out.Zones, z.Slug)
	}
	res, _ := json.Marshal(out)
	return textResult(string(res)), nil
}

// loadDraftForEdit fetches the draft through the admin handler, which
// also authorises the caller. A missing graph is an empty one.
func (s *Server) loadDraftForEdit(ctx context.Context, slug string) (*graph.Graph, *ToolCallResult) {
	g, _, bad := s.loadDraft(ctx, slug)
	return g, bad
}

// loadDraft is loadDraftForEdit with the draft's saved_at, for a save
// that must not overwrite a later one; empty when there is no draft.
func (s *Server) loadDraft(ctx context.Context, slug string) (*graph.Graph, string, *ToolCallResult) {
	status, body, err := callAdmin(ctx, s.Admin.GetAdventureGraph, "GET", "/api/admin/adventures/"+slug+"/graph",
		map[string]string{"slug": slug}, nil, nil)
	if err != nil {
		r := errorResult("call: " + err.Error())
		return nil, "", &r
	}
	if status == http.StatusNotFound && bytes.Contains(body, []byte(`"graph_not_found"`)) {
		return &graph.Graph{Nodes: []graph.Node{}, Edges: []graph.Edge{}}, "", nil
	}
	if status != http.StatusOK {
		r := adminCallResult(status, body)
		return nil, "", &r
	}
	var got struct {
		Envelope graph.Envelope `json:"envelope"`
		SavedAt  string         `json:"saved_at"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		r := errorResult("decoding draft: " + err.Error())
		return nil, "", &r
	}
	return &got.Envelope.Graph, got.SavedAt, nil
}

// decodeStrict decodes like graph.ParseEnvelope does: a field this
// build does not know is refused rather than silently dropped.
func decodeStrict(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return fmt.Errorf("missing")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// maxHereAgeSeconds is how old a fix may be for "here": older, and the
// author has probably walked on since.
const maxHereAgeSeconds = 120

type position struct {
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	AccuracyM  float64 `json:"accuracy_m"`
	AgeSeconds int     `json:"age_seconds"`
}

func (s *Server) creatorPosition(ctx context.Context) (position, *ToolCallResult) {
	var p position
	status, body, err := callAdmin(ctx, s.Admin.GetMyPosition, "GET", "/api/admin/me/position", nil, nil, nil)
	if err != nil {
		r := errorResult("call: " + err.Error())
		return p, &r
	}
	if status != http.StatusOK {
		r := adminCallResult(status, body)
		return p, &r
	}
	if err := json.Unmarshal(body, &p); err != nil {
		r := errorResult("decoding position: " + err.Error())
		return p, &r
	}
	return p, nil
}

// hereRadius picks the zone size the designer's "Mark where I stand"
// offers — 15, 30 or 60 m — as the smallest a team with the same GPS
// accuracy can reliably get inside.
func hereRadius(accuracyM float64) float64 {
	for _, r := range []float64{15, 30} {
		if accuracyM <= r/2 {
			return r
		}
	}
	return 60
}

// applyGraphOp applies one operation. here is the author's position,
// needed only by put_zone with here set.
func applyGraphOp(g *graph.Graph, op graphOp, here *position) error {
	switch op.Op {
	case "put_node":
		var n graph.Node
		if err := decodeStrict(op.Node, &n); err != nil {
			return fmt.Errorf("node: %w", err)
		}
		if n.Slug == "" {
			return fmt.Errorf("node needs a slug")
		}
		for i := range g.Nodes {
			if g.Nodes[i].Slug == n.Slug {
				if n.X == 0 && n.Y == 0 {
					n.X, n.Y = g.Nodes[i].X, g.Nodes[i].Y
				}
				g.Nodes[i] = n
				return nil
			}
		}
		if n.X == 0 && n.Y == 0 {
			n.X, n.Y = nextPosition(g)
		}
		g.Nodes = append(g.Nodes, n)
	case "set_text":
		return setText(g, op.Slug, op.Find, op.Replace, op.All)
	case "patch_node":
		return patchNode(g, op.Slug, op.Patch)
	case "delete_node":
		i := nodeIndex(g, op.Slug)
		if i < 0 {
			return fmt.Errorf("no node %q", op.Slug)
		}
		g.Nodes = append(g.Nodes[:i], g.Nodes[i+1:]...)
		edges := g.Edges[:0]
		for _, e := range g.Edges {
			if e.From != op.Slug && e.To != op.Slug {
				edges = append(edges, e)
			}
		}
		g.Edges = edges
	case "put_zone":
		var z graph.Zone
		if err := decodeStrict(op.Zone, &z); err != nil {
			return fmt.Errorf("zone: %w", err)
		}
		if z.Slug == "" {
			return fmt.Errorf("zone needs a slug")
		}
		if op.Here {
			if here == nil {
				return fmt.Errorf("no position for here")
			}
			z.Lat, z.Lng, z.Polygon = here.Lat, here.Lng, nil
			if z.RadiusM == 0 {
				z.RadiusM = hereRadius(here.AccuracyM)
			}
		}
		for i := range g.Zones {
			if g.Zones[i].Slug == z.Slug {
				g.Zones[i] = z
				return nil
			}
		}
		g.Zones = append(g.Zones, z)
	case "put_item":
		var it graph.Item
		if err := decodeStrict(op.Item, &it); err != nil {
			return fmt.Errorf("item: %w", err)
		}
		if it.Slug == "" {
			return fmt.Errorf("item needs a slug")
		}
		for i := range g.Items {
			if g.Items[i].Slug == it.Slug {
				g.Items[i] = it
				return nil
			}
		}
		g.Items = append(g.Items, it)
	case "delete_zone":
		for i := range g.Zones {
			if g.Zones[i].Slug == op.Slug {
				g.Zones = append(g.Zones[:i], g.Zones[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("no zone %q", op.Slug)
	case "put_theme":
		var th graph.Theme
		if err := decodeStrict(op.Theme, &th); err != nil {
			return fmt.Errorf("theme: %w", err)
		}
		if th.Slug == "" {
			return fmt.Errorf("theme needs a slug")
		}
		if op.Main {
			g.MainTheme = th.Slug
		}
		for i := range g.Themes {
			if g.Themes[i].Slug == th.Slug {
				g.Themes[i] = th
				return nil
			}
		}
		g.Themes = append(g.Themes, th)
	case "delete_theme":
		if op.Slug == g.MainTheme {
			return fmt.Errorf("theme %q is the main theme; make another one main first", op.Slug)
		}
		for _, n := range g.Nodes {
			if n.EffectCfg != nil && n.EffectCfg.Kind == graph.SetTheme && n.EffectCfg.Theme == op.Slug {
				return fmt.Errorf("theme %q is still switched to by %q", op.Slug, n.Slug)
			}
		}
		for i := range g.Themes {
			if g.Themes[i].Slug == op.Slug {
				g.Themes = append(g.Themes[:i], g.Themes[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("no theme %q", op.Slug)
	case "put_print_material":
		var pm graph.PrintMaterial
		if err := decodeStrict(op.Print, &pm); err != nil {
			return fmt.Errorf("print_material: %w", err)
		}
		if pm.ID == "" {
			return fmt.Errorf("print_material needs an id")
		}
		for i := range g.PrintMaterials {
			if g.PrintMaterials[i].ID == pm.ID {
				g.PrintMaterials[i] = pm
				return nil
			}
		}
		g.PrintMaterials = append(g.PrintMaterials, pm)
	case "delete_print_material":
		for i := range g.PrintMaterials {
			if g.PrintMaterials[i].ID == op.Slug {
				g.PrintMaterials = append(g.PrintMaterials[:i], g.PrintMaterials[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("no print material %q", op.Slug)
	case "connect":
		if op.From == "" || op.FromPort == "" || op.To == "" {
			return fmt.Errorf("from, from_port and to are required")
		}
		for _, slug := range []string{op.From, op.To} {
			if nodeIndex(g, slug) < 0 {
				return fmt.Errorf("no node %q", slug)
			}
		}
		disconnect(g, op.From, op.FromPort)
		g.Edges = append(g.Edges, graph.Edge{From: op.From, FromPort: op.FromPort, To: op.To})
	case "disconnect":
		if !disconnect(g, op.From, op.FromPort) {
			return fmt.Errorf("nothing leaves %s:%s", op.From, op.FromPort)
		}
	case "set_option":
		var o struct {
			HideScore       *bool `json:"hide_score"`
			JournalByUnlock *bool `json:"journal_by_unlock"`
			QuizPuzzles     *bool `json:"quiz_puzzles"`
			Tour            *bool `json:"tour"`
			MinPlayers      *int  `json:"min_players"`
			MaxPlayers      *int  `json:"max_players"`
		}
		if err := decodeStrict(op.Options, &o); err != nil {
			return fmt.Errorf("options: %w", err)
		}
		if o.HideScore == nil && o.JournalByUnlock == nil && o.QuizPuzzles == nil && o.Tour == nil && o.MinPlayers == nil && o.MaxPlayers == nil {
			return fmt.Errorf("options needs hide_score, journal_by_unlock, quiz_puzzles, tour, min_players or max_players")
		}
		if o.MinPlayers != nil {
			g.MinPlayers = *o.MinPlayers
		}
		if o.MaxPlayers != nil {
			g.MaxPlayers = *o.MaxPlayers
		}
		if o.HideScore != nil {
			g.HideScore = *o.HideScore
		}
		if o.JournalByUnlock != nil {
			g.JournalByUnlock = *o.JournalByUnlock
		}
		if o.QuizPuzzles != nil {
			g.QuizPuzzles = *o.QuizPuzzles
		}
		if o.Tour != nil {
			g.Tour = *o.Tour
		}
	default:
		return fmt.Errorf("unknown op")
	}
	return nil
}

func nodeIndex(g *graph.Graph, slug string) int {
	for i := range g.Nodes {
		if g.Nodes[i].Slug == slug {
			return i
		}
	}
	return -1
}

func disconnect(g *graph.Graph, from, port string) bool {
	found := false
	edges := g.Edges[:0]
	for _, e := range g.Edges {
		if e.From == from && e.FromPort == port {
			found = true
			continue
		}
		edges = append(edges, e)
	}
	g.Edges = edges
	return found
}

// nextPosition places a node the assistant added without coordinates to
// the right of everything else, so it does not land on top of another
// node when someone does open the designer.
func nextPosition(g *graph.Graph) (float64, float64) {
	if len(g.Nodes) == 0 {
		return 0, 0
	}
	maxX, y := g.Nodes[0].X, g.Nodes[0].Y
	for _, n := range g.Nodes[1:] {
		if n.X > maxX {
			maxX, y = n.X, n.Y
		}
	}
	return maxX + 280, y
}

func getCreatorPosition(ctx context.Context, s *Server, _ json.RawMessage) (ToolCallResult, error) {
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.GetMyPosition, "GET", "/api/admin/me/position", nil, nil, nil)
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	return adminCallResult(status, body), nil
}

func createGraphAdventure(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug        string          `json:"slug"`
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Language    string          `json:"language"`
		Brief       json.RawMessage `json:"brief"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errorResult("bad arguments: " + err.Error()), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	status, body, err := callAdmin(ctx, s.Admin.CreateAdventure, "POST", "/api/admin/adventures", nil, nil,
		map[string]string{"slug": a.Slug, "title": a.Title, "description": a.Description, "language": a.Language})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusCreated {
		return adminCallResult(status, body), nil
	}
	params := map[string]string{"slug": a.Slug}
	if len(a.Brief) > 0 {
		status, body, err = callAdmin(ctx, s.Admin.UpdateAdventure, "PATCH", "/api/admin/adventures/"+a.Slug, params, nil,
			map[string]json.RawMessage{"brief": a.Brief})
		if err != nil {
			return errorResult("call: " + err.Error()), nil
		}
		if status >= 300 {
			return errorResult(fmt.Sprintf("the adventure %q was created, but saving its brief failed: %s", a.Slug, body)), nil
		}
	}
	seed := graph.Graph{
		Nodes: []graph.Node{{Slug: "start", Type: graph.Start}, {Slug: "end", Type: graph.End, X: 560, EndCfg: &graph.EndConfig{Outcome: graph.Complete}}},
		Edges: []graph.Edge{{From: "start", FromPort: "out", To: "end"}},
	}
	status, body, err = callAdmin(ctx, s.Admin.PutAdventureGraph, "PUT", "/api/admin/adventures/"+a.Slug+"/graph", params, nil,
		graph.Envelope{Format: graph.FormatV2, Graph: seed})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	out, _ := json.Marshal(map[string]any{"slug": a.Slug, "nodes": []string{"start", "end"}})
	return textResult(string(out)), nil
}

// maxUndo is how many edits undo_adventure_graph_edit can step back.
const maxUndo = 30

// pushUndo records the draft as it was before an edit, keeping the
// latest maxUndo. The caller has already been authorised for the slug.
func (s *Server) pushUndo(ctx context.Context, slug string, before []byte) error {
	_, err := s.DB.ExecContext(ctx, `
		WITH adv AS (SELECT id FROM adventures WHERE slug = $1),
		ins AS (INSERT INTO adventure_graph_undo (adventure_id, graph) SELECT id, $2 FROM adv RETURNING adventure_id)
		DELETE FROM adventure_graph_undo WHERE adventure_id = (SELECT id FROM adv) AND id NOT IN (
			SELECT id FROM adventure_graph_undo WHERE adventure_id = (SELECT id FROM adv) ORDER BY id DESC LIMIT $3 - 1
		)
	`, slug, before, maxUndo)
	return err
}

func undoAdventureGraphEdit(ctx context.Context, s *Server, raw json.RawMessage) (ToolCallResult, error) {
	var a struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.Slug == "" {
		return errorResult("slug is required"), nil
	}
	if r := adminConfigured(s); r != nil {
		return *r, nil
	}
	// Loading the draft is what authorises the caller.
	if _, bad := s.loadDraftForEdit(ctx, a.Slug); bad != nil {
		return *bad, nil
	}
	var (
		id  int64
		doc []byte
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT u.id, u.graph FROM adventure_graph_undo u JOIN adventures a ON a.id = u.adventure_id
		WHERE a.slug = $1 ORDER BY u.id DESC LIMIT 1
	`, a.Slug).Scan(&id, &doc)
	if errors.Is(err, sql.ErrNoRows) {
		return errorResult("there is no edit to undo"), nil
	}
	if err != nil {
		return errorResult("loading undo: " + err.Error()), nil
	}
	var g graph.Graph
	if err := json.Unmarshal(doc, &g); err != nil {
		return errorResult("decoding undo: " + err.Error()), nil
	}
	status, body, err := callAdmin(ctx, s.Admin.PutAdventureGraph, "PUT", "/api/admin/adventures/"+a.Slug+"/graph",
		map[string]string{"slug": a.Slug}, nil, graph.Envelope{Format: graph.FormatV2, Graph: g})
	if err != nil {
		return errorResult("call: " + err.Error()), nil
	}
	if status != http.StatusOK {
		return adminCallResult(status, body), nil
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM adventure_graph_undo WHERE id = $1`, id); err != nil {
		return errorResult("the draft was restored, but forgetting the undone edit failed: " + err.Error()), nil
	}
	nodes := []string{}
	for _, n := range g.Nodes {
		nodes = append(nodes, n.Slug)
	}
	out, _ := json.Marshal(map[string]any{"restored": true, "nodes": nodes})
	return textResult(string(out)), nil
}
