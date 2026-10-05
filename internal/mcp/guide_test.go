package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klahr/zonventure/internal/graph"
	"github.com/klahr/zonventure/internal/graph/valid"
)

// The guide's recipes are what the model copies, so they must be exactly
// what the server accepts: strict parsing, and no validation errors.
func TestGuideExampleIsValid(t *testing.T) {
	env, err := graph.ParseEnvelope([]byte(guideExample))
	if err != nil {
		t.Fatal(err)
	}
	if r := valid.Check(&env.Graph); r.HasErrors() {
		t.Fatalf("guide example has errors: %+v", r.Issues)
	}
	doc := authoringGuide()
	for _, want := range []string{`"slug": "intro"`, `"slug": "fish"`, `"slug": "year"`, `"slug": "end"`, `"radius_m": 30`} {
		if !strings.Contains(doc, want) {
			t.Errorf("guide is missing %s", want)
		}
	}
	if strings.Contains(doc, "%!") {
		t.Error("guide has a formatting error")
	}
}

// Every tool the instructions and guide tell the model to call exists.
func TestGuideNamesRealTools(t *testing.T) {
	s := New(nil, "", nil, nil)
	text := serverInstructions + authoringGuide()
	for _, name := range []string{"get_authoring_guide", "create_graph_adventure", "get_adventure", "get_adventure_graph",
		"edit_adventure_graph", "undo_adventure_graph_edit", "simulate_adventure", "upload_adventure_media", "get_media_upload_url", "update_adventure_meta",
		"create_dev_ticket", "get_dev_screen", "dev_go_to", "dev_reset", "get_creator_position"} {
		if !strings.Contains(text, name) {
			t.Errorf("%s is not mentioned", name)
		}
		if _, ok := s.tools.handlers[name]; !ok {
			t.Errorf("%s is mentioned but not registered", name)
		}
	}
}

func TestInitializeAndPrompts(t *testing.T) {
	s := New(nil, "", nil, nil)
	ctx := context.Background()
	resp, _ := s.dispatch(ctx, Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"})
	init, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(init), `"instructions":"Zonventure builds`) || !strings.Contains(string(init), `"prompts":{}`) {
		t.Errorf("initialize: %s", init)
	}

	resp, _ = s.dispatch(ctx, Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "prompts/list"})
	if list, _ := json.Marshal(resp.Result); !strings.Contains(string(list), `"name":"field_session"`) {
		t.Errorf("prompts/list: %s", list)
	}

	resp, _ = s.dispatch(ctx, Request{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "prompts/get",
		Params: json.RawMessage(`{"name":"field_session","arguments":{"slug":"harbour"}}`)})
	if got, _ := json.Marshal(resp.Result); resp.Error != nil || !strings.Contains(string(got), `\"harbour\"`) {
		t.Errorf("prompts/get: %s %+v", got, resp.Error)
	}

	resp, _ = s.dispatch(ctx, Request{JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "prompts/get", Params: json.RawMessage(`{"name":"nope"}`)})
	if resp.Error == nil {
		t.Error("an unknown prompt should be an error")
	}
}
