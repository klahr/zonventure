# Locales

Translatable strings live as flat JSON files in this tree. The Go
binary embeds them via `//go:embed` and the runtime resolves keys at
request time. There are two domains:

- `web/` — strings in `internal/server/web/i18n.js`'s `t()` function
  (the SPA: arrange, play, live). Keys are the **English source
  strings** themselves; values are the per-locale translation. A key
  with no entry in a given locale falls through to the key (= the
  English source).
- `landing/` — strings in the server-rendered marketing/store pages
  (landing, store, store-event, pricing). Keys are **struct-field
  IDs** from `landingStrings` (e.g. `HeroHeading`, `StoreCardCTA`);
  values are the per-locale text.

## Crowdin upload / download

Crowdin treats `*/en.json` as the **source** files. Translations land
as `*/<lang>.json` (e.g. `web/de.json`).

Project structure for Crowdin:

```
File path on Crowdin           | Source path in repo
------------------------------ | --------------------------------------
/web/%locale%.json             | internal/server/web/locales/web/en.json
/landing/%locale%.json         | internal/server/web/locales/landing/en.json
```

For both files, set the **format** to `JSON`, **source language** to
English, and **placeholders** to `{{name}}` so Crowdin doesn't try to
translate the variable interpolation tokens.

The web/ keys are full English sentences — translators reading the
key already see the source. Landing keys are short IDs, so leave
"description" or similar Crowdin context fields pointing at the
struct definition in `internal/server/landing.go` for translator
reference.

## Refreshing the source files

The JSON files are derived from the legacy hardcoded dictionaries in
`internal/server/web/i18n.js` and `internal/server/landing.go`. The
generator lives at `cmd/i18n-extract`:

```
go run ./cmd/i18n-extract
```

Idempotent — running it twice with no source change produces no diff.
Once the runtime has been switched to consume these JSON files
directly (see CHANGELOG), the hardcoded dictionaries can be retired
and the extractor goes away.

## Adding a new language

Either:

1. **Via Crowdin** — add the language in the Crowdin project and let
   translators do the work. New `<lang>.json` files appear under each
   domain when you pull translations.
2. **By hand** — copy `web/en.json` to `web/<lang>.json` and translate
   the values; same for `landing/`. The runtime auto-discovers any
   locale file matching the embed pattern.

The HTML `lang=` attribute and the locale picker pull the available
language list from the file set; no code change required to add a
locale.
