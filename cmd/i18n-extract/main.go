// Command i18n-extract pulls translatable strings out of the two
// places they live today — the embedded JS dictionary in
// internal/server/web/i18n.js and the landingStrings struct map in
// internal/server/landing.go — and writes them as JSON files under
// internal/server/web/locales/. Those JSON files are the source of
// truth a Crowdin (or any other) translation pipeline reads from
// and writes back to.
//
// Run once to bootstrap the locales/ tree, then run again to refresh
// (e.g. after pulling a new sv.json from Crowdin and you want to
// reconcile against the old hardcoded values). The extractor is
// idempotent — same input, same output, sorted keys, deterministic
// formatting.
//
// Usage:
//
//	go run ./cmd/i18n-extract              # writes into the working tree
//	go run ./cmd/i18n-extract /path/to/repo
//
// Output layout:
//
//	internal/server/web/locales/
//	├── web/      — keys = English source strings; values = translations
//	│   ├── en.json
//	│   └── sv.json
//	└── landing/  — keys = struct-field IDs; values = translations
//	    ├── en.json
//	    └── sv.json
//
// The web/en.json case is identity (key == value) because the
// runtime's t() function returns the key when no translation is
// found — the file exists so Crowdin has something to declare as
// the source language. Crowdin keys-only mode would also work but
// flat key→value JSON is the most universally portable shape.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatalf("resolve root: %v", err)
	}

	webDicts, err := extractWebDicts(filepath.Join(root, "internal/server/web/i18n.js"))
	if err != nil {
		log.Fatalf("web extract: %v", err)
	}
	if err := writeWebJSONs(root, webDicts); err != nil {
		log.Fatalf("write web JSONs: %v", err)
	}

	landingDicts, err := extractLandingDicts(filepath.Join(root, "internal/server/landing.go"))
	if err != nil {
		log.Fatalf("landing extract: %v", err)
	}
	if err := writeLandingJSONs(root, landingDicts); err != nil {
		log.Fatalf("write landing JSONs: %v", err)
	}

	for lang, m := range webDicts {
		fmt.Printf("web/%s.json — %d entries\n", lang, len(m))
	}
	for lang, m := range landingDicts {
		fmt.Printf("landing/%s.json — %d entries\n", lang, len(m))
	}
}

/* ----------------------------- web (i18n.js) ----------------------------- */

// extractWebDicts walks i18n.js, finds the DICT object literal, and
// returns one map[key]value per language entry. Empty languages
// (the source `en: {}` case) come back as empty maps rather than
// being elided so the caller still emits an `en.json`.
func extractWebDicts(path string) (map[string]map[string]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks, err := tokenize(string(src))
	if err != nil {
		return nil, fmt.Errorf("tokenize %s: %w", path, err)
	}
	return parseDict(toks)
}

// writeWebJSONs persists the per-language maps. The English source
// file is synthesised when the input dict is empty (the JS DICT has
// `en: {}` — every key in any other language is an English source
// string). en.json keeps key→identity-value so Crowdin's source-
// language model has something concrete to import.
func writeWebJSONs(root string, dicts map[string]map[string]string) error {
	dir := filepath.Join(root, "internal/server/web/locales/web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Synthesise English from the union of all foreign keys.
	if len(dicts["en"]) == 0 {
		en := map[string]string{}
		for lang, m := range dicts {
			if lang == "en" {
				continue
			}
			for k := range m {
				en[k] = k
			}
		}
		dicts["en"] = en
	}
	for lang, m := range dicts {
		if err := writeJSON(filepath.Join(dir, lang+".json"), m); err != nil {
			return err
		}
	}
	return nil
}

/* --------------------------- landing (landing.go) --------------------------- */

// extractLandingDicts uses go/parser on landing.go and walks the
// `landingLocales` map literal. Each entry yields a map of struct
// field name → string value. Field values that are concatenations
// (`"a" + "b"`) get folded into a single string. Other expression
// shapes are not expected in this file; if any appear, we panic
// loudly rather than silently dropping content.
func extractLandingDicts(path string) (map[string]map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := map[string]map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "landingLocales" {
					continue
				}
				if i >= len(vs.Values) {
					return nil, fmt.Errorf("landingLocales has no initialiser")
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("landingLocales is not a composite literal")
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						return nil, fmt.Errorf("landingLocales entry is not a key-value pair")
					}
					keyLit, ok := kv.Key.(*ast.BasicLit)
					if !ok || keyLit.Kind != token.STRING {
						return nil, fmt.Errorf("landingLocales key is not a string literal")
					}
					lang, err := strconv.Unquote(keyLit.Value)
					if err != nil {
						return nil, fmt.Errorf("unquote lang key: %w", err)
					}
					valLit, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						return nil, fmt.Errorf("landingLocales[%q] is not a struct literal", lang)
					}
					fields := map[string]string{}
					for _, fe := range valLit.Elts {
						fkv, ok := fe.(*ast.KeyValueExpr)
						if !ok {
							return nil, fmt.Errorf("landingLocales[%q] field is not key-value", lang)
						}
						ident, ok := fkv.Key.(*ast.Ident)
						if !ok {
							return nil, fmt.Errorf("landingLocales[%q] field key is not an ident", lang)
						}
						val, err := evalStringExpr(fkv.Value)
						if err != nil {
							return nil, fmt.Errorf("landingLocales[%q].%s: %w", lang, ident.Name, err)
						}
						fields[ident.Name] = val
					}
					out[lang] = fields
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("landingLocales not found in %s", path)
	}
	return out, nil
}

