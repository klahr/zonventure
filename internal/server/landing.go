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

		NavStore: "Store",

		StoreTitle:          "Store — Zonventure",
		StoreMetaDesc:       "Buy a ticket. Open the link on your phone. Play right now — no app, no signup, just an adventure waiting outside.",
		StoreOGDesc:         "Buy a ticket and play right now. One purchase, one team, magic link to your inbox.",
		StoreHeroEyebrow:    "Adventures · always on",
		StoreHeroHeading:    "Buy a ticket. Open the link. Play right now.",
		StoreHeroLede:       "No app, no signup. Pay once, your team gets a magic link, you head outside and start solving puzzles wherever the adventure takes you.",
		StoreSectionEyebrow: "Available now",
		StoreSectionHeading: "Pick an adventure",
		StoreLoadingHeading: "Loading…",
		StoreLoadingBody:    "Fetching this week's adventures.",
		StoreEmptyHeading:   "No tickets right now",
		StoreEmptyBody:      "Check back soon — adventures come and go.",
		StoreErrorHeading:   "Couldn't load the store",
		StoreErrorBody:      "Try refreshing in a moment.",

		StoreHowEyebrow: "How it works",
		StoreHowHeading: "From checkout to playing in three steps",
		StoreStep1Title: "Buy your ticket",
		StoreStep1Body:  "Pick an adventure, type your email, pay with card via Stripe. One ticket buys your whole team.",
		StoreStep2Title: "Open the magic link",
		StoreStep2Body:  "Check your inbox — we email you a magic link per teammate. Open it on a phone and you're in.",
		StoreStep3Title: "Head outside, start solving",
		StoreStep3Body:  "Show up at the meeting point on the listing, scan the team-share QR for friends, and the puzzles unlock as you walk.",

		StoreToastPaid:      "Payment received. Check your inbox — your magic link is on its way (give it a minute).",
		StoreToastCancelled: "Checkout cancelled. Nothing was charged.",

		StoreTeamSizeSolo:   "Solo ticket",
		StoreTeamSizePlural: "Up to {n} players",
		StoreMeetsAt:        "meets at",
		StoreCardCTA:        "Details & buy →",
		StorePerTicket:      "per ticket",
		StorePerTeam:        "per team",

		StoreEventCrumbs:           "← All adventures",
		StoreEventAboutEyebrow:     "About this adventure",
		StoreEventAboutHeading:     "What you're buying into",
		StoreEventMetaTeamSize:     "Team size",
		StoreEventMetaFormat:       "Format",
		StoreEventMetaFormatValue:  "Phone-based outdoor adventure",
		StoreEventMetaWhen:         "When",
		StoreEventMetaWhenValue:    "Always on — start whenever",
		StoreEventMetaMeetingPoint: "Meeting point",
		StoreEventBuyHeading:       "Get your ticket",
		StoreEventEmailLabel:       "Your email",
		StoreEventEmailPlaceholder: "you@example.com",
		StoreEventBuyCTA:           "Continue to payment — {{PRICE_DISPLAY}}",
		StoreEventTermsHTML:        `Payment by Stripe. By continuing you accept the <a href="/terms">terms</a> and <a href="/privacy">privacy notice</a>.`,
		StoreEventFeature1:         "Magic links emailed to your inbox",
		StoreEventFeature2:         "Bring up to {n} friends with the team-share QR",
		StoreEventFeature3:         "No app to install — opens in any phone browser",
		StoreEventFeature4:         "Refundable up to event start",
		StoreEventBuyNowCTA:        "Buy now",
		StoreEventHowCTA:           "How it works",
		StoreEventHeroPriceSuffix:  "per ticket · {team_label}",

		StoreEventFAQEyebrow: "FAQ",
		StoreEventFAQHeading: "Common questions",
		StoreEventFAQ1Q:      "What if my friends don't have the link?",
		StoreEventFAQ1A:      "The buyer becomes the team captain. Inside the play surface, there's a QR code — anyone who scans it on their phone joins the team and picks an open slot. One purchase covers up to {n} players.",
		StoreEventFAQ2Q:      "Can I refund my ticket?",
		StoreEventFAQ2A:      "Yes. Email the operator listed at the bottom of this page; refunds are processed via Stripe and the team's magic links are deactivated automatically when the refund clears.",
		StoreEventFAQ3Q:      "Do I need a special device?",
		StoreEventFAQ3A:      "Any phone with a browser and GPS. Tested on iOS Safari and Android Chrome. The PWA-style page works offline once loaded — useful in patchy coverage.",
		StoreEventFAQ4Q:      "What happens if I close the tab?",
		StoreEventFAQ4A:      "Your session stays active for the duration of the event. If the device is lost, use the \"I lost my link\" recovery on the play page with the same email you used at checkout.",

		StoreEventBadEmail:   "Please enter a valid email address.",
		StoreEventGenericErr: "Something went wrong, please try again.",
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

		NavStore: "Butik",

		StoreTitle:          "Butik — Zonventure",
		StoreMetaDesc:       "Köp en biljett. Öppna länken i mobilen. Spela nu — ingen app, ingen registrering, bara ett äventyr som väntar utomhus.",
		StoreOGDesc:         "Köp en biljett och spela nu. Ett köp, ett lag, magisk länk till din inkorg.",
		StoreHeroEyebrow:    "Äventyr · alltid på",
		StoreHeroHeading:    "Köp en biljett. Öppna länken. Spela nu.",
		StoreHeroLede:       "Ingen app, ingen registrering. Betala en gång, ditt lag får en magisk länk, ni går ut och börjar lösa pussel där äventyret tar er.",
		StoreSectionEyebrow: "Tillgängliga nu",
		StoreSectionHeading: "Välj ett äventyr",
		StoreLoadingHeading: "Laddar…",
		StoreLoadingBody:    "Hämtar veckans äventyr.",
		StoreEmptyHeading:   "Inga biljetter just nu",
		StoreEmptyBody:      "Kika in igen — äventyr kommer och går.",
		StoreErrorHeading:   "Kunde inte ladda butiken",
		StoreErrorBody:      "Försök uppdatera om en stund.",

		StoreHowEyebrow: "Så funkar det",
		StoreHowHeading: "Från betalning till spel i tre steg",
		StoreStep1Title: "Köp din biljett",
		StoreStep1Body:  "Välj ett äventyr, skriv din e-post, betala med kort via Stripe. En biljett räcker för hela laget.",
		StoreStep2Title: "Öppna den magiska länken",
		StoreStep2Body:  "Kolla din inkorg — vi mejlar en magisk länk per lagkamrat. Öppna den i mobilen så är du inne.",
		StoreStep3Title: "Ut och lös pussel",
		StoreStep3Body:  "Möt upp på platsen som står på sidan, visa lagkamraterna QR-koden, och pusslen låses upp medan ni går.",

		StoreToastPaid:      "Betalning mottagen. Kolla din inkorg — din magiska länk är på väg (ge det en minut).",
		StoreToastCancelled: "Betalning avbruten. Inget debiterades.",

		StoreTeamSizeSolo:   "Solobiljett",
		StoreTeamSizePlural: "Upp till {n} spelare",
		StoreMeetsAt:        "möts vid",
		StoreCardCTA:        "Detaljer & köp →",
		StorePerTicket:      "per biljett",
		StorePerTeam:        "per lag",

		StoreEventCrumbs:           "← Alla äventyr",
		StoreEventAboutEyebrow:     "Om det här äventyret",
		StoreEventAboutHeading:     "Vad du får för pengarna",
		StoreEventMetaTeamSize:     "Lagstorlek",
		StoreEventMetaFormat:       "Format",
		StoreEventMetaFormatValue:  "Mobilbaserat utomhusäventyr",
		StoreEventMetaWhen:         "När",
		StoreEventMetaWhenValue:    "Alltid på — börja när du vill",
		StoreEventMetaMeetingPoint: "Mötesplats",
		StoreEventBuyHeading:       "Hämta din biljett",
		StoreEventEmailLabel:       "Din e-post",
		StoreEventEmailPlaceholder: "du@exempel.se",
		StoreEventBuyCTA:           "Gå till betalning — {{PRICE_DISPLAY}}",
		StoreEventTermsHTML:        `Betalning via Stripe. Genom att fortsätta godkänner du <a href="/terms">villkoren</a> och <a href="/privacy">integritetspolicyn</a>.`,
		StoreEventFeature1:         "Magiska länkar mejlas till din inkorg",
		StoreEventFeature2:         "Ta med upp till {n} vänner via QR-kod",
		StoreEventFeature3:         "Ingen app behövs — öppnas i valfri mobilwebbläsare",
		StoreEventFeature4:         "Återbetalning fram till eventets start",
		StoreEventBuyNowCTA:        "Köp nu",
		StoreEventHowCTA:           "Så funkar det",
		StoreEventHeroPriceSuffix:  "per biljett · {team_label}",

		StoreEventFAQEyebrow: "Vanliga frågor",
		StoreEventFAQHeading: "Frågor och svar",
		StoreEventFAQ1Q:      "Tänk om mina vänner inte har länken?",
		StoreEventFAQ1A:      "Köparen blir lagets kapten. Inne i spelläget finns en QR-kod — den som skannar den i sin mobil ansluter till laget och väljer en ledig plats. Ett köp räcker för upp till {n} spelare.",
		StoreEventFAQ2Q:      "Kan jag få återbetalning?",
		StoreEventFAQ2A:      "Ja. Mejla operatören i sidfoten; återbetalningar går via Stripe och lagets magiska länkar avaktiveras automatiskt när återbetalningen är klar.",
		StoreEventFAQ3Q:      "Behöver jag en särskild enhet?",
		StoreEventFAQ3A:      "Vilken telefon som helst med webbläsare och GPS. Testat på iOS Safari och Android Chrome. PWA-sidan fungerar offline efter första laddningen — bra vid dålig täckning.",
		StoreEventFAQ4Q:      "Vad händer om jag stänger fliken?",
		StoreEventFAQ4A:      "Din session är aktiv under hela eventet. Om enheten försvinner, använd \"Jag tappade min länk\"-flödet på spelsidan med samma e-post som vid köpet.",

		StoreEventBadEmail:   "Ange en giltig e-postadress.",
		StoreEventGenericErr: "Något gick fel, försök igen.",
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

// storeSubs returns the {{KEY}} replacements specific to the public
// /store listing and /store/event/{id} detail pages — all the body
// copy, vocabulary fragments, and FAQ lines for the requested
// locale. Topbar / footer chrome subs come from topbarSubs and are
// merged separately so the same store template can be served by
// either renderStoreListingPage or renderStoreEventPage without
// duplicating the chrome list.
//
// {{NAV_STORE}} is here rather than in topbarSubs because today only
// the store templates render that nav entry; landing/manual/pricing
// don't list the store in their topbars yet, and adding it there
// would require their templates to ship the placeholder too.
func storeSubs(s landingStrings) [][2]string {
	return [][2]string{
		{"{{NAV_STORE}}", s.NavStore},

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
