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

		// Pictures' alt text.
		"A dotted route across a town, from map pin to map pin": "En prickad väg genom en stad, från kartnål till kartnål",
		"An envelope flying out of a phone":                     "Ett kuvert som flyger ut ur en mobil",
		"A golden ticket rising out of an envelope":             "En gyllene biljett som stiger upp ur ett kuvert",
		"A wrapped gift box with a key tag":                     "Ett inslaget presentpaket med en nyckellapp",
		"The adventure's cover picture":                         "Äventyrets omslagsbild",

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
		"The link expires in 24 hours. If you didn't sign up, you can ignore this email.": "Länken slutar fungera om ett dygn. Om du inte har registrerat dig kan du strunta i mejlet.",
		"Confirm your new %s email":                "Bekräfta din nya e-postadress för %s",
		"Confirm the address to start using it.":   "Bekräfta adressen för att börja använda den.",
		"%s invites you to join them":              "%s bjuder in dig att gå med",
		"Accept or decline when you next sign in.": "Tacka ja eller nej nästa gång du loggar in.",
		"Join %s?": "Gå med i %s?",
		"An admin of %s wants to add your %s account to their organization as an event host. Sign in to accept or decline.":                   "En admin i %s vill lägga till ditt konto på %s i sin organisation som eventvärd. Logga in för att tacka ja eller nej.",
		"An admin of %s wants to add your %s account to their organization as an admin. Sign in to accept or decline.":                        "En admin i %s vill lägga till ditt konto på %s i sin organisation som admin. Logga in för att tacka ja eller nej.",
		"An admin of %s wants to add your %s account to their organization as a writer. Sign in to accept or decline.":                        "En admin i %s vill lägga till ditt konto på %s i sin organisation som skribent. Logga in för att tacka ja eller nej.",
		"An admin of %s has made you a %s account to join their organization as an event host. Choose your password, then accept or decline.": "En admin i %s har skapat ett konto på %s åt dig så att du kan gå med i deras organisation som eventvärd. Välj ditt lösenord och tacka sedan ja eller nej.",
		"An admin of %s has made you a %s account to join their organization as an admin. Choose your password, then accept or decline.":      "En admin i %s har skapat ett konto på %s åt dig så att du kan gå med i deras organisation som admin. Välj ditt lösenord och tacka sedan ja eller nej.",
		"An admin of %s has made you a %s account to join their organization as a writer. Choose your password, then accept or decline.":      "En admin i %s har skapat ett konto på %s åt dig så att du kan gå med i deras organisation som skribent. Välj ditt lösenord och tacka sedan ja eller nej.",
		"Choose your password to answer. The link works for 7 days.":                                                                          "Välj ditt lösenord för att svara. Länken fungerar i 7 dagar.",
		"Choose your password": "Välj ditt lösenord",
		"If you don't know them, ignore this email: nothing happens unless you choose a password and accept.": "Känner du inte till dem kan du strunta i mejlet: ingenting händer om du inte väljer ett lösenord och tackar ja.",
		"Sign in to answer": "Logga in och svara",
		"If you don't know them, decline: you stay out of their organization.": "Om du inte känner dem, tacka nej: då förblir du utanför deras organisation.",
		"Confirm your new email": "Bekräfta din nya e-postadress",
		"You asked to use this address for your %s account. Confirm it to make the change.": "Du har bett om att använda den här adressen för ditt konto på %s. Bekräfta den för att genomföra bytet.",
		"Confirm this address": "Bekräfta adressen",
		"If you didn't ask for this, ignore the email: your account keeps its current address.":                            "Om det inte var du kan du strunta i mejlet: ditt konto behåller sin nuvarande adress.",
		"Security notice for your %s account":                                                                              "Säkerhetsmeddelande för ditt konto på %s",
		"Your email address was changed":                                                                                   "Din e-postadress har ändrats",
		"The email address for your %[1]s account was changed to %[2]s. This address no longer gets the account's emails.": "E-postadressen för ditt konto på %[1]s har ändrats till %[2]s. Den här adressen får inte längre kontots mejl.",
		"Your password was changed":                                                                                        "Ditt lösenord har ändrats",
		"The password for your %s account was changed.":                                                                    "Lösenordet för ditt konto på %s har ändrats.",
		"Two-factor authentication was turned on":                                                                          "Tvåfaktorsautentisering har slagits på",
		"Two-factor authentication was turned on for your %s account.":                                                     "Tvåfaktorsautentisering har slagits på för ditt konto på %s.",
		"Two-factor authentication was turned off":                                                                         "Tvåfaktorsautentisering har stängts av",
		"Two-factor authentication was turned off for your %s account.":                                                    "Tvåfaktorsautentisering har stängts av för ditt konto på %s.",
		"If this was you, there's nothing to do. If it wasn't, someone else may have access to your account: reset your password straight away.":                             "Om det var du behöver du inte göra något. Om det inte var du kan någon annan ha tillgång till ditt konto: återställ ditt lösenord direkt.",
		"If this was you, there's nothing to do. If it wasn't, someone else may have access to your account: reset your password straight away and write to %[1]s at %[2]s.": "Om det var du behöver du inte göra något. Om det inte var du kan någon annan ha tillgång till ditt konto: återställ ditt lösenord direkt och skriv till %[1]s på %[2]s.",
		"Put my address back":                      "Återställ min adress",
		"Your %s account will be deleted on %s":    "Ditt konto på %s raderas den %s",
		"Your account will be deleted":             "Ditt konto kommer att raderas",
		"Your %s account will be deleted tomorrow": "Ditt konto på %s raderas i morgon",
		"Your account will be deleted tomorrow":    "Ditt konto raderas i morgon",
		"You asked to delete your %[1]s account. It and everything only it holds will be deleted for good on %[2]s.": "Du har bett om att radera ditt konto på %[1]s. Det och allt som bara finns i det raderas för gott den %[2]s.",
		"Your %[1]s account will be deleted for good on %[2]s, as you asked. After that it can't be restored.":       "Ditt konto på %[1]s raderas för gott den %[2]s, som du bad om. Efter det går det inte att återställa.",
		"Changed your mind? Sign in and cancel the deletion before %s.":                                              "Ångrat dig? Logga in och avbryt raderingen före den %s.",
		"Keep my account": "Behåll mitt konto",
		"To keep the account, sign in before then and choose Cancel deletion. If you didn't ask for this, sign in, cancel it and change your password.":                                                 "Logga in före dess och välj Avbryt radering om du vill behålla kontot. Om det inte var du som bad om det: logga in, avbryt raderingen och byt lösenord.",
		"If this was you, there's nothing to do. If it wasn't, use the button within 7 days: it puts this address back, signs out every session and revokes every API token. Then reset your password.": "Om det var du behöver du inte göra något. Om det inte var du, använd knappen inom 7 dagar: den återställer den här adressen, loggar ut alla sessioner och återkallar alla API-nycklar. Återställ sedan ditt lösenord.",

		// Players.
		"You get this because you play with %s.":                 "Du får det här eftersom du spelar med %s.",
		"You get this because your team plays %[1]s with %[2]s.": "Du får det här eftersom ditt lag spelar %[1]s med %[2]s.",
		"Your team **%s** is ready to play. Forward each link to the right teammate — opening it claims that player's place and starts their game.": "Ditt lag **%s** är redo att spela. Skicka vidare varje länk till rätt lagkamrat – den som öppnar länken tar den platsen och börjar spela.",
		"A calendar invitation is attached, so the event lands in your calendar at the right time.":                                                 "En kalenderinbjudan finns bifogad, så att eventet hamnar i din kalender vid rätt tid.",
		"Player %d":          "Spelare %d",
		"Team %d":            "Lag %d",
		"You're in!":         "Du är med!",
		"You're in: %s":      "Du är med: %s",
		"Your %s team links": "Dina laglänkar till %s",
		"The links for team %s — one per player.":                            "Länkarna till laget %s – en per spelare.",
		"Your %s ticket — team link inside":                                  "Din biljett till %s – laglänken finns i mejlet",
		"Set up your team and start whenever you're ready.":                  "Sätt upp ditt lag och börja när du är redo.",
		"Your %s party — host link inside":                                   "Ditt %s-party – värdlänken finns i mejlet",
		"Set up the party, print the cards and show the players their link.": "Förbered partyt, skriv ut korten och visa spelarna deras länk.",
		"%s is booked": "%s är bokat",
		"Open the host page to print and hide the cards and show the players their link. The first team to join starts the game, within 60 days of booking; play as many games as you like for 14 days from then.": "Öppna värdsidan för att skriva ut och gömma korten och visa spelarna deras länk. Det första laget som ansluter startar spelet, inom 60 dagar från bokningen; spela hur många omgångar du vill i 14 dagar från dess.",
		"Save your party adventures":                                "Spara dina partyäventyr",
		"Confirm your email address to keep your party adventures.": "Bekräfta din mejladress för att spara dina partyäventyr.",
		"Open the link to save your party adventures to this address. Then you can open them from any phone or computer. The link works for an hour.": "Öppna länken för att spara dina partyäventyr på den här adressen. Sedan kan du öppna dem från vilken mobil eller dator som helst. Länken fungerar i en timme.",
		"Save my party adventures":                                     "Spara mina partyäventyr",
		"Didn't ask for this? Ignore this email and nothing is saved.": "Har du inte bett om det här? Strunta i mejlet så sparas ingenting.",
		"Your party adventures":                                        "Dina partyäventyr",
		"Open your party adventures.":                                  "Öppna dina partyäventyr.",
		"Open the link to see every party adventure saved to this address. The link works for an hour.": "Öppna länken för att se alla partyäventyr som sparats på den här adressen. Länken fungerar i en timme.",
		"Open my party adventures":                           "Öppna mina partyäventyr",
		"Didn't ask for this? Ignore this email.":            "Har du inte bett om det här? Strunta i mejlet.",
		"%s is ready to play":                                "%s är redo att spelas",
		"Print the cards, hide them and let the teams join.": "Skriv ut korten, göm dem och låt lagen ansluta.",
		"Open the host page to print the cards and the answer key, and to show the players their link. Start the first game by %s; from then you can play as many games as you like for 14 days.": "Öppna värdsidan för att skriva ut korten och facit och visa spelarna deras länk. Starta första omgången senast %s; därefter kan du spela hur många omgångar du vill i 14 dagar.",
		"Open the host page to print the cards and the answer key, and to show the players their link. You can play as many games as you like until %s.":                                          "Öppna värdsidan för att skriva ut korten och facit och visa spelarna deras länk. Ni kan spela hur många omgångar ni vill fram till %s.",
		"Players join here: %s": "Spelarna ansluter här: %s",
		"Anyone with the host link can run and change your party adventure, so keep it to yourself.":    "Den som har värdlänken kan köra och ändra ditt partyäventyr, så håll den för dig själv.",
		"Your receipt comes in a separate email.":                                                       "Kvittot kommer i ett separat mejl.",
		"%[1]s — party adventure, up to %[2]d teams per game":                                           "%[1]s – partyäventyr, upp till %[2]d lag per omgång",
		"%[1]s — more teams, up to %[2]d teams per game":                                                "%[1]s – fler lag, upp till %[2]d lag per omgång",
		"The party adventure and everything about the players is deleted 14 days after the first game.": "Partyäventyret och allt om spelarna raderas 14 dagar efter första omgången.",
		"Keep %s?": "Vill du behålla %s?",
		"Create a free account and the party adventure is yours to keep.":                                                                          "Skapa ett gratis konto så får du behålla partyäventyret.",
		"Your party adventure is deleted on %s. Create a free account and it moves there instead, cards and all, to play again whenever you like.": "Ditt partyäventyr raderas %s. Skapa ett gratis konto så flyttas det dit i stället, med alla kort, så att du kan spela igen när du vill.",
		"Moving it ends the games you have now; their summaries were already mailed to you.":                                                       "Flytten avslutar de omgångar du har nu; sammanfattningarna har redan mejlats till dig.",
		"Keep my party adventure":                                  "Behåll mitt partyäventyr",
		"Not interested? Do nothing and it is deleted as planned.": "Inte intresserad? Gör ingenting så raderas den som planerat.",
		"%s is in your account":                                    "%s finns nu i ditt konto",
		"Your party adventure is now an adventure in your account. Edit it, print it and host it from there. The summaries of the games you played were mailed to you before.": "Ditt partyäventyr är nu ett äventyr i ditt konto. Redigera, skriv ut och kör det därifrån. Sammanfattningarna av omgångarna du spelade har redan mejlats till dig.",
		"%s has ended": "%s har tagit slut",
		"Your party adventure's days are over, so it and everything about its players has been deleted. The summaries of the games you played were mailed to you before.": "Ditt partyäventyrs dagar är slut, så det och allt om spelarna har raderats. Sammanfattningarna av omgångarna du spelade har redan mejlats till dig.",
		"Thank you for playing!": "Tack för att du spelade!",
		"Join %s":                "Var med i %s",
		"You get this because the host of %s sent you the player link.":                  "Du får det här eftersom värden för %s skickade spellänken till dig.",
		"Open the link, name your team and play.":                                        "Öppna länken, namnge ditt lag och spela.",
		"Open the link on the phone your team plays on, give the team a name and start.": "Öppna länken i mobilen ditt lag spelar på, ge laget ett namn och starta.",
		"Join the game":               "Gå med i spelet",
		"%[1]d teams, %[2]d players.": "%[1]d lag, %[2]d spelare.",
		"Ranking":                     "Resultat",
		"%[1]d. %[2]s — %[3]s points": "%[1]d. %[2]s – %[3]s poäng",
		"%d min":                      "%d min",
		"did not finish":              "gick inte i mål",
		"Questions":                   "Frågor",
		"- %[1]s: %[2]d%% right (%[3]d of %[4]d teams)":                    "- %[1]s: %[2]d %% rätt (%[3]d av %[4]d lag)",
		"%[1]s — results of game %[2]d":                                    "%[1]s – resultat från omgång %[2]d",
		"Who won, and how the questions went.":                             "Vem som vann och hur det gick på frågorna.",
		"Game %d is over":                                                  "Omgång %d är slut",
		"You get this because you host %s.":                                "Du får det här eftersom du är värd för %s.",
		"Anyone with this link can run the party, so keep it to yourself.": "Den som har länken kan styra partyt, så behåll den för dig själv.",
		"Open the host page":                                               "Öppna värdsidan",

		"Everything about the players is deleted a while after the party.":             "Allt om spelarna raderas en tid efter partyt.",
		"Thanks — your adventure is ready":                                             "Tack – ditt äventyr är redo",
		"A gift from %s — your adventure is ready":                                     "En gåva från %s – ditt äventyr är redo",
		"%s has given you a ticket. Set up your team and start whenever you're ready.": "%s har gett dig en biljett. Ordna ditt lag och börja när du är redo.",
		"A gift from %[1]s: your ticket to %[2]s":                                      "En gåva från %[1]s: din biljett till %[2]s",

		"Open your team page to claim your own place, share the others with your teammates, and start playing whenever you're ready.": "Öppna lagsidan för att ta din plats, dela de andra platserna med dina lagkamrater och börja spela när du är redo.",
		"Prefer to type it in? Go to %[1]s and enter your voucher code: **%[2]s**":                                                    "Vill du hellre skriva in den? Gå till %[1]s och ange din kod: **%[2]s**",
		"Files from the organisers": "Filer från arrangörerna",
		"We're checking it with %s now. Once it checks out, we email your team link to this address, usually within a working day.": "Vi kontrollerar den med %s nu. När den stämmer mejlar vi länken till ditt lag till den här adressen, oftast inom en arbetsdag.",
		"Your envelope goes to:": "Kuvertet skickas till:",
		"Something wrong? Contact the organiser before the envelope is posted.": "Något som inte stämmer? Kontakta arrangören innan kuvertet skickas.",
		"We've got your %s code":                                "Vi har fått din kod från %s",
		"We're checking it and will email your team link soon.": "Vi kontrollerar den och mejlar länken till ditt lag snart.",
		"To print": "Att skriva ut",
		"Print these at home before you set off: the adventure starts with them.":  "Skriv ut dem hemma innan ni ger er av: äventyret börjar med dem.",
		"Download them before you set off. They are also in the menu of the game.": "Ladda ner dem innan ni ger er av. De finns också i spelets meny.",
		"Open the team page":          "Öppna lagsidan",
		"The link works for 30 days.": "Länken fungerar i 30 dagar.",
		"Lost it? Open the play page and enter this email address to get a new link.": "Tappat bort den? Öppna spelsidan och ange den här e-postadressen så får du en ny länk.",
		"Lost it? Go to %s and enter this email address to get a new link.":           "Tappat bort den? Gå till %s och ange den här e-postadressen så får du en ny länk.",
		"Your %s play links":           "Dina spellänkar till %s",
		"Your way back into the game.": "Din väg tillbaka in i spelet.",
		"Your play links":              "Dina spellänkar",
		"Here are the links for the teams this address is registered with. Each works on one device: opening it claims it for that device.": "Här är länkarna till lagen som den här adressen är registrerad i. Varje länk fungerar på en enhet: den enhet som öppnar länken tar den.",
		"- **%[1]s** in %[2]s, team %[3]s: [%[4]s](%[4]s)\n":                                                     "- **%[1]s** i %[2]s, laget %[3]s: [%[4]s](%[4]s)\n",
		"- **%[1]s**, team %[2]s: [%[3]s](%[3]s)\n":                                                              "- **%[1]s**, laget %[2]s: [%[3]s](%[3]s)\n",
		"You get this because someone asked for the links of this address. Your links are the same as before.":   "Du får det här eftersom någon bad om länkarna för den här adressen. Dina länkar är desamma som förut.",
		"Your %s box is confirmed!":                                                                              "Din %s-låda är bekräftad!",
		"The link works for a year.":                                                                             "Länken fungerar i ett år.",
		"Your %s adventure — team link inside":                                                                   "Ditt äventyr på %s – laglänken finns i mejlet",
		"About your %s box":                                                                                      "Om din %s-låda",
		"We couldn't confirm the code from your %[1]s box with %[1]s.":                                           "Vi kunde inte bekräfta koden från din %[1]s-låda hos %[1]s.",
		"The organiser's reason: %s":                                                                             "Arrangörens förklaring: %s",
		"Please check the code and redeem it again, or reply to this email and we'll sort it out.":               "Kontrollera koden och lös in den igen, eller svara på mejlet så löser vi det.",
		"Your %s box redemption is cancelled":                                                                    "Inlösen av din %s-låda är avbruten",
		"Your redemption of a %s box has been cancelled. Reply to this email if that's a surprise.":              "Inlösen av din %s-låda har avbrutits. Svara på mejlet om det kommer som en överraskning.",
		"A box redemption is waiting for review":                                                                 "En lådinlösen väntar på granskning",
		"Someone has redeemed a partner box. Check the code in the partner's portal, then approve or reject it.": "Någon har löst in en partnerlåda. Kontrollera koden i partnerns portal och godkänn eller avvisa den sedan.",
		"Review the redemption":                                                                                  "Granska inlösen",
		"Listing review log: today's fingerprint":                                                                "Granskningsloggen för annonser: dagens fingeravtryck",
		"The listing review log holds %d entries. Its latest hash is:":                                           "Granskningsloggen för annonser har %d poster. Dess senaste hash är:",
		"Keep this email. Checking the log recomputes every hash; if its latest no longer matches one sent here, the log was changed.": "Spara det här mejlet. När loggen kontrolleras räknas varje hash om; stämmer den senaste inte längre med en som skickats hit har loggen ändrats.",
		"Today's check found nothing wrong: the log adds up, and approved listings serve what was approved.":                           "Dagens kontroll hittade inget fel: loggen stämmer, och godkända annonser visar det som godkändes.",
		"Listing review check: %d problems": "Granskningskontroll: %d problem",
		"Today's check found %d problems: something was changed outside review. Open the log and check it.": "Dagens kontroll hittade %d problem: något har ändrats utanför granskningen. Öppna loggen och kontrollera den.",
		"Check the log":                   "Kontrollera loggen",
		"A listing is waiting for review": "En annons väntar på granskning",
		"%s has put something up for sale. It goes public once it is approved.": "%s har lagt ut något till försäljning. Det blir publikt när det har godkänts.",
		"Review listings":  "Granska annonser",
		"“%s” is approved": "”%s” är godkänd",
		"Your listing for “%s” is approved and now public.": "Din annons för ”%s” är godkänd och nu publik.",
		"Open it":               "Öppna den",
		"“%s” was not approved": "”%s” godkändes inte",
		"Your listing for “%s” was not approved. If an earlier version is public, it stays as it was. Change the listing, save it and press Submit for review.": "Din annons för ”%s” godkändes inte. Om en tidigare version är publik ligger den kvar som den var. Ändra annonsen, spara den och tryck på Skicka in för granskning.",
		"Your organisation page is approved":                                        "Er organisationssida är godkänd",
		"Your organisation page was not approved":                                   "Er organisationssida godkändes inte",
		"The changes to the organisation page of “%s” are approved and now public.": "Ändringarna av organisationssidan för ”%s” är godkända och nu publika.",
		"The changes to the organisation page of “%s” were not approved. What is public now stays as it was. Change the page under Settings → Organization, save it and press Submit for review.": "Ändringarna av organisationssidan för ”%s” godkändes inte. Det som är publikt nu ligger kvar som det var. Ändra sidan under Inställningar → Organisation, spara den och tryck på Skicka in för granskning.",

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
		"The keeper's logbook":                                                        "Fyrvaktarens loggbok",
		"A clearer title, so teams know where to look.":                               "En tydligare titel, så att lagen vet var de ska leta.",
		"A message from the organisers":                                               "Ett meddelande från arrangörerna",
		"New file: %s. Open the menu and look under “Files from the organisers”.":     "Ny fil: %s. Öppna menyn och titta under ”Filer från arrangörerna”.",
		"%d new files: %s. Open the menu and look under “Files from the organisers”.": "%d nya filer: %s. Öppna menyn och titta under ”Filer från arrangörerna”.",
		"%s is cancelled":                   "%s är inställt",
		"Your player links no longer work.": "Era spelarlänkar fungerar inte längre.",
		"Hi %[1]s! The organisers have cancelled %[2]s, so your player links no longer work. Questions? Contact the organisers.": "Hej %[1]s! Arrangörerna har ställt in %[2]s, så era spelarlänkar fungerar inte längre. Frågor? Kontakta arrangörerna.",
		"Hi %s!\n\nThis is how your message will look.":                "Hej %s!\n\nSå här kommer ditt meddelande att se ut.",
		"Dear %s,\n\nThis is how an email step reads during the game.": "Kära %s,\n\nSå här ser ett mejl från ett steg ut under spelet.",
		"Saturday's game": "lördagens spel",

		// Start time, meeting place and the organiser's contact address.
		"It starts **%s**.":                           "Det börjar **%s**.",
		"The time has changed: it now starts **%s**.": "Tiden har ändrats: det börjar nu **%s**.",
		"Meeting place: **%s**":                       "Mötesplats: **%s**",
		"New time: %s":                                "Ny tid: %s",
		"It now starts %s.":                           "Det börjar nu %s.",
		"New time for %s":                             "Ny tid för %s",
		"Questions? Write to the organiser at %s.":    "Frågor? Skriv till arrangören på %s.",
		"Something wrong? Write to the organiser at %s before the envelope is posted.":                                                     "Något som inte stämmer? Skriv till arrangören på %s innan kuvertet skickas.",
		"Please check the code and redeem it again, or write to %s and we'll sort it out.":                                                 "Kontrollera koden och lös in den igen, eller skriv till %s så löser vi det.",
		"Hi %[1]s! The organisers have cancelled %[2]s, so your player links no longer work. Questions? Write to the organisers at %[3]s.": "Hej %[1]s! Arrangörerna har ställt in %[2]s, så era spelarlänkar fungerar inte längre. Frågor? Skriv till arrangörerna på %[3]s.",

		// Receipts for event passes.
		"Event pass for %[1]d players: %[2]s":        "Eventpass för %[1]d spelare: %[2]s",
		"%[1]d more players on an event pass: %[2]s": "%[1]d spelare till på ett eventpass: %[2]s",
		"Receipt no. %d":                   "Kvitto nr %d",
		"Date: %s":                         "Datum: %s",
		"Seller: %s":                       "Säljare: %s",
		"Buyer: %s":                        "Köpare: %s",
		"Item: %s":                         "Avser: %s",
		"Amount paid: **%s**":              "Betalt belopp: **%s**",
		"This purchase has been refunded.": "Köpet har återbetalats.",
		"Thanks for your purchase.":        "Tack för ditt köp.",
		"Thanks for your purchase":         "Tack för ditt köp",
		"Receipt":                          "Kvitto",
		"Download receipt":                 "Ladda ner kvitto",
		"Your %s receipt":                  "Ditt kvitto från %s",
	},
}
