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
var landingLocales = map[string]landingStrings{
	"en": {
		LangCode:      "en",
		LangAriaLabel: "Language",

		Title:         "Zonventure",
		MetaDesc:      "Zonventure is a self-hostable platform for running location-based adventure games. Author puzzles, schedule events, run a live experience for teams of players.",
		OGDesc:        "Self-hostable platform for location-based adventure games. Author puzzles, schedule events, run a live experience for teams.",
		SchemaOrgDesc: "Self-hostable platform for location-based adventure games.",
		SchemaAppDesc: "Author location-based adventure games and run them for teams of players using their phones.",

		NavPrimaryARIA: "Primary",
		NavToggleARIA:  "Toggle navigation",
		BrandARIA:      "Zonventure home",
		NavManual:      "Manual",
		NavPricing:     "Pricing",
		NavLogin:       "Log in",

		HeroTag:     "Open source · Self-hosted",
		HeroHeading: "Location-based adventure games.",
		HeroLede:    "Author puzzles, schedule events, run a live experience for teams of players. Players join via a magic link — no app to install, works on any phone.",
		CTAFree:     "Get started",

		BenefitsARIA:  "Features",
		Benefit1Title: "No app to install",
		Benefit1Body:  "Players join via a magic link. Works on any phone with a browser.",
		Benefit2Title: "Branded per adventure",
		Benefit2Body:  "Per-adventure colours and intro story. White-labelled out of the box.",
		Benefit3Title: "Live audience board",
		Benefit3Body:  "Leaderboard URL for a lobby TV so spectators can follow in real time.",

		ClosingHeading: "Run your first event.",
		ClosingBody:    "Sign in or create an account to start authoring puzzles and scheduling events.",
		CTAGetStarted:  "Sign in",
		CTAReadManual:  "Read the manual",

		FooterARIA:    "Footer",
		FooterPrivacy: "Privacy",
		FooterTerms:   "Terms",
		FooterCopy:    "Zonventure — open source adventure platform.",
	},

	"sv": {
		LangCode:      "sv",
		LangAriaLabel: "Språk",

		Title:         "Zonventure",
		MetaDesc:      "Zonventure är en självhostad plattform för platsbaserade äventyrsspel. Skapa pussel, schemalägg event och kör en live-upplevelse för lag av spelare.",
		OGDesc:        "Självhostad plattform för platsbaserade äventyrsspel. Skapa pussel, schemalägg event, kör en live-upplevelse.",
		SchemaOrgDesc: "Självhostad plattform för platsbaserade äventyrsspel.",
		SchemaAppDesc: "Skapa platsbaserade äventyrsspel och kör dem för lag av spelare med deras telefoner.",

		NavPrimaryARIA: "Huvudnavigering",
		NavToggleARIA:  "Visa/dölj meny",
		BrandARIA:      "Zonventure hem",
		NavManual:      "Manual",
		NavPricing:     "Priser",
		NavLogin:       "Logga in",

		HeroTag:     "Öppen källkod · Självhostad",
		HeroHeading: "Platsbaserade äventyrsspel.",
		HeroLede:    "Skapa pussel, schemalägg event, kör en live-upplevelse för lag av spelare. Spelarna ansluter via en magisk länk — ingen app att installera, fungerar i valfri mobil.",
		CTAFree:     "Kom igång",

		BenefitsARIA:  "Funktioner",
		Benefit1Title: "Ingen app att installera",
		Benefit1Body:  "Spelarna ansluter via en magisk länk. Fungerar i valfri telefon med webbläsare.",
		Benefit2Title: "Anpassat per äventyr",
		Benefit2Body:  "Egna färger och intro-berättelse per äventyr. White-label från start.",
		Benefit3Title: "Livetavla för publiken",
		Benefit3Body:  "Topplistelänk för en lobby-TV så publiken kan följa i realtid.",

		ClosingHeading: "Kör ditt första event.",
		ClosingBody:    "Logga in eller skapa ett konto för att börja skapa pussel och schemalägga event.",
		CTAGetStarted:  "Logga in",
		CTAReadManual:  "Läs manualen",

		FooterARIA:    "Sidfot",
		FooterPrivacy: "Integritet",
		FooterTerms:   "Villkor",
		FooterCopy:    "Zonventure — öppen källkods äventyrsplattform.",
	},
}

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
func renderLandingPage(w http.ResponseWriter, r *http.Request) {
	body, err := readWebFile("web/landing.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	lang := resolveLang(w, r)
	s := landingLocales[lang]
	out := string(body)
	subs := [][2]string{
		{"{{LANG_CODE}}", s.LangCode},
		{"{{LANG_ARIA}}", s.LangAriaLabel},
		{"{{TITLE}}", s.Title},
		{"{{META_DESC}}", s.MetaDesc},
		{"{{OG_DESC}}", s.OGDesc},
		{"{{SCHEMA_ORG_DESC}}", s.SchemaOrgDesc},
		{"{{SCHEMA_APP_DESC}}", s.SchemaAppDesc},
		{"{{NAV_PRIMARY_ARIA}}", s.NavPrimaryARIA},
		{"{{NAV_TOGGLE_ARIA}}", s.NavToggleARIA},
		{"{{BRAND_ARIA}}", s.BrandARIA},
		{"{{NAV_MANUAL}}", s.NavManual},
		{"{{NAV_PRICING}}", s.NavPricing},
		{"{{NAV_LOGIN}}", s.NavLogin},
		{"{{HERO_TAG}}", s.HeroTag},
		{"{{HERO_HEADING}}", s.HeroHeading},
		{"{{HERO_LEDE}}", s.HeroLede},
		{"{{CTA_FREE}}", s.CTAFree},
		{"{{CTA_POSSIBLE}}", s.CTAPossible},
		{"{{SCROLL_HINT}}", s.ScrollHint},
		{"{{BENEFITS_ARIA}}", s.BenefitsARIA},
		{"{{BENEFIT1_TITLE}}", s.Benefit1Title},
		{"{{BENEFIT1_BODY}}", s.Benefit1Body},
		{"{{BENEFIT2_TITLE}}", s.Benefit2Title},
		{"{{BENEFIT2_BODY}}", s.Benefit2Body},
		{"{{BENEFIT3_TITLE}}", s.Benefit3Title},
		{"{{BENEFIT3_BODY}}", s.Benefit3Body},
		{"{{USECASES_EYEBROW}}", s.UsecasesEyebrow},
		{"{{USECASES_HEADING}}", s.UsecasesHeading},
		{"{{USECASES_SUB}}", s.UsecasesSub},
		{"{{USECASE1_TITLE}}", s.Usecase1Title},
		{"{{USECASE1_BODY}}", s.Usecase1Body},
		{"{{USECASE2_TITLE}}", s.Usecase2Title},
		{"{{USECASE2_BODY}}", s.Usecase2Body},
		{"{{USECASE3_TITLE}}", s.Usecase3Title},
		{"{{USECASE3_BODY}}", s.Usecase3Body},
		{"{{USECASE_LINK}}", s.UsecaseLink},
		{"{{HOW_EYEBROW}}", s.HowEyebrow},
		{"{{HOW_HEADING}}", s.HowHeading},
		{"{{HOW_SUB}}", s.HowSub},
		{"{{STEP1_TITLE}}", s.Step1Title},
		{"{{STEP1_BODY}}", s.Step1Body},
		{"{{STEP2_TITLE}}", s.Step2Title},
		{"{{STEP2_BODY}}", s.Step2Body},
		{"{{STEP3_TITLE}}", s.Step3Title},
		{"{{STEP3_BODY}}", s.Step3Body},
		{"{{QUOTE_ARIA}}", s.QuoteARIA},
		{"{{QUOTE_BODY}}", s.QuoteBody},
		{"{{QUOTE_ATTR_LABEL}}", s.QuoteAttrLabel},
		{"{{QUOTE_ATTR_AFTER}}", s.QuoteAttrAfter},
		{"{{CLOSING_HEADING}}", s.ClosingHeading},
		{"{{CLOSING_BODY}}", s.ClosingBody},
		{"{{CTA_GET_STARTED}}", s.CTAGetStarted},
		{"{{CTA_READ_MANUAL}}", s.CTAReadManual},
		{"{{FOOTER_ARIA}}", s.FooterARIA},
		{"{{FOOTER_PRIVACY}}", s.FooterPrivacy},
		{"{{FOOTER_TERMS}}", s.FooterTerms},
		{"{{FOOTER_COPY}}", s.FooterCopy},
		// `aria-current="page"` on the active language link, blank on
		// the inactive one. Dropping the attribute entirely is cleaner
		// than `aria-current="false"` per WAI-ARIA practice.
		{"{{EN_CURRENT}}", ariaCurrentIf(lang == "en")},
		{"{{SV_CURRENT}}", ariaCurrentIf(lang == "sv")},
		{"{{EN_ACTIVE}}", activeClassIf(lang == "en")},
		{"{{SV_ACTIVE}}", activeClassIf(lang == "sv")},
	}
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
