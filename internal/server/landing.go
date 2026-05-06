package server

import (
	"net/http"
	"strings"

	"git.sr.ht/~klahr/zv/internal/auth"
)

// langCookie is the name of the persisted language preference. Set on
// any request that carries `?lang=` and read on subsequent requests so
// a returning visitor lands on the page in their chosen language.
const langCookie = "zv_lang"

// landingStrings holds every translatable string on the landing page.
// One struct shared by every supported locale so a missing field shows
// up as a compile error, not a silent untranslated paragraph.
type landingStrings struct {
	// HTML lang attribute and the picker's group ARIA label.
	// (Per-language flag + short label live inline in the template
	// because each row in the picker shows its own language's flag
	// regardless of which is currently active.)
	LangCode      string
	LangAriaLabel string // "Language", localized

	// <head> meta — these affect SEO, share previews, and tab title.
	Title          string
	MetaDesc       string
	OGDesc         string
	SchemaOrgDesc  string
	SchemaAppDesc  string

	// Topbar navigation.
	NavPrimaryARIA string
	NavToggleARIA  string // Aria label for the hamburger toggle on narrow screens.
	BrandARIA      string
	NavManual      string
	NavPricing     string
	NavLogin       string

	// Hero band.
	HeroTag      string
	HeroHeading  string
	HeroLede     string
	CTAFree      string
	CTAPossible  string
	ScrollHint   string

	// Benefits row.
	BenefitsARIA  string
	Benefit1Title string
	Benefit1Body  string
	Benefit2Title string
	Benefit2Body  string
	Benefit3Title string
	Benefit3Body  string

	// Use-case cards.
	UsecasesEyebrow string
	UsecasesHeading string
	UsecasesSub     string
	Usecase1Title   string
	Usecase1Body    string
	Usecase2Title   string
	Usecase2Body    string
	Usecase3Title   string
	Usecase3Body    string
	UsecaseLink     string

	// How-it-works.
	HowEyebrow string
	HowHeading string
	HowSub     string
	Step1Title string
	Step1Body  string
	Step2Title string
	Step2Body  string
	Step3Title string
	Step3Body  string

	// Quote band.
	QuoteARIA       string
	QuoteBody       string
	QuoteAttrLabel  string
	QuoteAttrAfter  string

	// Closing CTA.
	ClosingHeading string
	ClosingBody    string
	CTAGetStarted  string
	CTAReadManual  string

	// Footer.
	FooterARIA    string
	FooterPrivacy string
	FooterTerms   string
	FooterCopy    string

	// /store + /store/event/{id}. Both pages share the same locale
	// pool so a translator can update one column and watch every
	// surface follow. Keys prefixed with `Store*` are listing-only,
	// `StoreEvent*` are detail-only, and the rest (NavStore,
	// Step*, Toast*) are shared.
	NavStore string

	// Listing page (store.html).
	StoreTitle           string
	StoreMetaDesc        string
	StoreOGDesc          string
	StoreHeroEyebrow     string
	StoreHeroHeading     string
	StoreHeroLede        string
	StoreSectionEyebrow  string
	StoreSectionHeading  string
	StoreLoadingHeading  string
	StoreLoadingBody     string
	StoreEmptyHeading    string
	StoreEmptyBody       string
	StoreErrorHeading    string
	StoreErrorBody       string

	// "How it works" strip — shared by listing + detail.
	StoreHowEyebrow string
	StoreHowHeading string
	StoreStep1Title string
	StoreStep1Body  string
	StoreStep2Title string
	StoreStep2Body  string
	StoreStep3Title string
	StoreStep3Body  string

	// Toasts after Stripe redirect.
	StoreToastPaid      string
	StoreToastCancelled string

	// Card vocabulary — used both server-side and by the JS that
	// hydrates listing cards from /api/store/items. Two team-size
	// phrasings: solo (1) and the plural (anything ≥ 2). The
	// plural carries a `{n}` placeholder the JS replaces.
	StoreTeamSizeSolo   string
	StoreTeamSizePlural string
	StoreMeetsAt        string
	StoreCardCTA        string
	StorePerTicket      string
	StorePerTeam        string

	// Detail page (store-event.html).
	StoreEventCrumbs           string
	StoreEventAboutEyebrow     string
	StoreEventAboutHeading     string
	StoreEventMetaTeamSize     string
	StoreEventMetaFormat       string
	StoreEventMetaFormatValue  string
	StoreEventMetaWhen         string
	StoreEventMetaWhenValue    string
	StoreEventMetaMeetingPoint string
	StoreEventBuyHeading       string
	StoreEventEmailLabel       string
	StoreEventEmailPlaceholder string
	StoreEventBuyCTA           string // {{PRICE_DISPLAY}} placeholder is substituted per-event
	StoreEventTermsHTML        string // contains <a href="/terms">…</a>
	StoreEventFeature1         string
	StoreEventFeature2         string // carries `{n}` for team size
	StoreEventFeature3         string
	StoreEventFeature4         string
	StoreEventBuyNowCTA        string
	StoreEventHowCTA           string
	StoreEventHeroPriceSuffix  string // "per ticket · {team_label}"

	// FAQ accordion.
	StoreEventFAQEyebrow string
	StoreEventFAQHeading string
	StoreEventFAQ1Q      string
	StoreEventFAQ1A      string // carries `{n}` for team size
	StoreEventFAQ2Q      string
	StoreEventFAQ2A      string
	StoreEventFAQ3Q      string
	StoreEventFAQ3A      string
	StoreEventFAQ4Q      string
	StoreEventFAQ4A      string

	// Validation + error strings emitted into the inline JS i18n
	// block so client-side messages stay in the user's locale.
	StoreEventBadEmail   string
	StoreEventGenericErr string
}

