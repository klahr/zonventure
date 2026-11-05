package listing

import (
	"slices"
	"testing"
)

func TestListingIn(t *testing.T) {
	l, err := Scan("sv",
		[]byte(`{"en": {"title": "The Treasure", "description": "  ", "area": "", "start_label": "By the oak"}, "de": {}}`),
		[]byte(`{"en": {"name": "Treasure hunt"}, "fi": {"name": "Aarre"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Languages(); !slices.Equal(got, []string{"sv", "de", "en"}) {
		t.Errorf("languages = %v", got)
	}
	main := Texts{Name: "Skatten", Title: "Skatten", Description: "Hitta den", StartLabel: "Vid eken", Area: "Gamla stan"}
	want := Texts{Name: "Treasure hunt", Title: "The Treasure", Description: "Hitta den", StartLabel: "By the oak", Area: "Gamla stan"}
	if got := l.In("en", main); got != want {
		t.Errorf("in English = %+v, want %+v", got, want)
	}
	// The main language, a language the adventure is not played in (the
	// listing's own Finnish is never shown), and none asked for.
	for _, lang := range []string{"sv", "fi", ""} {
		if got := l.In(lang, main); got != main {
			t.Errorf("in %q = %+v, want the main texts", lang, got)
		}
	}
	// No main text, no translation: a name the listing does not have
	// is not made up from its English one.
	if got := l.In("en", Texts{Title: "Skatten"}); got.Name != "" {
		t.Errorf("name in English = %q, want none", got.Name)
	}
	// The adventure's description, translated with it.
	d, err := Scan("sv", []byte(`{"en": {"description": "Find it"}}`), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.In("en", Texts{Description: "Hitta den"}).Description; got != "Find it" {
		t.Errorf("description in English = %q, want the adventure's translation", got)
	}
	if got := Languages("sv", nil); got != nil {
		t.Errorf("languages of one = %v, want nil", got)
	}
}
