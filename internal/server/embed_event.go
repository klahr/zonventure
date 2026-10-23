package server

import (
	"database/sql"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/klahr/zonventure/internal/domains"
	"github.com/klahr/zonventure/internal/listing"
)

// embedPrefix is the one place other sites may frame: an event's buy
// button or card, pasted into an organisation's own pages.
const embedPrefix = "/embed/"

// renderEmbedEvent serves /embed/event/{event_id}?size=button|card: a
// self-guided event's ticket, small enough to sit in a sidebar
// (button) or as a card on a page about it, in its organisation's
// colours. Buying opens the event's store page in a new tab: Stripe's
// checkout won't run in a frame, and the buyer should see the address
// they pay on. An event that can't be bought right now says so calmly
// rather than leaving a broken box on someone else's page.
//
// The template is embed-event.html, override-friendly like the store's.
func renderEmbedEvent(w http.ResponseWriter, r *http.Request, db *sql.DB, publicURL string, forSale func(int64) bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "event_id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	body, err := readWebFileFor(r, "web/embed-event.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	size := "card"
	if r.URL.Query().Get("size") == "button" {
		size = "button"
	}

	var (
		name, blurb, currency, advTitle, orgName, orgSlug, advSlug string
		priceCents, teamSize                                       int
		hasCover, hasIcon, tour, home                              bool
		accent, accent2, bg, ink                                   sql.NullString
		brandFont                                                  string
		mainLang                                                   string
		advTexts, ownTexts                                         []byte
	)
	err = db.QueryRowContext(r.Context(), `
		SELECT COALESCE(l.name, e.name), COALESCE(l.blurb, ''), COALESCE(l.currency, ''),
		       COALESCE(a.title, e.adventure_slug), e.adventure_slug, o.name, o.slug,
		       COALESCE(l.price_cents, 0), COALESCE(l.team_size, 0),
		       COALESCE(l.cover_mime_type IS NOT NULL, FALSE), COALESCE(a.avatar_mime_type IS NOT NULL, FALSE),
		       COALESCE((g.graph->>'tour')::boolean, FALSE),
		       (a.start_lat IS NULL AND NOT COALESCE(jsonb_typeof(g.graph->'zones') = 'array' AND jsonb_array_length(g.graph->'zones') > 0, FALSE)),
		       o.accent_color, o.accent_secondary_color, o.background_color, o.text_color, o.brand_font,
		       `+listing.Columns+`
		FROM events e
		JOIN organizations o ON o.id = e.org_id
		LEFT JOIN event_listings l ON l.event_id = e.id
		LEFT JOIN adventures a ON a.org_id = e.org_id AND a.slug = e.adventure_slug
		LEFT JOIN adventure_graphs g ON g.id = e.graph_id
		WHERE e.id = $1 AND e.public_for_sale
	`, id).Scan(&name, &blurb, &currency, &advTitle, &advSlug, &orgName, &orgSlug, &priceCents, &teamSize,
		&hasCover, &hasIcon, &tour, &home, &accent, &accent2, &bg, &ink, &brandFont, &mainLang, &advTexts, &ownTexts)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	onSale := found && forSale(id)

	lang := resolveLang(w, r)
	s := landingLocales[lang]
	if found {
		// The listing in the page's language, where it is translated.
		texts, err := listing.Scan(mainLang, advTexts, ownTexts)
		if err != nil {
			http.Error(w, "lookup failed", http.StatusInternalServerError)
			return
		}
		tr := texts.In(lang, listing.Texts{Name: name, Blurb: blurb, Title: advTitle})
		name, blurb, advTitle = tr.Name, tr.Blurb, tr.Title
	}
	name, advTitle = strings.TrimSpace(name), strings.TrimSpace(advTitle)
	subtitle := ""
	if !strings.EqualFold(name, advTitle) {
		subtitle = advTitle
	}
	kindKey, kind := "game", s.StoreKindGame
	switch {
	case tour:
		kindKey, kind = "walk", s.StoreKindWalk
	case home:
		kindKey, kind = "home", s.StoreKindHome
	}
	per := s.StorePerTicket
	if teamSize > 1 {
		per = s.StorePerTeam
	}
	price := formatStorePrice(int64(priceCents), currency)
	buy := s.EmbedBuyCTA
	if onSale && priceCents == 0 {
		price, per, buy = s.StorePriceFree, "", s.EmbedFreeCTA
	}
	teamLabel := s.StoreTeamSizeSolo
	if teamSize > 1 {
		teamLabel = strings.ReplaceAll(s.StoreTeamSizePlural, "{n}", strconv.Itoa(teamSize))
	}
	// Links go to the organisation's own domain once it has one.
	base := domains.BaseForEvent(r.Context(), db, id, publicURL)
	buyURL := base + "/o/" + url.PathEscape(orgSlug) + "/store/event/" + strconv.FormatInt(id, 10) + "?ref=embed"
	orgURL := base + "/o/" + url.PathEscape(orgSlug)
	if !found {
		buyURL, orgURL = base, base
	}
	coverURL := ""
	if hasCover {
		coverURL = "/store-cover/event/" + strconv.FormatInt(id, 10)
	}
	// The adventure's icon, beside the title of a button.
	iconURL := ""
	if hasIcon {
		iconURL = "/adventure-assets/avatar/" + url.PathEscape(advSlug)
	}

	subs := [][2]string{
		{"{{LANG_CODE}}", s.LangCode},
		{"{{SIZE}}", size},
		{"{{ON_SALE}}", boolAttr(onSale)},
		{"{{TITLE}}", html.EscapeString(name)},
		{"{{SUBTITLE}}", html.EscapeString(subtitle)},
		{"{{ORG_NAME}}", html.EscapeString(orgName)},
		{"{{BLURB}}", html.EscapeString(strings.TrimSpace(blurb))},
		{"{{PRICE_DISPLAY}}", html.EscapeString(price)},
		{"{{PER_LABEL}}", html.EscapeString(per)},
		{"{{TEAM_LABEL}}", html.EscapeString(teamLabel)},
		{"{{KIND_LABEL}}", html.EscapeString(kind)},
		{"{{KIND}}", kindKey},
		{"{{IS_TOUR}}", boolAttr(tour)},
		{"{{COVER_URL}}", html.EscapeString(cssURLQuote(coverURL))},
		{"{{HAS_COVER}}", boolAttr(hasCover)},
		{"{{ICON_URL}}", html.EscapeString(iconURL)},
		{"{{HAS_ICON}}", boolAttr(hasIcon)},
		{"{{BUY_URL}}", html.EscapeString(buyURL)},
		{"{{ORG_URL}}", html.EscapeString(orgURL)},
		{"{{BUY_LABEL}}", html.EscapeString(buy)},
		{"{{OFF_SALE}}", html.EscapeString(s.EmbedOffSale)},
		{"{{SEE_MORE}}", html.EscapeString(s.EmbedSeeMore)},
		{"{{ORG_BRAND_OVERRIDES}}", orgBrandStyleBlock(accent.String, accent2.String, bg.String, ink.String) + orgFontBlock(brandFont)},
	}
	out := string(body)
	for _, sub := range subs {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	// Short cache, as the ticket page's: the price and listing change.
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(out))
}
