package server

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// renderStoreEventPage serves /store/event/{event_id} — the per-
// game detail page paired with the listing at /store. Returns 404
// for events that aren't currently public_for_sale + priced + in
// running/paused state, so a stale link doesn't expose past prices
// or events the operator has unlisted.
//
// Substitution shape mirrors renderLandingPage / renderTopbarPage:
// the template is plain HTML with {{KEY}} placeholders, swapped via
// strings.ReplaceAll. Per-event values are HTML-escaped where they
// land inside text content (blurb, names) or attribute values
// (cover URL). Operators who want a richer / branded variant drop
// their own store-event.html into ZV_WEB_OVERRIDE_DIR; the embedded
// brand-neutral version is shadowed.
func renderStoreEventPage(w http.ResponseWriter, r *http.Request, db *sql.DB, publicURL string) {
	idStr := chi.URLParam(r, "event_id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		servePublicPage("web/404.html", http.StatusNotFound)(w, r)
		return
	}

	body, err := readWebFile("web/store-event.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var (
		eventName  sql.NullString
		advSlug    string
		advTitle   sql.NullString
		orgName    string
		blurb      string
		priceCents sql.NullInt64
		currency   string
		teamSize   int
		startLabel sql.NullString
		hasCover   bool
	)
	err = db.QueryRowContext(r.Context(), `
		SELECT e.name, e.adventure_slug,
		       a.title AS adv_title,
		       o.name AS org_name,
		       e.public_blurb,
		       e.ticket_price_cents,
		       e.ticket_currency,
		       e.ticket_team_size,
		       a.start_label,
		       (e.cover_image_mime_type IS NOT NULL) AS has_cover
		FROM events e
		JOIN organizations o ON o.id = e.org_id
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		WHERE e.id = $1
		  AND e.public_for_sale
		  AND e.ticket_price_cents IS NOT NULL
		  AND e.state IN ('running', 'paused')
	`, id).Scan(&eventName, &advSlug, &advTitle, &orgName, &blurb, &priceCents,
		&currency, &teamSize, &startLabel, &hasCover)
	if errors.Is(err, sql.ErrNoRows) {
		servePublicPage("web/404.html", http.StatusNotFound)(w, r)
		return
	}
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}

	// Fall through to slug for the title when the adventure row
	// has no human name; the slug is at worst kebab-case which is
	// readable enough for the rare case the row was renamed away.
	title := strings.TrimSpace(advSlug)
	if advTitle.Valid && strings.TrimSpace(advTitle.String) != "" {
		title = strings.TrimSpace(advTitle.String)
	}
	subtitle := ""
	if eventName.Valid && strings.TrimSpace(eventName.String) != "" &&
		!strings.EqualFold(strings.TrimSpace(eventName.String), title) {
		subtitle = strings.TrimSpace(eventName.String)
	}

	priceMajor := ""
	if priceCents.Valid && priceCents.Int64 > 0 {
		// Drop trailing zero cents for whole-currency amounts so
		// "25 €" reads better than "25.00 €" on hero copy. Stripe
		// itself tolerates either side: this is a display choice.
		amount := float64(priceCents.Int64) / 100
		if priceCents.Int64%100 == 0 {
			priceMajor = fmt.Sprintf("%d", int(amount))
		} else {
			priceMajor = fmt.Sprintf("%.2f", amount)
		}
	}
	priceDisplay := formatStorePrice(priceCents.Int64, currency)

	coverURL := ""
	if hasCover {
		coverURL = "/store-cover/event/" + strconv.FormatInt(id, 10)
	}

	teamSizeStr := strconv.Itoa(teamSize)
	teamLabel := "Solo"
	if teamSize > 1 {
		teamLabel = fmt.Sprintf("Up to %d players", teamSize)
	}

	// Localised topbar chrome shared with /landing and /pricing —
	// the lang-picker, brand link, footer copy. The page body's
	// English copy is intentionally not yet localised; that will
	// follow once the store has more visitors than translators.
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	subs := topbarSubs(s, lang)

	subs = append(subs,
		[2]string{"{{TITLE}}", html.EscapeString(title)},
		[2]string{"{{SUBTITLE}}", html.EscapeString(subtitle)},
		[2]string{"{{ORG_NAME}}", html.EscapeString(orgName)},
		[2]string{"{{BLURB}}", html.EscapeString(strings.TrimSpace(blurb))},
		[2]string{"{{PRICE_DISPLAY}}", html.EscapeString(priceDisplay)},
		[2]string{"{{PRICE_MAJOR}}", html.EscapeString(priceMajor)},
		[2]string{"{{CURRENCY}}", html.EscapeString(strings.ToUpper(currency))},
		[2]string{"{{TEAM_SIZE}}", teamSizeStr},
		[2]string{"{{TEAM_LABEL}}", html.EscapeString(teamLabel)},
		[2]string{"{{START_LABEL}}", html.EscapeString(strings.TrimSpace(startLabel.String))},
		[2]string{"{{COVER_URL}}", html.EscapeString(coverURL)},
		[2]string{"{{HAS_COVER}}", boolAttr(hasCover)},
		[2]string{"{{EVENT_ID}}", strconv.FormatInt(id, 10)},
		[2]string{"{{ADVENTURE_SLUG}}", html.EscapeString(advSlug)},
		[2]string{"{{CANONICAL_URL}}", html.EscapeString(buildAbsoluteURL(publicURL, "/store/event/"+strconv.FormatInt(id, 10)))},
	)

	out := string(body)
	for _, sub := range subs {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	// Short cache: the price / blurb are mutable in the arrange UI.
	// 60 seconds is plenty for crawler bursts without staleness
	// becoming a customer-visible problem.
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(out))
}

// formatStorePrice mirrors the JS helper on the listing page —
// known currencies get a symbol, others fall back to the ISO code.
// Whole-amount totals drop ".00" for cleaner display.
func formatStorePrice(cents int64, currency string) string {
	if cents <= 0 {
		return ""
	}
	c := strings.ToLower(currency)
	amount := float64(cents) / 100
	formatted := ""
	if cents%100 == 0 {
		formatted = strconv.FormatInt(cents/100, 10)
	} else {
		formatted = fmt.Sprintf("%.2f", amount)
	}
	switch c {
	case "eur":
		return "€" + formatted
	case "usd":
		return "$" + formatted
	case "gbp":
		return "£" + formatted
	case "sek", "dkk", "nok":
		return formatted + " kr"
	default:
		return formatted + " " + strings.ToUpper(c)
	}
}

// buildAbsoluteURL joins the operator-configured public origin with
// a path. Used for canonical URLs and OG tags where a relative path
// would render as "javascript:" or get prefixed with the crawler's
// own host. Empty publicURL falls back to the path alone — operator
// hasn't set ZV_PUBLIC_URL yet, OG previews will be incomplete but
// the page still works.
func buildAbsoluteURL(publicURL, path string) string {
	base := strings.TrimRight(publicURL, "/")
	if base == "" {
		return path
	}
	return base + path
}

// boolAttr returns "true" / "false" for {{KEY}} placeholders that
// drive CSS data attributes or simple JS boolean checks in inlined
// scripts.
func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
