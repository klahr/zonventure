package server

import (
	"encoding/json"
	"fmt"
	"github.com/klahr/zonventure/internal/settings"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/klahr/zonventure/internal/auth"
	"github.com/klahr/zonventure/internal/httpx"
	"github.com/klahr/zonventure/internal/logx"
)

// landingPageCache holds the precomputed per-(lang, name, publicURL)
// rendered body so we don't re-run the ~80 strings.ReplaceAll loop on
// every request to a marketing page. Filled lazily on first request
// and kept for the process lifetime (locales + templates are static
// after startup; override-dir hits skip the cache entirely so a dev
// workflow with ZV_WEB_OVERRIDE_DIR still sees edits without
// restart).
var (
	landingPageCacheMu sync.RWMutex
	landingPageCache   = map[string][]byte{}
)

// cachedRender memoises build()'s result keyed by the named tuple.
// First call: build runs, output is stored. Subsequent calls return
// the stored bytes without re-running build. The override-dir gate
// short-circuits the cache so dev iteration is unaffected.
func cachedRender(lang, name, extraKey string, build func() []byte) []byte {
	if webOverrideDir != "" {
		return build()
	}
	key := lang + "|" + name + "|" + extraKey
	landingPageCacheMu.RLock()
	if v, ok := landingPageCache[key]; ok {
		landingPageCacheMu.RUnlock()
		return v
	}
	landingPageCacheMu.RUnlock()
	v := build()
	landingPageCacheMu.Lock()
	landingPageCache[key] = v
	landingPageCacheMu.Unlock()
	return v
}

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
	SkipToContent string // the skip link before the topbar

	// <head> meta — these affect SEO, share previews, and tab title.
	Title         string
	MetaDesc      string
	OGDesc        string
	SchemaOrgDesc string
	SchemaAppDesc string

	// Topbar navigation.
	NavPrimaryARIA string
	NavToggleARIA  string // Aria label for the hamburger toggle on narrow screens.
	BrandARIA      string
	NavManual      string
	NavPricing     string
	NavLogin       string

	// Hero band.
	HeroTag     string
	HeroHeading string
	HeroLede    string
	CTAFree     string
	CTAPossible string
	ScrollHint  string

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
	QuoteARIA      string
	QuoteBody      string
	QuoteAttrLabel string
	QuoteAttrAfter string

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

	// 404 page.
	NotFoundTitle     string
	NotFoundHeading   string
	NotFoundBody      string
	NotFoundCTAHome   string
	NotFoundCTAManual string

	// The same card when the database can't be reached.
	UnavailableTitle   string
	UnavailableHeading string
	UnavailableBody    string

	// /store + /store/event/{id}. Both pages share the same locale
	// pool so a translator can update one column and watch every
	// surface follow. Keys prefixed with `Store*` are listing-only,
	// `StoreEvent*` are detail-only, and the rest (NavStore,
	// Step*, Toast*) are shared.
	NavStore string

	// Verified creator badge (listing review), on the org page, its
	// cards and the ticket page.
	VerifiedCreator     string
	VerifiedCreatorHint string
	// Listing page (store.html).
	StoreTitle          string
	StoreMetaDesc       string
	StoreOGDesc         string
	StoreHeroEyebrow    string
	StoreHeroHeading    string
	StoreHeroLede       string
	StoreSectionEyebrow string
	StoreSectionHeading string
	StoreLoadingHeading string
	StoreLoadingBody    string
	StoreEmptyHeading   string
	StoreEmptyBody      string
	StoreErrorHeading   string
	StoreErrorBody      string

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

	// Post-purchase hand-off: while the JS polls the team-token
	// endpoint, between Stripe's redirect and the webhook landing,
	// these short messages tell the buyer what's happening so the
	// page never goes silent.
	StoreHandoffPreparing     string // "Setting up your team — one moment…"
	StoreHandoffEmailFallback string // shown on timeout / 404 — "Almost done. Your team link is on its way to your inbox."
	StoreHandoffFailed        string // state == "failed" path — "Something went wrong with this purchase."

	// Card vocabulary — used both server-side and by the JS that
	// hydrates listing cards from /api/store/items. Two team-size
	// phrasings: solo (1) and the plural (anything ≥ 2). The
	// plural carries a `{n}` placeholder the JS replaces.
	StoreTeamSizeSolo   string
	StoreTeamSizePlural string
	StoreMeetsAt        string
	StoreMapLabel       string
	StorePlaysAnywhere  string
	StoreMapZoomHint    string
	StoreMapPanHint     string
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
	StoreEventLetterLangLabel  string
	StoreEventLetterLangHint   string
	StoreEventEmailPlaceholder string
	StoreEventBuyCTA           string // {{PRICE_DISPLAY}} placeholder is substituted per-event
	StoreEventTermsHTML        string // contains <a href="/terms">…</a>
	StoreEventPaymentByStripe  string // before the terms when Stripe takes the payment
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
	StoreEventNotForSale string
	// StoreEventOffSaleHeading heads the page of a listing that is
	// paused or has stopped selling, over StoreEventNotForSale.
	StoreEventOffSaleHeading string

	// Gift box redemption page (redeem.html).
	RedeemTitle           string
	RedeemIntro           string
	RedeemPartnerLabel    string
	RedeemPartnerChoose   string
	RedeemProductLabel    string
	RedeemAddressHeading  string
	RedeemAddressHint     string
	RedeemLetterLangLabel string
	RedeemLetterLangHint  string
	RedeemNameLabel       string
	RedeemEmailLabel      string
	RedeemStreetLabel     string
	RedeemAddress2Label   string
	RedeemPostalCodeLabel string
	RedeemCityLabel       string
	RedeemCountryLabel    string
	RedeemTermsHTML       string
	RedeemSubmit          string
	RedeemSending         string
	RedeemDoneHeading     string
	RedeemDoneBody        string
	RedeemDoneLetter      string
	RedeemNone            string
	RedeemErrMissing      string
	RedeemErrTerms        string
	RedeemErrEmail        string
	RedeemErrCode         string
	RedeemErrUsed         string
	RedeemErrUsedContact  string
	RedeemErrProduct      string
	RedeemErrGeneric      string
	RedeemErrRateLimited  string

	// Per-org landing page (org-landing.html). Used for the hero
	// CTA, the featured-quests strip, and the description fallback
	// when an org hasn't set a tagline.
	LandingTaglineFallback string
	LandingBrowseCTA       string
	LandingFeaturedHeading string
	LandingFeaturedEmpty   string
	LandingSeeAll          string
	LandingVisitWebsite    string
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
//  1. explicit `?lang=xx` query (and persists it via cookie)
//  2. previously-stored `zv_lang` cookie
//  3. Accept-Language first segment when it matches a supported locale
//  4. fall back to "en"
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
		{"{{SKIP_TO_CONTENT}}", s.SkipToContent},
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