// landingLocales maps a language code to its strings. Add a new
// language by appending a fresh entry — the placeholder substitution
// is by struct field, so a new entry without all fields fails the
// compile.
//
// These are the brand-neutral defaults shipped with the OSS binary.
// A self-hosted deployment that wants different copy (richer
// marketing, multi-segment positioning, custom testimonials) can
// shadow per-field via landing-locales.json under
// ZV_WEB_OVERRIDE_DIR — see internal/server/overrides.go.
//
// Fields the embedded landing.html doesn't reference (Usecase*, How*,
// Quote*, ScrollHint, CTAPossible) are intentionally left empty: an
// override-shipped richer template can fill them in via the JSON,
// but a stock binary doesn't need filler text it never displays.
// landingLocales is populated at server startup from the embedded
// JSON files under web/locales/landing/<lang>.json — see
// i18n_load.go::loadLandingFromJSON. The map's nil-zero value would
// race against any landing request that arrives before init runs;
// declaring it as an empty (non-nil) map keeps reads safe even in
// the unlikely case the init step is skipped.
var landingLocales = map[string]landingStrings{}


// resolveLang picks the language for this request. Order:
//   1. explicit `?lang=xx` query (and persists it via cookie)
//   2. previously-stored `zv_lang` cookie
//   3. Accept-Language first segment when it matches a supported locale
//   4. fall back to "en"
//
// When (1) fires the cookie is set so the next visit hits (2) without a
// query string.
func resolveLang(w http.ResponseWriter, r *http.Request) string {
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); q != "" {
		if _, ok := landingLocales[q]; ok {
			http.SetCookie(w, &http.Cookie{
				Name:     langCookie,
				Value:    q,
				Path:     "/",
				MaxAge:   60 * 60 * 24 * 365, // one year
				HttpOnly: false,              // intentionally readable by JS so in-app picker can sync
				SameSite: http.SameSiteLaxMode,
				// Secure when the request itself was TLS / proxy-
				// forwarded HTTPS. The cookie holds only a locale code
				// (no auth, no sensitive data), so this is hardening
				// rather than load-bearing — but standard practice on
				// any cookie when the connection is already secure.
				Secure: auth.CookieSecure(r, false),
			})
			return q
		}
	}
	if c, err := r.Cookie(langCookie); err == nil {
		if _, ok := landingLocales[strings.ToLower(c.Value)]; ok {
			return strings.ToLower(c.Value)
		}
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		// Crude — first chunk before the first ',' or ';'. Enough for
		// the "browser sends sv-SE" common case without pulling a full
		// RFC 4647 matcher in.
		first := al
		if i := strings.IndexAny(first, ",;"); i >= 0 {
			first = first[:i]
		}
		first = strings.ToLower(strings.TrimSpace(first))
		// Normalise sv-SE → sv.
		if i := strings.IndexByte(first, '-'); i >= 0 {
			first = first[:i]
		}
		if _, ok := landingLocales[first]; ok {
			return first
		}
	}
	return "en"
}

