package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/graph/look"
)

// The looks' pictures and typefaces for the server-rendered pages
// (store, recap, live board). They ship once, with the player: Vite
// copies web/packages/play-ui's covers and fonts into the play app's
// build under hashed names, and its manifest says which is which. These
// pages link those files rather than embedding second copies.

const playUISrc = "../../packages/play-ui/src/"

var (
	playManifestOnce sync.Once
	playManifest     map[string]string
)

// playAsset is the URL of a play-ui source file (relative to its src,
// e.g. "covers/party-800.webp") in the play app's build, or "" when the
// build doesn't have it (no `make web` yet, or no such file).
func playAsset(src string) string {
	playManifestOnce.Do(func() {
		playManifest = map[string]string{}
		b, err := spaFS.ReadFile("web/dist/play/.vite/manifest.json")
		if err != nil {
			return
		}
		var m map[string]struct {
			File string `json:"file"`
		}
		if json.Unmarshal(b, &m) != nil {
			return
		}
		for k, v := range m {
			if strings.HasPrefix(k, playUISrc) {
				playManifest[strings.TrimPrefix(k, playUISrc)] = "/app/play/" + v.File
			}
		}
	})
	return playManifest[src]
}

// themeCoverURL is the cover picture of starter theme id, 800px wide —
// what the store cards, recap and live board show. "" for none.
func themeCoverURL(id string) string {
	if id == "" || strings.ContainsAny(id, "/.") {
		return ""
	}
	return playAsset("covers/" + id + "-800.webp")
}

// lookFontURL is the stylesheet that loads look font key, or "" when the
// key isn't one of look.Fonts or the build lacks its file.
func lookFontURL(key string) string {
	if _, ok := look.Fonts[key]; !ok || playAsset("fonts/"+fontStem(key)+"-latin.woff2") == "" {
		return ""
	}
	return "/assets/look-font/" + key + ".css"
}

// fontStem is font key's file name in play-ui/src/fonts.
func fontStem(key string) string {
	if key == "baloo-2" {
		return "baloo2"
	}
	return key
}

// The unicode ranges of the latin and latin-ext subsets, as in
// play-ui/src/fonts/adventure-fonts.css.
const (
	latinRange    = "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"
	latinExtRange = "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF"
)

// serveLookFont is GET /assets/look-font/{key}.css: the @font-face rules
// of one look font, pointing at the play app's copies.
func serveLookFont(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSuffix(chi.URLParam(r, "file"), ".css")
	family, ok := look.Fonts[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	var b strings.Builder
	for _, sub := range []struct{ suffix, rng string }{{"-latin.woff2", latinRange}, {"-latin-ext.woff2", latinExtRange}} {
		u := playAsset("fonts/" + fontStem(key) + sub.suffix)
		if u == "" {
			continue
		}
		b.WriteString("@font-face {\n  font-family: \"" + family + "\";\n  font-style: normal;\n  font-weight: 400 700;\n  font-display: swap;\n")
		b.WriteString("  src: url(\"" + u + "\") format(\"woff2\");\n  unicode-range: " + sub.rng + ";\n}\n")
	}
	if b.Len() == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(b.String()))
}
