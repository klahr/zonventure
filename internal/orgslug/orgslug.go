// Package orgslug owns the URL-safe org-slug generation rule used
// by the per-org store URLs (/o/{slug}/store, /o/{slug}/store/event/{id}).
//
// Sits in its own package because every site that mints an organization
// row (admin.CreateOrganization, admin.CreateOwnOrg, auth.signup,
// auth.oidc) needs the same helper, and `internal/admin` already
// imports `internal/auth` — keeping this in either of them would
// produce an import cycle.
//
// The DB-side migration (0051_org_slug.sql) carries an equivalent
// `zv_slugify` PL/pgSQL function used to backfill existing rows and
// as a BEFORE INSERT trigger fallback for raw test fixtures. The Go
// form here is the canonical, richer transliteration; the SQL form
// is "good enough" for a couple of dozen row backfills. Keep the
// two roughly aligned — drift is fine, but a slug computed by Go
// must remain valid by the constraint Validate enforces.
package orgslug

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxLen and MinLen bound the slug to a length that fits comfortably
// in a URL bar and is long enough to read at a glance. 30 keeps room
// in the URL for the rest of the path (`/store/event/{id}`); 3 stops
// "a" / "x" from collapsing the namespace and avoids collisions with
// short reserved words once we strip them.
const (
	MaxLen = 30
	MinLen = 3
)

// Reserved is the set of slug values the routing layer wants for
// itself or that read confusingly when prefixed with /o/. Enforced
// only at slug-write time (Validate); existing reserved-name orgs
// in the DB would have been backfilled with the SQL slugify which
// can't see this list, so a one-shot data audit is wise — but in
// practice a fresh deployment hits Validate before the trigger
// would have a chance to clash.
var Reserved = map[string]bool{
	"o":          true, // would collide with the /o/{slug} prefix itself
	"admin":      true,
	"api":        true,
	"auth":       true,
	"arrange":    true,
	"play":       true,
	"live":       true,
	"team":       true,
	"recap":      true,
	"store":      true,
	"health":     true,
	"version":    true,
	"webhooks":   true,
	"metrics":    true,
	"legal":      true,
	"pricing":    true,
	"manual":     true,
	"settings":   true,
	"static":     true,
	"assets":     true,
	"www":        true,
	"sitemap":    true,
	"robots":     true,
	"favicon":    true,
	"login":      true,
	"signup":     true,
	"signin":     true,
	"register":   true,
	"dashboard":  true,
	"events":     true,
	"adventures": true,
	"library":    true,
	"quests":     true,
	"logs":       true,
}

// validRE matches the canonical slug shape: lowercase ASCII letters,
// digits, and single hyphens between non-empty groups. Used by
// Validate as the post-transliteration acceptance test.
var validRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// transliterate folds common Latin / Nordic diacritics to ASCII so
// "Mårten" becomes "marten" rather than collapsing to "m-rten".
// Everything else (CJK, Cyrillic, emoji, punctuation) is mapped to
// a hyphen by the subsequent regexp pass — no attempt at a full
// Unicode → ASCII shim because the slugs are display-secondary
// (the org name + URL are still distinct). Operators with a
// non-Latin org name fall back to editing the slug manually in
// the Settings card.
var transliterate = strings.NewReplacer(
	"å", "a", "ä", "a", "ö", "o",
	"Å", "a", "Ä", "a", "Ö", "o",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"É", "e", "È", "e", "Ê", "e", "Ë", "e",
	"á", "a", "à", "a", "â", "a", "ã", "a",
	"Á", "a", "À", "a", "Â", "a", "Ã", "a",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"Í", "i", "Ì", "i", "Î", "i", "Ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o",
	"Ó", "o", "Ò", "o", "Ô", "o", "Õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"Ú", "u", "Ù", "u", "Û", "u", "Ü", "u",
	"ý", "y", "ÿ", "y", "Ý", "y", "Ÿ", "y",
	"ñ", "n", "Ñ", "n",
	"ç", "c", "Ç", "c",
	"ß", "ss",
	"æ", "ae", "Æ", "ae",
	"œ", "oe", "Œ", "oe",
	"ø", "o", "Ø", "o",
)

var collapseRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify produces a candidate slug from an arbitrary string. The
// output is guaranteed to satisfy Validate when the input contains
// at least one alphanumeric character; otherwise it returns "org"
// as a deterministic fallback, deferring uniqueness handling to
// the caller (Reserve).
func Slugify(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = transliterate.Replace(s)
	s = collapseRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "org"
	}
	if len(s) > MaxLen {
		s = strings.TrimRight(s[:MaxLen], "-")
	}
	if len(s) < MinLen {
		// Pad short slugs ("ab" → "ab-org") rather than rejecting,
		// so the auto-derive path always succeeds.
		s = s + "-org"
		if len(s) > MaxLen {
			s = s[:MaxLen]
		}
	}
	// Reserved-slug clash: "www", "api", "admin" etc. would Validate-
	// fail if returned verbatim. The auto-derive contract is "always
	// produces something the caller can save", so suffix to push it
	// out of the reserved namespace.
	if Reserved[s] {
		s = s + "-org"
		if len(s) > MaxLen {
			s = s[:MaxLen]
		}
	}
	return s
}

// Validate returns nil if slug is acceptable, or an error explaining
// what's wrong. Callers should hand the error message back to the
// frontend as-is — they're written for end-user consumption.
func Validate(slug string) error {
	if slug == "" {
		return errors.New("slug is required")
	}
	if len(slug) < MinLen {
		return fmt.Errorf("slug must be at least %d characters", MinLen)
	}
	if len(slug) > MaxLen {
		return fmt.Errorf("slug must be at most %d characters", MaxLen)
	}
	if !validRE.MatchString(slug) {
		return errors.New("slug must use lowercase letters, digits, and single hyphens (e.g. \"mindrace\" or \"team-alpha\")")
	}
	if Reserved[slug] {
		return fmt.Errorf("\"%s\" is reserved — pick a different slug", slug)
	}
	return nil
}

// querier abstracts *sql.DB / *sql.Tx for Reserve so the helper
// works inside or outside a transaction. Matches the surface
// `database/sql` exposes on both types without naming either.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Reserve picks a unique slug for `name`, retrying with -2/-3/… on
// collision against the existing organizations.slug column. The
// caller passes the same querier they're holding open for the
// org-INSERT — running both inside one transaction means the
// uniqueness check and the eventual INSERT see a consistent view
// of the table.
//
// Returns the chosen slug, or an error after 9999 attempts (which
// indicates real corruption — the bound is theatre).
func Reserve(ctx context.Context, q querier, name string) (string, error) {
	base := Slugify(name)
	candidate := base
	// If the auto-derived candidate trips the reserved list (e.g.
	// the name slugified to "store"), nudge it once with a "-org"
	// suffix before the uniqueness loop — the loop only handles
	// collisions, not validity.
	if Reserved[candidate] {
		candidate = candidate + "-org"
		if len(candidate) > MaxLen {
			candidate = candidate[:MaxLen]
		}
		base = candidate
	}
	for n := 1; n < 10000; n++ {
		var exists bool
		err := q.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM organizations WHERE slug = $1)`, candidate).Scan(&exists)
		if err != nil {
			return "", fmt.Errorf("orgslug: collision check: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, n+1)
		if len(candidate) > MaxLen {
			// Keep the suffix readable by trimming from the base
			// rather than the tail.
			suffix := fmt.Sprintf("-%d", n+1)
			head := base[:MaxLen-len(suffix)]
			head = strings.TrimRight(head, "-")
			candidate = head + suffix
		}
	}
	return "", errors.New("orgslug: could not find unique slug after 9999 attempts")
}
