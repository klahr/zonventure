package server_test

import (
	"testing"

	"github.com/klahr/zonventure/internal/graph"
)

// Removing a language with translated texts takes the password; adding
// one, editing its texts or dropping one that has none does not.
func TestGraphRemoveLanguageNeedsPassword(t *testing.T) {
	ts, _, cleanup := newTestServer(t)
	defer cleanup()

	pub := newClient(t, ts)
	if s := pub.do("POST", "/api/auth/login", map[string]string{"username": "pub", "password": "pubpw"}, nil); s != 200 {
		t.Fatalf("login = %d", s)
	}
	save := func(tr map[string]map[string]string, password string) int {
		g := graph.Graph{
			Nodes:        []graph.Node{{Slug: "start", Type: graph.Start}},
			Translations: tr,
		}
		env := graph.Envelope{Format: graph.FormatV2, Graph: g}
		if password != "" {
			return pub.doWithConfirm("PUT", "/api/admin/adventures/demo/graph", password, env, nil)
		}
		return pub.doAny("PUT", "/api/admin/adventures/demo/graph", env, nil)
	}

	if s := save(map[string]map[string]string{"en": {}, "de": {}}, ""); s != 200 {
		t.Fatalf("adding languages = %d", s)
	}
	if s := save(map[string]map[string]string{"en": {"meta:title": "Title"}}, ""); s != 200 {
		t.Fatalf("dropping a language without texts = %d", s)
	}
	if s := save(map[string]map[string]string{"en": {"meta:title": "The title"}}, ""); s != 200 {
		t.Fatalf("editing a text = %d", s)
	}
	if s := save(nil, ""); s != 401 {
		t.Fatalf("removing a language without the password = %d, want 401", s)
	}
	if s := save(nil, "wrong"); s != 401 {
		t.Fatalf("removing a language with a wrong password = %d, want 401", s)
	}
	if s := save(nil, "pubpw"); s != 200 {
		t.Fatalf("removing a language with the password = %d", s)
	}
}