// topbarSubs returns just the topbar-related substitutions: the
// strings every page that ships the shared topbar needs (lang code,
// nav labels, picker active-class flags). Used by `renderTopbarPage`
// for /manual and /pricing — pages whose body content isn't
// translated but whose topbar / language-picker still has to honour
// the caller's locale.
func topbarSubs(s landingStrings, lang string) [][2]string {
	return [][2]string{
		{"{{LANG_CODE}}", s.LangCode},
		{"{{LANG_ARIA}}", s.LangAriaLabel},
		{"{{NAV_PRIMARY_ARIA}}", s.NavPrimaryARIA},
		{"{{NAV_TOGGLE_ARIA}}", s.NavToggleARIA},
		{"{{BRAND_ARIA}}", s.BrandARIA},
		{"{{NAV_MANUAL}}", s.NavManual},
		{"{{NAV_PRICING}}", s.NavPricing},
		{"{{NAV_STORE}}", s.NavStore},
		{"{{NAV_LOGIN}}", s.NavLogin},
		{"{{FOOTER_ARIA}}", s.FooterARIA},
		{"{{FOOTER_PRIVACY}}", s.FooterPrivacy},
		{"{{FOOTER_TERMS}}", s.FooterTerms},
		{"{{FOOTER_COPY}}", s.FooterCopy},
		{"{{EN_CURRENT}}", ariaCurrentIf(lang == "en")},
		{"{{SV_CURRENT}}", ariaCurrentIf(lang == "sv")},
		{"{{EN_ACTIVE}}", activeClassIf(lang == "en")},
		{"{{SV_ACTIVE}}", activeClassIf(lang == "sv")},
	}
}

// storeSubs returns the {{KEY}} replacements specific to the public
// /store listing and /store/event/{id} detail pages — all the body
// copy, vocabulary fragments, and FAQ lines for the requested
// locale. Topbar / footer chrome subs come from topbarSubs (which
// also owns {{NAV_STORE}} so every page sharing the topbar can link
// here) and are merged separately so the same store template can be
// served by either renderStoreListingPage or renderStoreEventPage
// without duplicating the chrome list.
func storeSubs(s landingStrings) [][2]string {
	return [][2]string{
		{"{{STORE_TITLE}}", s.StoreTitle},
		{"{{STORE_META_DESC}}", s.StoreMetaDesc},
		{"{{STORE_OG_DESC}}", s.StoreOGDesc},
		{"{{STORE_HERO_EYEBROW}}", s.StoreHeroEyebrow},
		{"{{STORE_HERO_HEADING}}", s.StoreHeroHeading},
		{"{{STORE_HERO_LEDE}}", s.StoreHeroLede},
		{"{{STORE_SECTION_EYEBROW}}", s.StoreSectionEyebrow},
		{"{{STORE_SECTION_HEADING}}", s.StoreSectionHeading},
		{"{{STORE_LOADING_HEADING}}", s.StoreLoadingHeading},
		{"{{STORE_LOADING_BODY}}", s.StoreLoadingBody},
		{"{{STORE_EMPTY_HEADING}}", s.StoreEmptyHeading},
		{"{{STORE_EMPTY_BODY}}", s.StoreEmptyBody},
		{"{{STORE_ERROR_HEADING}}", s.StoreErrorHeading},
		{"{{STORE_ERROR_BODY}}", s.StoreErrorBody},

		{"{{STORE_HOW_EYEBROW}}", s.StoreHowEyebrow},
		{"{{STORE_HOW_HEADING}}", s.StoreHowHeading},
		{"{{STORE_STEP1_TITLE}}", s.StoreStep1Title},
		{"{{STORE_STEP1_BODY}}", s.StoreStep1Body},
		{"{{STORE_STEP2_TITLE}}", s.StoreStep2Title},
		{"{{STORE_STEP2_BODY}}", s.StoreStep2Body},
		{"{{STORE_STEP3_TITLE}}", s.StoreStep3Title},
		{"{{STORE_STEP3_BODY}}", s.StoreStep3Body},

		{"{{STORE_TOAST_PAID}}", s.StoreToastPaid},
		{"{{STORE_TOAST_CANCELLED}}", s.StoreToastCancelled},

		{"{{STORE_TEAM_SIZE_SOLO}}", s.StoreTeamSizeSolo},
		{"{{STORE_TEAM_SIZE_PLURAL}}", s.StoreTeamSizePlural},
		{"{{STORE_MEETS_AT}}", s.StoreMeetsAt},
		{"{{STORE_CARD_CTA}}", s.StoreCardCTA},
		{"{{STORE_PER_TICKET}}", s.StorePerTicket},
		{"{{STORE_PER_TEAM}}", s.StorePerTeam},

		{"{{STORE_EVENT_CRUMBS}}", s.StoreEventCrumbs},
		{"{{STORE_EVENT_ABOUT_EYEBROW}}", s.StoreEventAboutEyebrow},
		{"{{STORE_EVENT_ABOUT_HEADING}}", s.StoreEventAboutHeading},
		{"{{STORE_EVENT_META_TEAM_SIZE}}", s.StoreEventMetaTeamSize},
		{"{{STORE_EVENT_META_FORMAT}}", s.StoreEventMetaFormat},
		{"{{STORE_EVENT_META_FORMAT_VALUE}}", s.StoreEventMetaFormatValue},
		{"{{STORE_EVENT_META_WHEN}}", s.StoreEventMetaWhen},
		{"{{STORE_EVENT_META_WHEN_VALUE}}", s.StoreEventMetaWhenValue},
		{"{{STORE_EVENT_META_MEETING_POINT}}", s.StoreEventMetaMeetingPoint},
		{"{{STORE_EVENT_BUY_HEADING}}", s.StoreEventBuyHeading},
		{"{{STORE_EVENT_EMAIL_LABEL}}", s.StoreEventEmailLabel},
		{"{{STORE_EVENT_EMAIL_PLACEHOLDER}}", s.StoreEventEmailPlaceholder},
		{"{{STORE_EVENT_BUY_CTA}}", s.StoreEventBuyCTA},
		{"{{STORE_EVENT_TERMS_HTML}}", s.StoreEventTermsHTML},
		{"{{STORE_EVENT_FEATURE1}}", s.StoreEventFeature1},
		{"{{STORE_EVENT_FEATURE2}}", s.StoreEventFeature2},
		{"{{STORE_EVENT_FEATURE3}}", s.StoreEventFeature3},
		{"{{STORE_EVENT_FEATURE4}}", s.StoreEventFeature4},
		{"{{STORE_EVENT_BUY_NOW_CTA}}", s.StoreEventBuyNowCTA},
		{"{{STORE_EVENT_HOW_CTA}}", s.StoreEventHowCTA},
		{"{{STORE_EVENT_HERO_PRICE_SUFFIX}}", s.StoreEventHeroPriceSuffix},

		{"{{STORE_EVENT_FAQ_EYEBROW}}", s.StoreEventFAQEyebrow},
		{"{{STORE_EVENT_FAQ_HEADING}}", s.StoreEventFAQHeading},
		{"{{STORE_EVENT_FAQ1_Q}}", s.StoreEventFAQ1Q},
		{"{{STORE_EVENT_FAQ1_A}}", s.StoreEventFAQ1A},
		{"{{STORE_EVENT_FAQ2_Q}}", s.StoreEventFAQ2Q},
		{"{{STORE_EVENT_FAQ2_A}}", s.StoreEventFAQ2A},
		{"{{STORE_EVENT_FAQ3_Q}}", s.StoreEventFAQ3Q},
		{"{{STORE_EVENT_FAQ3_A}}", s.StoreEventFAQ3A},
		{"{{STORE_EVENT_FAQ4_Q}}", s.StoreEventFAQ4Q},
		{"{{STORE_EVENT_FAQ4_A}}", s.StoreEventFAQ4A},

		{"{{STORE_EVENT_BAD_EMAIL}}", s.StoreEventBadEmail},
		{"{{STORE_EVENT_GENERIC_ERR}}", s.StoreEventGenericErr},
	}
}