// evalStringExpr folds a Go expression that's expected to be a
// string-typed compile-time constant — string literal or string
// concatenation — into a runtime string. Anything else (function
// call, identifier reference, sprintf) returns an error so the
// caller can surface the offending field rather than silently
// emit "".
func evalStringExpr(e ast.Expr) (string, error) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", fmt.Errorf("non-string basic literal: %s", v.Value)
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", fmt.Errorf("unquote: %w", err)
		}
		return s, nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", fmt.Errorf("unsupported binary op: %s", v.Op)
		}
		l, err := evalStringExpr(v.X)
		if err != nil {
			return "", err
		}
		r, err := evalStringExpr(v.Y)
		if err != nil {
			return "", err
		}
		return l + r, nil
	case *ast.ParenExpr:
		return evalStringExpr(v.X)
	}
	return "", fmt.Errorf("unsupported expression type %T", e)
}

func writeLandingJSONs(root string, dicts map[string]map[string]string) error {
	dir := filepath.Join(root, "internal/server/web/locales/landing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for lang, m := range dicts {
		if err := writeJSON(filepath.Join(dir, lang+".json"), m); err != nil {
			return err
		}
	}
	return nil
}

/* ----------------------------- shared ----------------------------- */

// writeJSON marshals m with sorted keys and a trailing newline so the
// file diffs cleanly across runs and the working-tree always
// reproduces from the same input.
func writeJSON(path string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]struct {
		Key string
		Val string
	}, len(keys))
	for i, k := range keys {
		ordered[i].Key = k
		ordered[i].Val = m[k]
	}

	// json.Marshal on a map randomises key order; build the output
	// by hand so the bytes are deterministic. Crowdin diffs will
	// otherwise look noisy on every regeneration.
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, kv := range ordered {
		k, err := json.Marshal(kv.Key)
		if err != nil {
			return err
		}
		v, err := json.Marshal(kv.Val)
		if err != nil {
			return err
		}
		sb.Write(k)
		sb.WriteString(": ")
		sb.Write(v)
		if i < len(ordered)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("}\n")
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

/* --------------------- JS tokenizer + dict parser --------------------- */
//
// i18n.js is structured JS but we don't want to drag in a JS engine
// for a one-shot build tool. A minimal tokenizer that knows about
// strings (single, double and backtick), comments (line + block) and
// punctuation is enough for the DICT shape: { lang: { 'k': 'v', ...
// }, ... }. Everything else (assignments, function bodies, IIFE
// wrapper) is parsed as Idents/Other and skipped.

type tokKind int

const (
	tEOF tokKind = iota
	tLBrace
	tRBrace
	tColon
	tComma
	tString
	tIdent
)

type tok struct {
	kind tokKind
	val  string
}

func tokenize(src string) ([]tok, error) {
	var out []tok
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			// line comment to EOL
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '{':
			out = append(out, tok{tLBrace, "{"})
			i++
		case c == '}':
			out = append(out, tok{tRBrace, "}"})
			i++
		case c == ':':
			out = append(out, tok{tColon, ":"})
			i++
		case c == ',':
			out = append(out, tok{tComma, ","})
			i++
		case c == '"' || c == '\'' || c == '`':
			s, n, err := scanString(src, i, c)
			if err != nil {
				return nil, err
			}
			out = append(out, tok{tString, s})
			i += n
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentCont(src[j]) {
				j++
			}
			out = append(out, tok{tIdent, src[i:j]})
			i = j
		default:
			// Unrecognised single byte: skip. The tokenizer doesn't need
			// to model every JS punctuation — operators, parens etc.
			// don't appear inside the DICT region we care about.
			i++
		}
	}
	out = append(out, tok{tEOF, ""})
	return out, nil
}

