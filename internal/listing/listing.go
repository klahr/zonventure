// Package listing is what an event says in each language its adventure
// is played in: on its store listing, in its emails and on its pages.
//
// The event's own words — its name, store blurb and meeting place — are
// written in the console, in the adventure's main language and its
// translations (events.translations); a self-guided event's are
// reviewed with its listing, and the approved copy keeps them
// (event_listings.translations). The adventure's words — its title,
// meeting point, area and note to buyers — are translations in the
// adventure version the event plays, under the graph's meta keys,
// reviewed with that version.
//
// An event speaks the languages its adventure version does: the main
// one, then those it has translations for. A translation of the
// event's own words into a language the adventure is not played in is
// kept but never shown.
package listing

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Texts are an event's words in one language. Empty is "as in the main
// language".
type Texts struct {
	// The event's own.
	Name         string `json:"name,omitempty"`
	Blurb        string `json:"blurb,omitempty"`
	MeetingPlace string `json:"meeting_place,omitempty"`

	// The adventure's: graph.MetaKey title, start_label, area and
	// buyer_note.
	Title      string `json:"title,omitempty"`
	StartLabel string `json:"start_label,omitempty"`
	Area       string `json:"area,omitempty"`
	BuyerNote  string `json:"buyer_note,omitempty"`
}

// Translations are an event's own words in other languages, by
// language tag: Name, Blurb and MeetingPlace only.
type Translations map[string]Texts

// Columns selects, for a listing l, the adventure version g it plays
// and its adventure a, what Scan reads: the version's main language
// (the adventure's, for a version saved before graphs kept it), its
// store translations by language, and the listing's own.
const Columns = `COALESCE(NULLIF(g.graph->>'language', ''), a.language, ''),
	COALESCE((SELECT jsonb_object_agg(t.key, jsonb_build_object(
	    'title', t.value->>'meta:title', 'start_label', t.value->>'meta:start_label', 'area', t.value->>'meta:area',
	    'buyer_note', t.value->>'meta:buyer_note'))
	  FROM jsonb_each(CASE WHEN jsonb_typeof(g.graph->'translations') = 'object' THEN g.graph->'translations' ELSE '{}' END) t), '{}'),
	COALESCE(l.translations, '{}')`

// Listing is a listing's languages and its words in each.
type Listing struct {
	main      string
	adventure map[string]Texts
	own       Translations
}

// Scan reads what Columns selected.
func Scan(main string, adventure, own []byte) (Listing, error) {
	l := Listing{main: main}
	if err := json.Unmarshal(adventure, &l.adventure); err != nil {
		return Listing{}, err
	}
	if err := json.Unmarshal(own, &l.own); err != nil {
		return Listing{}, err
	}
	return l, nil
}

// Languages are the languages the listing's adventure is played in: the
// main one first. Nil when it has no translations, as
// graph.Graph.Languages.
func (l Listing) Languages() []string {
	translated := make([]string, 0, len(l.adventure))
	for lang := range l.adventure {
		translated = append(translated, lang)
	}
	return Languages(l.main, translated)
}

// Languages are a version's languages from its main language and those
// it has translations for: the main one first, the rest in order. Nil
// with none but the main one, as graph.Graph.Languages.
func Languages(main string, translated []string) []string {
	var extra []string
	for _, lang := range translated {
		if lang != main && !slices.Contains(extra, lang) {
			extra = append(extra, lang)
		}
	}
	if len(extra) == 0 || main == "" {
		return nil
	}
	sort.Strings(extra)
	return append([]string{main}, extra...)
}

// VersionLanguages selects, for an adventure version (its alias g) of an
// adventure (alias a), what Languages takes: its main language, and the
// languages it has translations for as a JSON array.
func VersionLanguages(g, a string) string {
	return `COALESCE(NULLIF(` + g + `.graph->>'language', ''), ` + a + `.language, ''),
	COALESCE((SELECT jsonb_agg(k) FROM jsonb_object_keys(CASE WHEN jsonb_typeof(` + g + `.graph->'translations') = 'object' THEN ` + g + `.graph->'translations' ELSE '{}' END) k), '[]')`
}

// In is main in lang: each of its texts translated where lang has a
// translation, as it is elsewhere.
func (l Listing) In(lang string, main Texts) Texts {
	if lang == "" || lang == l.main || !slices.Contains(l.Languages(), lang) {
		return main
	}
	or := func(tr, was string) string {
		if s := strings.TrimSpace(tr); s != "" && was != "" {
			return s
		}
		return was
	}
	a, o := l.adventure[lang], l.own[lang]
	return Texts{
		Name:         or(o.Name, main.Name),
		Blurb:        or(o.Blurb, main.Blurb),
		MeetingPlace: or(o.MeetingPlace, main.MeetingPlace),
		Title:        or(a.Title, main.Title),
		StartLabel:   or(a.StartLabel, main.StartLabel),
		Area:         or(a.Area, main.Area),
		BuyerNote:    or(a.BuyerNote, main.BuyerNote),
	}
}