// storeI18nJSON returns the JSON blob the /store listing's inline
// `<script id="store-i18n" type="application/json">` consumes. We
// marshal one map server-side rather than substituting strings into
// a literal JSON template, because translator-supplied values can
// contain double quotes / backslashes / `</script` substrings that
// would break out of the JSON or even the surrounding script element
// if interpolated raw. json.Marshal handles every escape correctly.
//
// Keep the keys aligned with the t() lookups in store.html.
func storeI18nJSON(s landingStrings) string {
	m := map[string]string{
		"toastPaid":            s.StoreToastPaid,
		"toastCancelled":       s.StoreToastCancelled,
		"teamSizeSolo":         s.StoreTeamSizeSolo,
		"teamSizePlural":       s.StoreTeamSizePlural,
		"meetsAt":              s.StoreMeetsAt,
		"mapLabel":             s.StoreMapLabel,
		"playsAnywhere":        s.StorePlaysAnywhere,
		"mapZoomHint":          s.StoreMapZoomHint,
		"mapPanHint":           s.StoreMapPanHint,
		"cardCTA":              s.StoreCardCTA,
		"perTicket":            s.StorePerTicket,
		"emptyHeading":         s.StoreEmptyHeading,
		"emptyBody":            s.StoreEmptyBody,
		"errorHeading":         s.StoreErrorHeading,
		"errorBody":            s.StoreErrorBody,
		"handoffPreparing":     s.StoreHandoffPreparing,
		"handoffEmailFallback": s.StoreHandoffEmailFallback,
		"handoffFailed":        s.StoreHandoffFailed,
		"verifiedCreator":      s.VerifiedCreator,
		"verifiedCreatorHint":  s.VerifiedCreatorHint,
	}
	return marshalJSONForScript(m)
}