// renderStoreListingPage serves /store. Mirrors renderTopbarPage but
// also splices in the storeSubs map so the page body's body copy
// (hero, "how it works", empty / loading / error states, card
// vocabulary) renders in the caller's locale. The card grid itself
// hydrates from /api/store/items at runtime, but the surrounding
// chrome and the JSON locale block emitted at the bottom of the
// template stay server-rendered.
func renderStoreListingPage(w http.ResponseWriter, r *http.Request) {
	body, err := readWebFile("web/store.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	out := string(body)
	for _, sub := range topbarSubs(s, lang) {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	for _, sub := range storeSubs(s) {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// renderTopbarPage reads an embedded HTML template, splices in the
// topbar locale strings, and writes the result. Used for pages whose
// body is plain English (today: manual, pricing) but whose shared
// topbar + footer still need to honour the caller's locale and
// surface the language picker correctly. The page body should
// reference the topbar/footer keys via {{KEY}} placeholders.
func renderTopbarPage(w http.ResponseWriter, r *http.Request, name string) {
	body, err := readWebFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	out := string(body)
	for _, sub := range topbarSubs(s, lang) {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// renderLandingPage substitutes the locale's strings into the embedded
// landing template and writes the result. Style mirrors renderLegalPage
// — strings.ReplaceAll over a {{KEY}} placeholder grammar so the
// template stays a normal HTML file an editor can syntax-highlight.
//
// Chrome substitutions (lang attribute, topbar nav, footer, picker
// flags) come from topbarSubs so the same set covers every page that
// shares the topbar. Body-content substitutions (hero, benefits,
// use-cases, "how it works", quote band, closing CTA) are landing-
// specific and stay inline below. A new chrome field — like
// {{NAV_STORE}} — only needs adding to topbarSubs once and every
// caller picks it up automatically.
func renderLandingPage(w http.ResponseWriter, r *http.Request) {
	body, err := readWebFile("web/landing.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	out := string(body)
	subs := topbarSubs(s, lang)
	subs = append(subs,
		// <head> meta — landing-specific, not part of the shared
		// topbar (manual / pricing have their own page-level meta).
		[2]string{"{{TITLE}}", s.Title},
		[2]string{"{{META_DESC}}", s.MetaDesc},
		[2]string{"{{OG_DESC}}", s.OGDesc},
		[2]string{"{{SCHEMA_ORG_DESC}}", s.SchemaOrgDesc},
		[2]string{"{{SCHEMA_APP_DESC}}", s.SchemaAppDesc},

		// Hero band.
		[2]string{"{{HERO_TAG}}", s.HeroTag},
		[2]string{"{{HERO_HEADING}}", s.HeroHeading},
		[2]string{"{{HERO_LEDE}}", s.HeroLede},
		[2]string{"{{CTA_FREE}}", s.CTAFree},
		[2]string{"{{CTA_POSSIBLE}}", s.CTAPossible},
		[2]string{"{{SCROLL_HINT}}", s.ScrollHint},

		// Benefits.
		[2]string{"{{BENEFITS_ARIA}}", s.BenefitsARIA},
		[2]string{"{{BENEFIT1_TITLE}}", s.Benefit1Title},
		[2]string{"{{BENEFIT1_BODY}}", s.Benefit1Body},
		[2]string{"{{BENEFIT2_TITLE}}", s.Benefit2Title},
		[2]string{"{{BENEFIT2_BODY}}", s.Benefit2Body},
		[2]string{"{{BENEFIT3_TITLE}}", s.Benefit3Title},
		[2]string{"{{BENEFIT3_BODY}}", s.Benefit3Body},

		// Use-cases.
		[2]string{"{{USECASES_EYEBROW}}", s.UsecasesEyebrow},
		[2]string{"{{USECASES_HEADING}}", s.UsecasesHeading},
		[2]string{"{{USECASES_SUB}}", s.UsecasesSub},
		[2]string{"{{USECASE1_TITLE}}", s.Usecase1Title},
		[2]string{"{{USECASE1_BODY}}", s.Usecase1Body},
		[2]string{"{{USECASE2_TITLE}}", s.Usecase2Title},
		[2]string{"{{USECASE2_BODY}}", s.Usecase2Body},
		[2]string{"{{USECASE3_TITLE}}", s.Usecase3Title},
		[2]string{"{{USECASE3_BODY}}", s.Usecase3Body},
		[2]string{"{{USECASE_LINK}}", s.UsecaseLink},

		// How-it-works.
		[2]string{"{{HOW_EYEBROW}}", s.HowEyebrow},
		[2]string{"{{HOW_HEADING}}", s.HowHeading},
		[2]string{"{{HOW_SUB}}", s.HowSub},
		[2]string{"{{STEP1_TITLE}}", s.Step1Title},
		[2]string{"{{STEP1_BODY}}", s.Step1Body},
		[2]string{"{{STEP2_TITLE}}", s.Step2Title},
		[2]string{"{{STEP2_BODY}}", s.Step2Body},
		[2]string{"{{STEP3_TITLE}}", s.Step3Title},
		[2]string{"{{STEP3_BODY}}", s.Step3Body},

		// Quote band.
		[2]string{"{{QUOTE_ARIA}}", s.QuoteARIA},
		[2]string{"{{QUOTE_BODY}}", s.QuoteBody},
		[2]string{"{{QUOTE_ATTR_LABEL}}", s.QuoteAttrLabel},
		[2]string{"{{QUOTE_ATTR_AFTER}}", s.QuoteAttrAfter},

		// Closing CTA.
		[2]string{"{{CLOSING_HEADING}}", s.ClosingHeading},
		[2]string{"{{CLOSING_BODY}}", s.ClosingBody},
		[2]string{"{{CTA_GET_STARTED}}", s.CTAGetStarted},
		[2]string{"{{CTA_READ_MANUAL}}", s.CTAReadManual},
	)
	for _, sub := range subs {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Vary on Accept-Language + Cookie so caches don't serve the
	// English version to a Swedish-cookie visitor.
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

func ariaCurrentIf(active bool) string {
	if active {
		return `aria-current="page"`
	}
	return ""
}

func activeClassIf(active bool) string {
	if active {
		return "active"
	}
	return ""
}
