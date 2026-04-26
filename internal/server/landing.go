package server

import (
	"net/http"
	"strings"
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
var landingLocales = map[string]landingStrings{
	"en": {
		LangCode:      "en",
		LangAriaLabel: "Language",

		Title:          "Zonventure — adventures your players will remember",
		MetaDesc:       "The platform for arrangers running treasure hunts, team-building events, school field trips and city tours. Author the puzzles, schedule an event, watch your players walk and solve.",
		OGDesc:         "Treasure hunts. Team-builds. Field trips. City walks. You author the puzzles, your players walk and solve.",
		SchemaOrgDesc:  "Platform for running location-based adventure games — treasure hunts, team-building events, school field trips, city tours.",
		SchemaAppDesc:  "Author location-based adventure games and run them for teams of players using their phones.",

		NavPrimaryARIA: "Primary",
		NavToggleARIA:  "Toggle navigation",
		BrandARIA:      "Zonventure home",
		NavManual:      "Manual",
		NavPricing:     "Pricing",
		NavLogin:       "Log in",

		HeroTag:     "Adventure platform for arrangers",
		HeroHeading: "Adventures your players will remember.",
		HeroLede:    "Treasure hunts. Team-builds. Field trips. City walks. You design the story; your players walk, solve, and finish with the kind of grin that gets posted to LinkedIn.",
		CTAFree:     "Get started — it's free",
		CTAPossible: "See what's possible",
		ScrollHint:  "Scroll",

		BenefitsARIA:  "At-a-glance benefits",
		Benefit1Title: "No app to install",
		Benefit1Body:  "Players join via a magic link. No app store, no friction, no excuses.",
		Benefit2Title: "Branded for your event",
		Benefit2Body:  "Your colours, your logo, your intro story. White-labelled out of the box.",
		Benefit3Title: "Live audience board",
		Benefit3Body:  "A leaderboard URL for your lobby TV. Spectators cheer in real time.",

		UsecasesEyebrow: "Built for arrangers",
		UsecasesHeading: "Whoever your players are, the platform stays out of the way.",
		UsecasesSub:     "Three of the most common shapes — but if you've got a different one in mind, the same tools handle it.",
		Usecase1Title:   "Corporate team-builds",
		Usecase1Body:    "Stage a treasure hunt as the offsite event everyone asks for again next year.",
		Usecase2Title:   "Schools &amp; youth groups",
		Usecase2Body:    "Outdoor learning that gets pupils moving, thinking, and finishing things together.",
		Usecase3Title:   "Tourism &amp; city walks",
		Usecase3Body:    "Sell self-guided adventures around your city. Visitors play whenever it suits them.",
		UsecaseLink:     "See how it works →",

		HowEyebrow: "From idea to event",
		HowHeading: "Three steps. One afternoon.",
		HowSub:     "No agency. No production team. Just you, the puzzles, and a date.",
		Step1Title: "Author",
		Step1Body:  "Write your puzzles, drop them on the map, attach hints and photos.",
		Step2Title: "Schedule",
		Step2Body:  "Pick a date. Invite teams. Each player gets a personal magic link.",
		Step3Title: "Watch them play",
		Step3Body:  "Live activity feed. Audience leaderboard. The look on their faces when they finish.",

		QuoteARIA:      "What arrangers say",
		QuoteBody:      "The thing nobody tells you about a good treasure hunt is that an hour in, the players forget you organised it. They think it&#39;s magic. That&#39;s the highest compliment a platform like this can earn.",
		QuoteAttrLabel: "An arranger",
		QuoteAttrAfter: ", after their first event",

		ClosingHeading: "Ready to plan something they&#39;ll talk about for years?",
		ClosingBody:    "Sign up for free. Browse the global catalog or author your own. Buy team slots only when you want a real event.",
		CTAGetStarted:  "Get started",
		CTAReadManual:  "Read the manual",

		FooterARIA:    "Footer",
		FooterPrivacy: "Privacy",
		FooterTerms:   "Terms",
		FooterCopy:    "© Zonventure. Adventures, made by you.",
	},

	"sv": {
		LangCode:      "sv",
		LangAriaLabel: "Språk",

		Title:          "Zonventure — äventyr dina spelare aldrig glömmer",
		MetaDesc:       "Plattformen för arrangörer som kör skattjakter, lagevent, skolutflykter och stadsvandringar. Skapa pussel, schemalägg ett event och se dina spelare gå och lösa.",
		OGDesc:         "Skattjakter. Lagevent. Skolutflykter. Stadsvandringar. Du skapar pusslen — dina spelare går och löser.",
		SchemaOrgDesc:  "Plattform för platsbaserade äventyrsspel — skattjakter, lagevent, skolutflykter, stadsvandringar.",
		SchemaAppDesc:  "Skapa platsbaserade äventyrsspel och kör dem för lag av spelare med deras telefoner.",

		NavPrimaryARIA: "Huvudnavigering",
		NavToggleARIA:  "Visa/dölj meny",
		BrandARIA:      "Zonventure hem",
		NavManual:      "Manual",
		NavPricing:     "Priser",
		NavLogin:       "Logga in",

		HeroTag:     "Äventyrsplattform för arrangörer",
		HeroHeading: "Äventyr som spelarna minns.",
		HeroLede:    "Skattjakter. Lagevent. Skolutflykter. Stadsvandringar. Du formar berättelsen; dina spelare går, löser och avslutar med ett leende värt en LinkedIn-post.",
		CTAFree:     "Kom igång — det är gratis",
		CTAPossible: "Se vad som är möjligt",
		ScrollHint:  "Skrolla",

		BenefitsARIA:  "Snabba fördelar",
		Benefit1Title: "Ingen app att installera",
		Benefit1Body:  "Spelarna ansluter via en magisk länk. Ingen app store, ingen friktion, inga ursäkter.",
		Benefit2Title: "Anpassat efter ditt event",
		Benefit2Body:  "Era färger, er logotyp, er intro-berättelse. Helt white-label från start.",
		Benefit3Title: "Livetavla för publiken",
		Benefit3Body:  "En topplistelänk för lobbyns TV. Publiken hejar i realtid.",

		UsecasesEyebrow: "Byggd för arrangörer",
		UsecasesHeading: "Oavsett vilka spelarna är håller plattformen sig ur vägen.",
		UsecasesSub:     "Tre av de vanligaste formaten — men har du något annat i åtanke fungerar samma verktyg.",
		Usecase1Title:   "Företagsevent",
		Usecase1Body:    "Arrangera en skattjakt som blir det offsite-event alla vill göra om nästa år.",
		Usecase2Title:   "Skolor &amp; ungdomsgrupper",
		Usecase2Body:    "Utomhuspedagogik som får eleverna att röra sig, tänka och slutföra något tillsammans.",
		Usecase3Title:   "Turism &amp; stadsvandringar",
		Usecase3Body:    "Sälj självgående äventyr i staden. Besökarna spelar när det passar dem.",
		UsecaseLink:     "Så funkar det →",

		HowEyebrow: "Från idé till event",
		HowHeading: "Tre steg. En eftermiddag.",
		HowSub:     "Ingen byrå. Inget produktionsteam. Bara du, pusslen och ett datum.",
		Step1Title: "Skapa",
		Step1Body:  "Skriv dina pussel, placera dem på kartan, lägg till ledtrådar och bilder.",
		Step2Title: "Schemalägg",
		Step2Body:  "Välj ett datum. Bjud in lag. Varje spelare får en personlig magisk länk.",
		Step3Title: "Se dem spela",
		Step3Body:  "Liveflöde. Topplista för publiken. Och uttrycket i deras ansikten när de är klara.",

		QuoteARIA:      "Vad arrangörer säger",
		QuoteBody:      "Det ingen säger om en bra skattjakt är att efter en timme har spelarna glömt att du arrangerat den. De tror det är magi. Det är det finaste betyget en plattform kan få.",
		QuoteAttrLabel: "En arrangör",
		QuoteAttrAfter: ", efter sitt första event",

		ClosingHeading: "Redo att planera något de pratar om i flera år?",
		ClosingBody:    "Skapa konto gratis. Bläddra i den globala katalogen eller skapa egna äventyr. Köp bara lagplatser när du faktiskt ska köra event.",
		CTAGetStarted:  "Kom igång",
		CTAReadManual:  "Läs manualen",

		FooterARIA:    "Sidfot",
		FooterPrivacy: "Integritet",
		FooterTerms:   "Villkor",
		FooterCopy:    "© Zonventure. Äventyr — skapade av dig.",
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
	body, err := webFS.ReadFile(name)
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
	body, err := webFS.ReadFile("web/landing.html")
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