// storeEventI18nJSON returns the JSON blob the /store/event/{id}
// page's inline `<script id="store-event-i18n">` consumes. Same
// rationale as storeI18nJSON: avoid raw substitution into a JSON
// literal.
func storeEventI18nJSON(s landingStrings) string {
	return marshalJSONForScript(map[string]string{
		"toastPaid":      s.StoreToastPaid,
		"toastCancelled": s.StoreToastCancelled,
		"badEmail":       s.StoreEventBadEmail,
		"genericErr":     s.StoreEventGenericErr,
		"notForSale":     s.StoreEventNotForSale,
	})
}

// orgLandingI18nJSON returns the JSON blob org-landing.html consumes
// for its featured-event card grid. Smaller key set than the main
// store page; only the labels the cards render.
func orgLandingI18nJSON(s landingStrings) string {
	return marshalJSONForScript(map[string]string{
		"teamSizeSolo":   s.StoreTeamSizeSolo,
		"teamSizePlural": s.StoreTeamSizePlural,
		"perTicket":      s.StorePerTicket,
		"perTeam":        s.StorePerTeam,
		"emptyHeading":   s.LandingFeaturedEmpty,
		"errorHeading":   s.StoreErrorHeading,
	})
}

// marshalJSONForScript JSON-encodes v and additionally escapes the
// HTML token `</script>` so a malicious or unlucky string can't end
// the surrounding inline `<script type="application/json">` block.
// `<` is escaped to `<`, which JSON.parse decodes back to `<`.
// json.Marshal already escapes <, >, & for safety in HTML contexts;
// we keep that on (it's the default) and double-check by rewriting
// the close-tag sequence as a final guard.
func marshalJSONForScript(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Fail closed: an empty object is harmless on the page (every
		// t() lookup returns "", which the JS already handles). Logged
		// once at startup if this ever happens.
		logx.Warn("store.i18n.marshal_failed", "err", err.Error())
		return "{}"
	}
	// Defence in depth: json.Marshal escapes < > & by default but
	// only inside string values. Direct substring guard for the
	// </script terminator and for ]]> which would close a CDATA
	// section (not used today but cheap to forbid).
	out := strings.ReplaceAll(string(b), "</", `<\/`)
	return out
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
		{"{{VERIFIED_CREATOR}}", s.VerifiedCreator},
		{"{{VERIFIED_CREATOR_HINT}}", s.VerifiedCreatorHint},
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
		{"{{STORE_HANDOFF_PREPARING}}", s.StoreHandoffPreparing},
		{"{{STORE_HANDOFF_EMAIL_FALLBACK}}", s.StoreHandoffEmailFallback},
		{"{{STORE_HANDOFF_FAILED}}", s.StoreHandoffFailed},

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
		{"{{STORE_EVENT_LETTER_LANG_LABEL}}", s.StoreEventLetterLangLabel},
		{"{{STORE_EVENT_LETTER_LANG_HINT}}", s.StoreEventLetterLangHint},
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

		// JSON blobs that the inline `<script type="application/json">`
		// elements parse with JSON.parse. Build them once with
		// json.Marshal so a translator's quote / backslash / </script
		// can never break the page or escape the script context.
		{"{{STORE_I18N_JSON}}", storeI18nJSON(s)},
		{"{{STORE_EVENT_I18N_JSON}}", storeEventI18nJSON(s)},
		{"{{ORG_LANDING_I18N_JSON}}", orgLandingI18nJSON(s)},
	}
}

