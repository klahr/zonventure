package emailtpl

import (
	"fmt"
	"net/http"
	"strings"
)

// Languages are those emails are written in, the default first.
var Languages = []string{"en", "sv"}

// Lang is the email language for a language tag: "sv-SE" is "sv"; one
// emails are not written in is English.
func Lang(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	for _, l := range Languages {
		if l == tag {
			return l
		}
	}
	return "en"
}

// LangFromRequest is the language of whoever made r: the site's language
// cookie if they chose one, else what their browser asks for. For the
// emails a visitor asks for — signing up, resetting a password.
func LangFromRequest(r *http.Request) string {
	if c, err := r.Cookie("zv_lang"); err == nil && c.Value != "" {
		return Lang(c.Value)
	}
	al := r.Header.Get("Accept-Language")
	if i := strings.IndexAny(al, ",;"); i >= 0 {
		al = al[:i]
	}
	return Lang(al)
}

// T is s in lang, with args put in as fmt would. The English is the key;
// a translation may reorder the args with %[n]s.
func T(lang, s string, args ...any) string {
	if tr, ok := catalog[lang][s]; ok {
		s = tr
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// catalog is every email's words in each language but English.
var catalog = map[string]map[string]string{
	"sv": {
		// The page around every email.
		"%s · sent with %s": "%s · skickat med %s",

		// Signing in.
		"Reset your %s password":                              "Återställ ditt lösenord för %s",
		"Choose a new password — the link works for an hour.": "Välj ett nytt lösenord – länken fungerar i en timme.",
		"Reset your password":                                 "Återställ ditt lösenord",
		"Someone — hopefully you — asked to reset the password for this %s account.": "Någon – förhoppningsvis du – har bett om att få återställa lösenordet för det här kontot på %s.",
		"Choose a new password": "Välj ett nytt lösenord",
		"The link expires in 1 hour. If you didn't ask for this, ignore the email: your password stays as it is.": "Länken slutar fungera om en timme. Om det inte var du kan du strunta i mejlet: ditt lösenord är kvar som det är.",
		"Verify your %s email":     "Bekräfta din e-postadress för %s",
		"One click and you're in.": "Ett klick, så är du inne.",
		"Welcome to %s!":           "Välkommen till %s!",
		"Confirm your email address to finish setting up your account.": "Bekräfta din e-postadress för att bli klar med ditt konto.",
		"Confirm my email": "Bekräfta min e-postadress",
		"The link expires in 1 hour. If you didn't sign up, you can ignore this email.": "Länken slutar fungera om en timme. Om du inte har registrerat dig kan du strunta i mejlet.",
		"Confirm your new %s email":              "Bekräfta din nya e-postadress för %s",
		"Confirm the address to start using it.": "Bekräfta adressen för att börja använda den.",
		"Confirm your new email":                 "Bekräfta din nya e-postadress",
		"You asked to use this address for your %s account. Confirm it to make the change.": "Du har bett om att använda den här adressen för ditt konto på %s. Bekräfta den för att genomföra bytet.",
		"Confirm this address": "Bekräfta adressen",
		"If you didn't ask for this, ignore the email: your account keeps its current address.": "Om det inte var du kan du strunta i mejlet: ditt konto behåller sin nuvarande adress.",

		// Players.
		"You get this because you play with %s.":                 "Du får det här eftersom du spelar med %s.",
		"You get this because your team plays %[1]s with %[2]s.": "Du får det här eftersom ditt lag spelar %[1]s med %[2]s.",
		"Your team **%s** is ready to play. Forward each link to the right teammate — opening it claims that player's place and starts their game.": "Ditt lag **%s** är redo att spela. Skicka vidare varje länk till rätt lagkamrat – den som öppnar länken tar den platsen och börjar spela.",
		"A calendar invitation is attached, so the event lands in your calendar at the right time.":                                                 "En kalenderinbjudan finns bifogad, så att eventet hamnar i din kalender vid rätt tid.",
		"You're in!":         "Ni är med!",
		"You're in: %s":      "Ni är med: %s",
		"Your %s team links": "Era laglänkar till %s",
		"The links for team %s — one per player.":                             "Länkarna till laget %s – en per spelare.",
		"Your %s ticket — team link inside":                                   "Din biljett till %s – laglänken finns i mejlet",
		"Set up your team and start whenever you're ready.":                   "Sätt upp ert lag och börja när ni är redo.",
		"Your %s party — host link inside":                                    "Ditt %s-kalas – värdlänken finns i mejlet",
		"Set up the party, print the cards and start when everyone is there.": "Förbered kalaset, skriv ut korten och starta när alla är där.",
		"%s is booked": "%s är bokat",
		"Open the host page to split the children into teams, print and hide the cards, and start the game when everyone is there.": "Öppna värdsidan för att dela in barnen i lag, skriva ut och gömma korten och starta spelet när alla är där.",
		"Anyone with this link can run the party, so keep it to yourself.":                                                          "Den som har länken kan styra kalaset, så behåll den för dig själv.",
		"Open the host page": "Öppna värdsidan",

		"Everything about the players is deleted a while after the party.": "Allt om spelarna raderas en tid efter kalaset.",
		"Thanks — your adventure is ready":                                 "Tack – ert äventyr är redo",

		"Open your team page to claim your own place, share the others with your teammates, and start playing whenever you're ready.": "Öppna lagsidan för att ta din plats, dela de andra med dina lagkamrater och börja spela när ni är redo.",
		"Prefer to type it in? Go to %[1]s and enter your voucher code: **%[2]s**":                                                    "Vill du hellre skriva in den? Gå till %[1]s och ange din vouchers-kod: **%[2]s**",
		"Open the team page": "Öppna lagsidan",
		"The link stays valid while you play. Lost it? Use “I lost my link” on the play page with this email address.": "Länken fungerar så länge ni spelar. Tappat bort den? Välj ”Jag har tappat min länk” på spelsidan och ange den här e-postadressen.",
		"Your %s play links":           "Dina spellänkar till %s",
		"Your way back into the game.": "Din väg tillbaka in i spelet.",
		"Your play links":              "Dina spellänkar",
		"Here are fresh links for the teams this address is registered with. Each works on one device: opening it claims it for that device. Your old links no longer work.": "Här är nya länkar till lagen som den här adressen är registrerad i. Varje länk fungerar på en enhet: den enhet som öppnar länken tar den. Dina gamla länkar fungerar inte längre.",
		"- **%[1]s** in %[2]s, team %[3]s: [%[4]s](%[4]s)\n":                                                       "- **%[1]s** i %[2]s, laget %[3]s: [%[4]s](%[4]s)\n",
		"- **%[1]s**, team %[2]s: [%[3]s](%[3]s)\n":                                                                "- **%[1]s**, laget %[2]s: [%[3]s](%[3]s)\n",
		"You get this because someone asked for the links of this address. If it wasn't you, ignore it.":           "Du får det här eftersom någon bad om länkarna för den här adressen. Om det inte var du kan du strunta i det.",
		"Your %s box is confirmed!":                                                                                "Din %s-låda är bekräftad!",
		"The link stays valid for a year. Lost it? Use “I lost my link” on the play page with this email address.": "Länken fungerar i ett år. Tappat bort den? Välj ”Jag har tappat min länk” på spelsidan och ange den här e-postadressen.",
		"Your %s adventure — team link inside":                                                                     "Ditt äventyr på %s – laglänken finns i mejlet",
		"About your %s box":                                                                                        "Om din %s-låda",
		"We couldn't confirm the code from your %[1]s box with %[1]s.":                                             "Vi kunde inte bekräfta koden från din %[1]s-låda hos %[1]s.",
		"Please check the code and redeem it again, or reply to this email and we'll sort it out.":                 "Kontrollera koden och lös in den igen, eller svara på mejlet så löser vi det.",
		"Your %s box redemption is cancelled":                                                                      "Inlösen av din %s-låda är avbruten",
		"Your redemption of a %s box has been cancelled. Reply to this email if that's a surprise.":                "Inlösen av din %s-låda har avbrutits. Svara på mejlet om det kommer som en överraskning.",
		"A box redemption is waiting for review":                                                                   "En lådinlösen väntar på granskning",
		"Someone has redeemed a partner box. Check the code in the partner's portal, then approve or reject it.":   "Någon har löst in en partnerlåda. Kontrollera koden i partnerns portal och godkänn eller avvisa den sedan.",
		"Review the redemption":                                                                                    "Granska inlösen",

		// Designers.
		"You get this because you work on %s.":                      "Du får det här eftersom du arbetar med %s.",
		" Choose which emails you get in your account settings: %s": " Välj vilka mejl du får i dina kontoinställningar: %s",
		"“%[1]s” in %[2]s":                                          "”%[1]s” i %[2]s",
		"%[1]s mentioned you in %[2]s":                              "%[1]s nämnde dig i %[2]s",
		"%s mentioned you":                                          "%s nämnde dig",
		"On %s:":                                                    "I %s:",
		"Open the conversation":                                     "Öppna samtalet",
		"%[1]s gave you “%[2]s”":                                    "%[1]s gav dig ”%[2]s”",
		"You now own a step in %s.":                                 "Du är nu ansvarig för ett steg i %s.",
		"A step is yours":                                           "Ett steg är ditt",
		"%[1]s made you the owner of %[2]s. Its status, and the conversation about it, are in the step's inspector.": "%[1]s gjorde dig ansvarig för %[2]s. Stegets status och samtalet om det finns i stegets inspektör.",
		"Open the step": "Öppna steget",
		"%[1]s suggested a change to “%[2]s”":   "%[1]s föreslog en ändring av ”%[2]s”",
		"Accept or decline it in the designer.": "Godta eller avböj det i designern.",
		"A suggested change":                    "En föreslagen ändring",
		"%[1]s suggested a change to %[2]s, a step you own. You can see it field by field, then accept or decline it.": "%[1]s föreslog en ändring av %[2]s, ett steg du är ansvarig för. Du kan se den fält för fält och sedan godta eller avböja den.",
		"Review the suggestion":                             "Granska förslaget",
		"%[1]s accepted your suggestion on “%[2]s”":         "%[1]s godtog ditt förslag på ”%[2]s”",
		"%[1]s declined your suggestion on “%[2]s”":         "%[1]s avböjde ditt förslag på ”%[2]s”",
		"Your suggestion was accepted.":                     "Ditt förslag godtogs.",
		"Your suggestion was declined.":                     "Ditt förslag avböjdes.",
		"Suggestion accepted":                               "Förslaget godtogs",
		"Suggestion declined":                               "Förslaget avböjdes",
		"%[1]s accepted the change you suggested to %[2]s.": "%[1]s godtog ändringen du föreslog av %[2]s.",
		"%[1]s declined the change you suggested to %[2]s.": "%[1]s avböjde ändringen du föreslog av %[2]s.",
		"“%s” is ready for review":                          "”%s” är redo att granskas",
		"%s asks for a look.":                               "%s ber dig titta på det.",
		"Ready for review":                                  "Redo att granskas",
		"%[1]s marked %[2]s as needing review. It's yours, so have a look and set it to ready — or comment on what to change.": "%[1]s markerade att %[2]s behöver granskas. Steget är ditt, så titta på det och markera det som klart – eller kommentera vad som behöver ändras.",
		"Review the step": "Granska steget",
		"%s's assistant":  "%ss assistent",

		// Samples for the previews.
		"The Seagulls":                    "Måsarna",
		"Harbour Mystery":                 "Hamnmysteriet",
		"Meet at the lighthouse at 14:00": "Vi ses vid fyren kl. 14:00",
		"Hi {{team}}!\n\nWe start from the old lighthouse — look for the **red flag**.\nBring a charged phone and good shoes.\n\nSee you there!": "Hej {{team}}!\n\nVi startar vid den gamla fyren – leta efter den **röda flaggan**.\nTa med en laddad mobil och bra skor.\n\nVi ses!",
		"A letter has arrived": "Ett brev har kommit",
		"Dear detectives,\n\nThe keeper's last entry mentions a tide table. Find it before the water rises.\n\n— A friend": "Kära detektiver,\n\nFyrvaktarens sista anteckning nämner en tidvattentabell. Hitta den innan vattnet stiger.\n\n– En vän",
		"@ben could you make the second hint a bit less obvious?":                                                          "@ben kan du göra den andra ledtråden lite mindre uppenbar?",
		"The keeper's logbook":                                         "Fyrvaktarens loggbok",
		"A clearer title, so teams know where to look.":                "En tydligare titel, så att lagen vet var de ska leta.",
		"A message from the organisers":                                "Ett meddelande från arrangörerna",
		"Hi %s!\n\nThis is how your message will look.":                "Hej %s!\n\nSå här kommer ditt meddelande att se ut.",
		"Dear %s,\n\nThis is how an email step reads during the game.": "Kära %s,\n\nSå här ser ett mejl från ett steg ut under spelet.",
		"Saturday's game":                                              "lördagens spel",
	},
}