// scanString reads a JS string literal starting at src[start] (which
// must be the opening quote of kind quote). Returns the unescaped
// content and the total byte length consumed including both quotes.
func scanString(src string, start int, quote byte) (string, int, error) {
	var sb strings.Builder
	i := start + 1
	for i < len(src) {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			n := src[i+1]
			switch n {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case '0':
				sb.WriteByte(0)
			case '\\', '"', '\'', '`', '/':
				sb.WriteByte(n)
			case '\n':
				// JS line-continuation: backslash-newline disappears.
			case 'u':
				// \uXXXX or \u{XXXXXX}. Decode hex into a rune.
				if i+2 < len(src) && src[i+2] == '{' {
					end := strings.IndexByte(src[i+3:], '}')
					if end < 0 {
						return "", 0, fmt.Errorf("unterminated \\u{} escape at %d", i)
					}
					v, err := strconv.ParseUint(src[i+3:i+3+end], 16, 32)
					if err != nil {
						return "", 0, fmt.Errorf("bad \\u{} escape at %d: %w", i, err)
					}
					sb.WriteRune(rune(v))
					i += 3 + end + 1
					continue
				}
				if i+5 >= len(src) {
					return "", 0, fmt.Errorf("truncated \\u escape at %d", i)
				}
				v, err := strconv.ParseUint(src[i+2:i+6], 16, 32)
				if err != nil {
					return "", 0, fmt.Errorf("bad \\u escape at %d: %w", i, err)
				}
				sb.WriteRune(rune(v))
				i += 6
				continue
			default:
				// Unknown escape — JS tolerates it as the literal
				// character. Mirror that.
				sb.WriteByte(n)
			}
			i += 2
			continue
		}
		if c == quote {
			return sb.String(), i - start + 1, nil
		}
		sb.WriteByte(c)
		i++
	}
	return "", 0, fmt.Errorf("unterminated string starting at %d", start)
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$'
}
func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// parseDict locates the DICT identifier in the token stream, skips
// to its `{`, and walks each language entry. Returns the per-
// language map[key]value table.
func parseDict(toks []tok) (map[string]map[string]string, error) {
	i := 0
	for i < len(toks) {
		if toks[i].kind == tIdent && toks[i].val == "DICT" {
			break
		}
		i++
	}
	if i >= len(toks) {
		return nil, fmt.Errorf("DICT identifier not found")
	}
	for i < len(toks) && toks[i].kind != tLBrace {
		i++
	}
	if i >= len(toks) {
		return nil, fmt.Errorf("DICT initialiser { not found")
	}
	i++ // consume {

	out := map[string]map[string]string{}
	for i < len(toks) && toks[i].kind != tRBrace {
		if toks[i].kind == tComma {
			i++
			continue
		}
		// Lang key — JS object keys can be either bareword or quoted.
		var lang string
		switch toks[i].kind {
		case tIdent, tString:
			lang = toks[i].val
		default:
			return nil, fmt.Errorf("expected lang key at token %d (kind=%d val=%q)", i, toks[i].kind, toks[i].val)
		}
		i++
		if i >= len(toks) || toks[i].kind != tColon {
			return nil, fmt.Errorf("expected : after lang key %q", lang)
		}
		i++
		if i >= len(toks) || toks[i].kind != tLBrace {
			return nil, fmt.Errorf("expected { after lang %q", lang)
		}
		i++
		entries := map[string]string{}
		for i < len(toks) && toks[i].kind != tRBrace {
			if toks[i].kind == tComma {
				i++
				continue
			}
			if toks[i].kind != tString {
				return nil, fmt.Errorf("expected key string in %s, got token %d (kind=%d val=%q)", lang, i, toks[i].kind, toks[i].val)
			}
			key := toks[i].val
			i++
			if i >= len(toks) || toks[i].kind != tColon {
				return nil, fmt.Errorf("expected : after key %q", key)
			}
			i++
			if i >= len(toks) || toks[i].kind != tString {
				return nil, fmt.Errorf("expected value string for key %q in %s", key, lang)
			}
			entries[key] = toks[i].val
			i++
		}
		if i < len(toks) && toks[i].kind == tRBrace {
			i++ // consume closing }
		}
		out[lang] = entries
	}
	return out, nil
}