// renderOrgPage renders /o/{slug}, an organisation's page: who it is —
// name, tagline, logo, cover, colours — and every adventure it sells,
// read from /api/o/{slug}/store/items. It is also where a buyer lands
// back from checkout (?paid=…), which the page's script follows up.
// {{STORE_SCOPE}} stays "org" for templates that still style by it.
func renderOrgPage(w http.ResponseWriter, r *http.Request, org orgProfile) {
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
	apiBase := "/api/o/" + org.Slug + "/store"
	orgSubs := [][2]string{
		{"{{STORE_SCOPE}}", "org"},
		{"{{ORG_NAME}}", html.EscapeString(org.Name)},
		{"{{ORG_TAGLINE}}", html.EscapeString(strings.TrimSpace(org.Tagline.String))},
		{"{{ORG_PATH}}", "/o/" + org.Slug},
		{"{{LOGO_BLOCK}}", org.logoBlock()},
		{"{{WEBSITE_LINK_BLOCK}}", org.websiteBlock()},
		{"{{HERO_BG_STYLE}}", org.heroBgStyle()},
		{"{{ORG_BRAND_OVERRIDES}}", org.brandStyle()},
		{"{{ORG_VERIFIED}}", boolAttr(org.Verified)},
	}
	for _, sub := range orgSubs {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	out = strings.ReplaceAll(out, "{{STORE_API_BASE}}", apiBase)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// redeemSubs are redeem.html's text substitutions. Labels go in as
// plain text; RedeemTermsHTML carries the terms and privacy links.
func redeemSubs(s landingStrings) [][2]string {
	return [][2]string{
		{"{{REDEEM_TITLE}}", s.RedeemTitle},
		{"{{REDEEM_INTRO}}", s.RedeemIntro},
		{"{{REDEEM_PARTNER_LABEL}}", s.RedeemPartnerLabel},
		{"{{REDEEM_PARTNER_CHOOSE}}", s.RedeemPartnerChoose},
		{"{{REDEEM_PRODUCT_LABEL}}", s.RedeemProductLabel},
		{"{{REDEEM_ADDRESS_HEADING}}", s.RedeemAddressHeading},
		{"{{REDEEM_ADDRESS_HINT}}", s.RedeemAddressHint},
		{"{{REDEEM_LETTER_LANG_LABEL}}", s.RedeemLetterLangLabel},
		{"{{REDEEM_LETTER_LANG_HINT}}", s.RedeemLetterLangHint},
		{"{{REDEEM_NAME_LABEL}}", s.RedeemNameLabel},
		{"{{REDEEM_EMAIL_LABEL}}", s.RedeemEmailLabel},
		{"{{REDEEM_STREET_LABEL}}", s.RedeemStreetLabel},
		{"{{REDEEM_ADDRESS2_LABEL}}", s.RedeemAddress2Label},
		{"{{REDEEM_POSTAL_CODE_LABEL}}", s.RedeemPostalCodeLabel},
		{"{{REDEEM_CITY_LABEL}}", s.RedeemCityLabel},
		{"{{REDEEM_COUNTRY_LABEL}}", s.RedeemCountryLabel},
		{"{{REDEEM_TERMS_HTML}}", s.RedeemTermsHTML},
		{"{{REDEEM_SUBMIT}}", s.RedeemSubmit},
		{"{{REDEEM_I18N_JSON}}", marshalJSONForScript(map[string]string{
			"sending":        s.RedeemSending,
			"doneHeading":    s.RedeemDoneHeading,
			"doneBody":       s.RedeemDoneBody,
			"doneLetter":     s.RedeemDoneLetter,
			"none":           s.RedeemNone,
			"errMissing":     s.RedeemErrMissing,
			"errTerms":       s.RedeemErrTerms,
			"errEmail":       s.RedeemErrEmail,
			"errCode":        s.RedeemErrCode,
			"errUsed":        s.RedeemErrUsed,
			"errUsedContact": s.RedeemErrUsedContact,
			"errProduct":     s.RedeemErrProduct,
			"errGeneric":     s.RedeemErrGeneric,
			"errRateLimited": s.RedeemErrRateLimited,
		})},
	}
}

// renderRedeemPage serves /redeem and /o/{slug}/redeem, where a gift box
// bought through a partner is redeemed. The per-org page only offers
// that organisation's products; the options and the form post go to the
// matching API base, and names orgName where the bare page names the site.
func renderRedeemPage(w http.ResponseWriter, r *http.Request, orgSlug, orgName string) {
	body, err := readWebFile("web/redeem.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	if orgName != "" {
		s.RedeemIntro = strings.ReplaceAll(s.RedeemIntro, "{{SITE_NAME}}", html.EscapeString(orgName))
	}
	out := string(body)
	for _, sub := range topbarSubs(s, lang) {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	for _, sub := range redeemSubs(s) {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	apiBase := "/api/redeem"
	if orgSlug != "" {
		apiBase = "/api/o/" + orgSlug + "/redeem"
	}
	out = strings.ReplaceAll(out, "{{REDEEM_API_BASE}}", apiBase)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// render404 serves the locale-aware 404 page. Kept minimal (no
// topbar chrome) on the theory that a 404 is the last surface that
// should depend on the rest of the site working — so the page is a
// self-contained card with its own CSS. The four user-visible
// strings are localised so SV / future locale visitors don't get
// English on the failure floor.
func render404(w http.ResponseWriter, r *http.Request) {
	s := landingLocales[resolveLang(w, r)]
	renderErrorPage(w, r, http.StatusNotFound, s.NotFoundTitle, s.NotFoundHeading, s.NotFoundBody)
}

// renderErrorPage is the 404 page's card with status and its own text.
func renderErrorPage(w http.ResponseWriter, r *http.Request, status int, title, heading, body string) {
	page, err := readWebFile("web/404.html")
	if err != nil {
		http.Error(w, http.StatusText(status), status)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	subs := [][2]string{
		{"{{LANG_CODE}}", lang},
		{"{{STATUS_CODE}}", strconv.Itoa(status)},
		{"{{NOT_FOUND_TITLE}}", title},
		{"{{NOT_FOUND_HEADING}}", heading},
		{"{{NOT_FOUND_BODY}}", body},
		{"{{NOT_FOUND_CTA_HOME}}", s.NotFoundCTAHome},
		{"{{NOT_FOUND_CTA_MANUAL}}", s.NotFoundCTAManual},
	}
	out := string(page)
	for _, sub := range subs {
		out = strings.ReplaceAll(out, sub[0], sub[1])
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(out))
}

// lookupFailed answers a request whose session or player cookie can't
// be looked up because the database is down: a page request gets the
// error page, an API call the JSON error msg.
func lookupFailed(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			!strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/ws/") &&
			strings.Contains(r.Header.Get("Accept"), "text/html")
		if !page {
			httpx.Error(w, http.StatusInternalServerError, msg)
			return
		}
		s := landingLocales[resolveLang(w, r)]
		w.Header().Set("Retry-After", "60")
		renderErrorPage(w, r, http.StatusServiceUnavailable, s.UnavailableTitle, s.UnavailableHeading, s.UnavailableBody)
	}
}

// renderTopbarPage reads an embedded HTML template, splices in the
// topbar locale strings, and writes the result. Used for pages whose
// body is plain English (today: manual, pricing) but whose shared
// topbar + footer still need to honour the caller's locale and
// surface the language picker correctly. The page body should
// reference the topbar/footer keys via {{KEY}} placeholders.
//
// Also splices the live pricing constants so the manual's "first N
// free, then €X per player" copy stays in sync with whatever
// admin.PricePerPlayerCents / FreePlayersPerEvent are configured to —
// no more risk of the documentation lying about the price.
func renderTopbarPage(w http.ResponseWriter, r *http.Request, name string) {
	lang := resolveLang(w, r)
	// The prices are part of the key: a save on Settings → Pricing
	// renders the page anew.
	out := cachedRender(lang, name, pricingKey(), func() []byte {
		body, err := readWebFile(name)
		if err != nil {
			return nil
		}
		s := landingLocales[lang]
		txt := string(body)
		for _, sub := range topbarSubs(s, lang) {
			txt = strings.ReplaceAll(txt, sub[0], sub[1])
		}
		for _, sub := range pricingSubs() {
			txt = strings.ReplaceAll(txt, sub[0], sub[1])
		}
		return []byte(txt)
	})
	if out == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// pricingSubs renders {{FREE_PLAYERS_PER_EVENT}} and
// {{PRICE_PER_EXTRA_PLAYER}} from the prices in force (Settings →
// Pricing), so the manual never disagrees with the charge.
func pricingSubs() [][2]string {
	p := settings.CurrentPricing()
	price := strconv.FormatInt(p.PlayerCents/100, 10)
	if minor := p.PlayerCents % 100; minor != 0 {
		price += "," + fmt.Sprintf("%02d", minor)
	}
	return [][2]string{
		{"{{FREE_PLAYERS_PER_EVENT}}", strconv.FormatInt(p.FreePlayers, 10)},
		{"{{PRICE_PER_EXTRA_PLAYER}}", price + " kr"},
	}
}

func pricingKey() string {
	p := settings.CurrentPricing()
	return strconv.FormatInt(p.PlayerCents, 10) + "/" + strconv.FormatInt(p.FreePlayers, 10)
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
//
// companyName (ZV_COMPANY_NAME) names the operator in the schema.org
// Organization node; empty omits the node rather than claiming a
// publisher the operator never configured.
func renderLandingPage(w http.ResponseWriter, r *http.Request, publicURL, companyName, homeOrg string) {
	lang := resolveLang(w, r)
	// Absolute URL for og:url + og:image. Falls back to "/" when no
	// PublicURL is configured (dev / unconfigured self-host) — that's
	// no worse than what shipped before, and the page still renders.
	landingURL := strings.TrimRight(publicURL, "/")
	if landingURL == "" {
		landingURL = "/"
	}
	out := cachedRender(lang, "web/landing.html", landingURL+"\x00"+companyName+"\x00"+homeOrg, func() []byte {
		return buildLandingBody(lang, landingURL, companyName, homeOrg)
	})
	if out == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Vary on Accept-Language + Cookie so caches don't serve the
	// English version to a Swedish-cookie visitor.
	w.Header().Set("Vary", "Cookie, Accept-Language")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// buildLandingBody is the actual substitution loop, factored out of
// renderLandingPage so it can be invoked once (per lang+publicURL)
// from inside the cachedRender callback. Reads the embedded template
// fresh every call so an override-dir hit picks up edits — the
// cachedRender wrapper short-circuits in dev anyway.
func buildLandingBody(lang, landingURL, companyName, homeOrg string) []byte {
	body, err := readWebFile("web/landing.html")
	if err != nil {
		return nil
	}
	s := landingLocales[lang]
	out := string(body)
	subs := topbarSubs(s, lang)
	subs = append(subs,
		// <head> meta — landing-specific, not part of the shared
		// topbar (manual / pricing have their own page-level meta).
		[2]string{"{{TITLE}}", s.Title},
		[2]string{"{{META_DESC}}", s.MetaDesc},
		[2]string{"{{OG_DESC}}", s.OGDesc},
		[2]string{"{{SCHEMA_ORG_NODE}}", schemaOrgNode(companyName, s.SchemaOrgDesc)},
		[2]string{"{{SCHEMA_ORG_DESC}}", s.SchemaOrgDesc},
		[2]string{"{{SCHEMA_APP_DESC}}", s.SchemaAppDesc},
		[2]string{"{{LANDING_URL}}", landingURL},
		// The home organisation's adventures, which the landing page
		// lists; empty when the site has none, and the list stays empty.
		[2]string{"{{HOME_STORE_API}}", homeStoreAPI(homeOrg)},

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
	return []byte(out)
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

// schemaOrgNode renders the landing's JSON-LD Organization entry,
// trailing comma included so it sits in front of the
// SoftwareApplication node. Empty name → no node.
func schemaOrgNode(name, desc string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	b, err := json.Marshal(map[string]string{
		"@type":       "Organization",
		"name":        name,
		"description": desc,
	})
	if err != nil {
		return ""
	}
	return "\n    " + string(b) + ","
}

// homeStoreAPI is where the home organisation's store items are read,
// or empty when the site has no home organisation.
func homeStoreAPI(homeOrg string) string {
	if homeOrg == "" {
		return ""
	}
	return "/api/o/" + url.PathEscape(homeOrg) + "/store"
}
