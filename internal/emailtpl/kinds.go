package emailtpl

import (
	"strconv"
	"strings"

	"github.com/klahr/zonventure/internal/sitename"
)

// The emails, one function each: what they say, in a language, given
// what they are about. Senders call them for the real thing and the
// preview calls them with Sample data, so a preview is exactly what will
// be sent. Every word goes through T, so each has its Swedish in lang.go.

// Lines keeps an author's line breaks in Markdown, as they wrote them:
// in-game emails and arrangers' messages are written as plain text that
// may use Markdown emphasis and links.
func Lines(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n")
	paras := strings.Split(s, "\n\n")
	for i, p := range paras {
		paras[i] = strings.ReplaceAll(strings.TrimSpace(p), "\n", "  \n")
	}
	return strings.Join(paras, "\n\n")
}

// mdEscape keeps a name from being read as Markdown.
func mdEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "`", "\\`", "<", "&lt;", "#", `\#`).Replace(s)
}

// --- signing in (always the platform's own brand) ---------------------------

func PasswordReset(lang, link string) (string, Content) {
	return T(lang, "Reset your %s password", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "Choose a new password — the link works for an hour."),
		Heading:   T(lang, "Reset your password"),
		Body:      T(lang, "Someone — hopefully you — asked to reset the password for this %s account.", mdEscape(sitename.Name())),
		Buttons:   []Button{{T(lang, "Choose a new password"), link}},
		Footer:    T(lang, "The link expires in 1 hour. If you didn't ask for this, ignore the email: your password stays as it is."),
	}
}

func VerifyEmail(lang, link string) (string, Content) {
	return T(lang, "Verify your %s email", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "One click and you're in."),
		Heading:   T(lang, "Welcome to %s!", sitename.Name()),
		Body:      T(lang, "Confirm your email address to finish setting up your account."),
		Buttons:   []Button{{T(lang, "Confirm my email"), link}},
		Footer:    T(lang, "The link expires in 1 hour. If you didn't sign up, you can ignore this email."),
	}
}

func EmailChange(lang, link string) (string, Content) {
	return T(lang, "Confirm your new %s email", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "Confirm the address to start using it."),
		Heading:   T(lang, "Confirm your new email"),
		Body:      T(lang, "You asked to use this address for your %s account. Confirm it to make the change.", mdEscape(sitename.Name())),
		Buttons:   []Button{{T(lang, "Confirm this address"), link}},
		Footer:    T(lang, "If you didn't ask for this, ignore the email: your account keeps its current address."),
	}
}

// --- players -----------------------------------------------------------------

// PlayerLink is one player's way into a game.
type PlayerLink struct {
	Team, Player, URL string
	Event             string
}

func playerFooter(lang, event, org string) string {
	if event == "" {
		return T(lang, "You get this because you play with %s.", org)
	}
	return T(lang, "You get this because your team plays %[1]s with %[2]s.", event, org)
}

// TeamInvitation is a team's links, one per player.
func TeamInvitation(lang, team, event, org string, players []PlayerLink, calendar bool) (string, Content) {
	var b strings.Builder
	b.WriteString(T(lang, "Your team **%s** is ready to play. Forward each link to the right teammate — opening it claims that player's place and starts their game.", mdEscape(team)))
	b.WriteString("\n\n")
	for _, p := range players {
		b.WriteString("- **" + mdEscape(p.Player) + "**: [" + p.URL + "](" + p.URL + ")\n")
	}
	if calendar {
		b.WriteString("\n" + T(lang, "A calendar invitation is attached, so the event lands in your calendar at the right time."))
	}
	heading := T(lang, "You're in!")
	if event != "" {
		heading = T(lang, "You're in: %s", event)
	}
	return T(lang, "Your %s team links", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "The links for team %s — one per player.", team),
		Heading:   heading,
		Body:      b.String(),
		Footer:    playerFooter(lang, event, org),
	}
}

// TeamLink is the one link a buyer sets their team up from. voucher (an
// already display-formatted code) and playURL are optional: when both
// are set the body also offers the type-it-in path, so a buyer reading
// the mail on a different device than the one they'll play on can just
// enter the code at /play. Pass "" for either to omit that line.
func TeamLink(lang, teamURL, playURL, voucher, org string) (string, Content) {
	body := T(lang, "Open your team page to claim your own place, share the others with your teammates, and start playing whenever you're ready.")
	if voucher != "" && playURL != "" {
		body += "\n\n" + T(lang, "Prefer to type it in? Go to %[1]s and enter your voucher code: **%[2]s**", playURL, voucher)
	}
	return T(lang, "Your %s ticket — team link inside", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "Set up your team and start whenever you're ready."),
		Heading:   T(lang, "Thanks — your adventure is ready"),
		Body:      body,
		Buttons:   []Button{{T(lang, "Open the team page"), teamURL}},
		Footer:    T(lang, "The link stays valid while you play. Lost it? Use “I lost my link” on the play page with this email address.") + " " + playerFooter(lang, "", org),
	}
}

// FileLink is one of the files the organisers left for an event's
// players, and where to download it.
type FileLink struct {
	Name, URL string
}

// WithFiles lists the event's files under the body, one download link
// each, for a ticket or an invitation; none leaves c as it is.
func WithFiles(c Content, files []FileLink) Content {
	if len(files) == 0 {
		return c
	}
	var b strings.Builder
	b.WriteString("\n\n**" + T(c.Lang, "Files from the organisers") + "**  \n")
	b.WriteString(T(c.Lang, "Download them before you set off. They are also in the menu of the game.") + "\n\n")
	for _, f := range files {
		b.WriteString("- [" + mdEscape(f.Name) + "](" + f.URL + ")\n")
	}
	c.Body += strings.TrimRight(b.String(), "\n")
	return c
}

// PartyHost is a hosted event's host link: the page the buyer sets the
// event up and runs it from. The link is their only key, so the mail
// says so.
func PartyHost(lang, hostURL, party, org string) (string, Content) {
	return T(lang, "Your %s party — host link inside", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "Set up the party, print the cards and show the players their link."),
		Heading:   T(lang, "%s is booked", party),
		Body: T(lang, "Open the host page to print and hide the cards and show the players their link. The first team to join starts the game; play as many games as you like for 14 days from then.") +
			"\n\n" + T(lang, "Anyone with this link can run the party, so keep it to yourself."),
		Buttons: []Button{{T(lang, "Open the host page"), hostURL}},
		Footer:  T(lang, "Everything about the players is deleted a while after the party.") + " " + playerFooter(lang, "", org),
	}
}

// HostedPlayerLink is the player link a host sends someone from the host page:
// it opens the game, where the team names itself.
func HostedPlayerLink(lang, playerURL, party, org string) (string, Content) {
	return T(lang, "Join %s", party), Content{
		Lang:      lang,
		Preheader: T(lang, "Open the link, name your team and play."),
		Heading:   T(lang, "Join %s", party),
		Body:      T(lang, "Open the link on the phone your team plays on, give the team a name and start."),
		Buttons:   []Button{{T(lang, "Join the game"), playerURL}},
		Footer:    playerFooter(lang, "", org),
	}
}

// TrailConfirm saves the trails a browser holds to an email address:
// the link confirms the address is the reader's.
func TrailConfirm(lang, link string) (string, Content) {
	return T(lang, "Save your party adventures"), Content{
		Lang:      lang,
		Preheader: T(lang, "Confirm your email address to keep your party adventures."),
		Heading:   T(lang, "Save your party adventures"),
		Body:      T(lang, "Open the link to save your party adventures to this address. Then you can open them from any phone or computer. The link works for an hour."),
		Buttons:   []Button{{T(lang, "Save my party adventures"), link}},
		Footer:    T(lang, "Didn't ask for this? Ignore this email and nothing is saved."),
	}
}

// TrailSignin opens the quiz trails saved to an email address.
func TrailSignin(lang, link string) (string, Content) {
	return T(lang, "Your party adventures"), Content{
		Lang:      lang,
		Preheader: T(lang, "Open your party adventures."),
		Heading:   T(lang, "Your party adventures"),
		Body:      T(lang, "Open the link to see every party adventure saved to this address. The link works for an hour."),
		Buttons:   []Button{{T(lang, "Open my party adventures"), link}},
		Footer:    T(lang, "Didn't ask for this? Ignore this email."),
	}
}

// TrailPaid is a paid quiz trail's host link, mailed to the buyer: where
// to print the cards and show the players their link. Stripe mails the
// receipt.
func TrailPaid(lang, hostURL, playerURL, title, startBy, org string) (string, Content) {
	return T(lang, "%s is ready to play", title), Content{
		Lang:      lang,
		Preheader: T(lang, "Print the cards, hide them and let the teams join."),
		Heading:   T(lang, "%s is ready to play", title),
		Body: T(lang, "Open the admin page to print the cards and the answer key, and to show the players their link. Start the first game by %s; from then you can play as many games as you like for 14 days.", startBy) +
			"\n\n" + T(lang, "Players join here: %s", playerURL) +
			"\n\n" + T(lang, "Anyone with the admin link can run and change your party adventure, so keep it to yourself. Your receipt comes in a separate email."),
		Buttons: []Button{{T(lang, "Open the admin page"), hostURL}},
		Footer:  T(lang, "The party adventure and everything about the players is deleted 14 days after the first game.") + " " + playerFooter(lang, "", org),
	}
}

// TrailKeep offers, a week into play, to keep a quiz trail for good in
// an account of the buyer's own.
func TrailKeep(lang, keepURL, title, until string) (string, Content) {
	return T(lang, "Keep %s?", title), Content{
		Lang:      lang,
		Preheader: T(lang, "Create a free account and the party adventure is yours to keep."),
		Heading:   T(lang, "Keep %s?", title),
		Body: T(lang, "Your party adventure is deleted on %s. Create a free account and it moves there instead, cards and all, to play again whenever you like.", until) +
			"\n\n" + T(lang, "Moving it ends the games you have now; their summaries were already mailed to you."),
		Buttons: []Button{{T(lang, "Keep my party adventure"), keepURL}},
		Footer:  T(lang, "Not interested? Do nothing and it is deleted as planned."),
	}
}

// TrailEnded says a quiz trail is gone: deleted when its days were over,
// or moved into the buyer's account (kept).
func TrailEnded(lang, title string, kept bool, org string) (string, Content) {
	if kept {
		return T(lang, "%s is in your account", title), Content{
			Lang:    lang,
			Heading: T(lang, "%s is in your account", title),
			Body:    T(lang, "Your party adventure is now an adventure in your account. Edit it, print it and host it from there. The summaries of the games you played were mailed to you before."),
			Footer:  playerFooter(lang, "", org),
		}
	}
	return T(lang, "%s has ended", title), Content{
		Lang:    lang,
		Heading: T(lang, "%s has ended", title),
		Body:    T(lang, "Your party adventure's days are over, so it and everything about its players has been deleted. The summaries of the games you played were mailed to you before.") + "\n\n" + T(lang, "Thank you for playing!"),
		Footer:  playerFooter(lang, "", org),
	}
}

// RoundTeam is one team's line in a game summary.
type RoundTeam struct {
	Name     string
	Players  int
	Score    float64
	Finished bool
	Minutes  int
}

// RoundCard is one question's line in a game summary: how many of the
// teams that reached it got it right.
type RoundCard struct {
	Title   string
	Reached int
	Right   int
}

// RoundSummary is a hosted event's summary of one finished game, mailed
// to the host: who played, the ranking, and how each question went.
func RoundSummary(lang, hostURL, party, org string, round int, teams []RoundTeam, cards []RoundCard) (string, Content) {
	players := 0
	for _, t := range teams {
		players += t.Players
	}
	var b strings.Builder
	b.WriteString(T(lang, "%[1]d teams, %[2]d players.", len(teams), players))
	if len(teams) > 0 {
		b.WriteString("\n\n**" + T(lang, "Ranking") + "**\n\n")
		for i, t := range teams {
			line := T(lang, "%[1]d. %[2]s — %[3]s points", i+1, mdEscape(t.Name), formatPoints(t.Score))
			if t.Finished {
				line += ", " + T(lang, "%d min", t.Minutes)
			} else {
				line += ", " + T(lang, "did not finish")
			}
			b.WriteString(line + "\n")
		}
	}
	if len(cards) > 0 {
		b.WriteString("\n**" + T(lang, "Questions") + "**\n\n")
		for _, c := range cards {
			pct := 0
			if c.Reached > 0 {
				pct = c.Right * 100 / c.Reached
			}
			b.WriteString(T(lang, "- %[1]s: %[2]d%% right (%[3]d of %[4]d teams)", mdEscape(c.Title), pct, c.Right, c.Reached) + "\n")
		}
	}
	return T(lang, "%[1]s — results of game %[2]d", party, round), Content{
		Lang:      lang,
		Preheader: T(lang, "Who won, and how the questions went."),
		Heading:   T(lang, "Game %d is over", round),
		Body:      b.String(),
		Buttons:   []Button{{T(lang, "Open the host page"), hostURL}},
		Footer:    T(lang, "You get this because you host %s.", party) + " " + playerFooter(lang, "", org),
	}
}

// formatPoints shows a score without a needless fraction.
func formatPoints(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// BoxConfirmed is a box buyer's team link, once their code checks out.
func BoxConfirmed(lang, partner, teamURL, playURL, voucher, org string) (string, Content) {
	_, c := TeamLink(lang, teamURL, playURL, voucher, org)
	c.Heading = T(lang, "Your %s box is confirmed!", partner)
	c.Footer = T(lang, "The link stays valid for a year. Lost it? Use “I lost my link” on the play page with this email address.")
	return T(lang, "Your %s adventure — team link inside", sitename.Name()), c
}

// BoxRejected tells a buyer their box's code did not check out.
func BoxRejected(lang, partner string) (string, Content) {
	subject := T(lang, "About your %s box", partner)
	return subject, Content{
		Lang:    lang,
		Heading: subject,
		Body:    T(lang, "We couldn't confirm the code from your %[1]s box with %[1]s.", partner) + "\n\n" + T(lang, "Please check the code and redeem it again, or reply to this email and we'll sort it out."),
	}
}

// BoxCancelled tells a buyer their redemption was withdrawn.
func BoxCancelled(lang, partner string) (string, Content) {
	subject := T(lang, "Your %s box redemption is cancelled", partner)
	return subject, Content{
		Lang:    lang,
		Heading: subject,
		Body:    T(lang, "Your redemption of a %s box has been cancelled. Reply to this email if that's a surprise.", partner),
	}
}

// BoxWaiting tells an organisation's admins a code waits to be checked.
func BoxWaiting(lang, link string) (string, Content) {
	subject := T(lang, "A box redemption is waiting for review")
	c := Content{Lang: lang, Heading: subject, Body: T(lang, "Someone has redeemed a partner box. Check the code in the partner's portal, then approve or reject it.")}
	if link != "" {
		c.Buttons = []Button{{T(lang, "Review the redemption"), link}}
	}
	return subject, c
}

// ListingWaiting tells the platform's admins an organisation's listing
// waits for review before it goes public.
func ListingWaiting(lang, org, link string) (string, Content) {
	subject := T(lang, "A listing is waiting for review")
	c := Content{Lang: lang, Heading: subject, Body: T(lang, "%s has put something up for sale. It goes public once it is approved.", mdEscape(org))}
	if link != "" {
		c.Buttons = []Button{{T(lang, "Review listings"), link}}
	}
	return subject, c
}

// ListingApproved tells an organisation's admins their listing is public.
func ListingApproved(lang, what, link string) (string, Content) {
	subject := T(lang, "“%s” is approved", what)
	c := Content{Lang: lang, Heading: subject, Body: T(lang, "Your listing for “%s” is approved and now public.", mdEscape(what))}
	if link != "" {
		c.Buttons = []Button{{T(lang, "Open it"), link}}
	}
	return subject, c
}

// ListingRejected tells an organisation's admins their listing was
// turned down, and why. What was public before stays up.
func ListingRejected(lang, what, reason, link string) (string, Content) {
	subject := T(lang, "“%s” was not approved", what)
	c := Content{
		Lang:    lang,
		Heading: subject,
		Body:    T(lang, "Your changes to the listing for “%s” were not approved. Anything already public stays as it was. Change the listing and save it to send it for review again.", mdEscape(what)),
		Quote:   reason,
	}
	if link != "" {
		c.Buttons = []Button{{T(lang, "Open it"), link}}
	}
	return subject, c
}

// PlayLinks is every link an address has, sent when it asks.
func PlayLinks(lang string, links []PlayerLink, org string) (string, Content) {
	var b strings.Builder
	b.WriteString(T(lang, "Here are fresh links for the teams this address is registered with. Each works on one device: opening it claims it for that device. Your old links no longer work."))
	b.WriteString("\n\n")
	for _, l := range links {
		if l.Event != "" {
			b.WriteString(T(lang, "- **%[1]s** in %[2]s, team %[3]s: [%[4]s](%[4]s)\n", mdEscape(l.Player), mdEscape(l.Event), mdEscape(l.Team), l.URL))
		} else {
			b.WriteString(T(lang, "- **%[1]s**, team %[2]s: [%[3]s](%[3]s)\n", mdEscape(l.Player), mdEscape(l.Team), l.URL))
		}
	}
	return T(lang, "Your %s play links", sitename.Name()), Content{
		Lang:      lang,
		Preheader: T(lang, "Your way back into the game."),
		Heading:   T(lang, "Your play links"),
		Body:      b.String(),
		Footer:    T(lang, "You get this because someone asked for the links of this address. If it wasn't you, ignore it."),
	}
}

// GameEmail is what an email step sends a team as it passes. Its words
// are the author's, in the adventure's language; only the footer is ours.
func GameEmail(lang, subject, body, event, org string) (string, Content) {
	return subject, Content{
		Lang:      lang,
		Preheader: firstLine(body),
		Body:      Lines(body),
		Footer:    playerFooter(lang, event, org),
	}
}

// TeamMessage is what an arranger writes to the teams of an event.
func TeamMessage(lang, subject, body, event, org string) (string, Content) {
	return subject, Content{
		Lang:      lang,
		Preheader: firstLine(body),
		Heading:   subject,
		Body:      Lines(body),
		Footer:    playerFooter(lang, event, org),
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 110 {
		return string(r[:110]) + "…"
	}
	return s
}

// --- designers -----------------------------------------------------------------

// Work is where a designer notification points.
type Work struct {
	Adventure, Step, Link, SettingsLink string
}

func (w Work) footer(lang string) string {
	f := T(lang, "You get this because you work on %s.", w.Adventure)
	if w.SettingsLink != "" {
		f += T(lang, " Choose which emails you get in your account settings: %s", w.SettingsLink)
	}
	return f
}

func (w Work) on(lang string) string {
	if w.Step == "" {
		return w.Adventure
	}
	return T(lang, "“%[1]s” in %[2]s", w.Step, w.Adventure)
}

// Assistant is how an email names someone's assistant.
func Assistant(lang, who string) string { return T(lang, "%s's assistant", who) }

func Mention(lang, who string, w Work, comment string) (string, Content) {
	return T(lang, "%[1]s mentioned you in %[2]s", who, w.Adventure), Content{
		Lang:      lang,
		Preheader: firstLine(comment),
		Heading:   T(lang, "%s mentioned you", who),
		Body:      T(lang, "On %s:", w.on(lang)),
		Quote:     comment,
		Buttons:   []Button{{T(lang, "Open the conversation"), w.Link}},
		Footer:    w.footer(lang),
	}
}

func StepAssigned(lang, who string, w Work) (string, Content) {
	return T(lang, "%[1]s gave you “%[2]s”", who, w.Step), Content{
		Lang:      lang,
		Preheader: T(lang, "You now own a step in %s.", w.Adventure),
		Heading:   T(lang, "A step is yours"),
		Body:      T(lang, "%[1]s made you the owner of %[2]s. Its status, and the conversation about it, are in the step's inspector.", who, w.on(lang)),
		Buttons:   []Button{{T(lang, "Open the step"), w.Link}},
		Footer:    w.footer(lang),
	}
}

func SuggestionMade(lang, who string, w Work, note string) (string, Content) {
	return T(lang, "%[1]s suggested a change to “%[2]s”", who, w.Step), Content{
		Lang:      lang,
		Preheader: T(lang, "Accept or decline it in the designer."),
		Heading:   T(lang, "A suggested change"),
		Body:      T(lang, "%[1]s suggested a change to %[2]s, a step you own. You can see it field by field, then accept or decline it.", who, w.on(lang)),
		Quote:     note,
		Buttons:   []Button{{T(lang, "Review the suggestion"), w.Link}},
		Footer:    w.footer(lang),
	}
}

func SuggestionDecided(lang, who string, accepted bool, w Work) (string, Content) {
	subject, pre, heading, body := "%[1]s declined your suggestion on “%[2]s”", "Your suggestion was declined.", "Suggestion declined", "%[1]s declined the change you suggested to %[2]s."
	if accepted {
		subject, pre, heading, body = "%[1]s accepted your suggestion on “%[2]s”", "Your suggestion was accepted.", "Suggestion accepted", "%[1]s accepted the change you suggested to %[2]s."
	}
	return T(lang, subject, who, w.Step), Content{
		Lang:      lang,
		Preheader: T(lang, pre),
		Heading:   T(lang, heading),
		Body:      T(lang, body, who, w.on(lang)),
		Buttons:   []Button{{T(lang, "Open the step"), w.Link}},
		Footer:    w.footer(lang),
	}
}

func ReviewRequested(lang, who string, w Work) (string, Content) {
	return T(lang, "“%s” is ready for review", w.Step), Content{
		Lang:      lang,
		Preheader: T(lang, "%s asks for a look.", who),
		Heading:   T(lang, "Ready for review"),
		Body:      T(lang, "%[1]s marked %[2]s as needing review. It's yours, so have a look and set it to ready — or comment on what to change.", who, w.on(lang)),
		Buttons:   []Button{{T(lang, "Review the step"), w.Link}},
		Footer:    w.footer(lang),
	}
}

// --- previews ------------------------------------------------------------------

// Kind is an email for the preview: its name and sample content.
type Kind struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Audience string `json:"audience"`
	// Sample builds it with made-up names, in the given brand's name.
	Sample func(lang, org string) (string, Content) `json:"-"`
}

func sampleWork(lang string) Work {
	return Work{Adventure: T(lang, "Harbour Mystery"), Step: T(lang, "The keeper's logbook"), Link: "https://example.org/app/arrange/", SettingsLink: "https://example.org/app/arrange/#/settings/account"}
}

// Kinds are every email, for the preview, players' first.
var Kinds = []Kind{
	{"team_invitation", "Team invitation", "players", func(lang, org string) (string, Content) {
		return TeamInvitation(lang, T(lang, "The Seagulls"), T(lang, "Harbour Mystery"), org, []PlayerLink{
			{Player: "Anna", URL: "https://example.org/play?t=a1b2c3"},
			{Player: "Ben", URL: "https://example.org/play?t=d4e5f6"},
		}, true)
	}},
	{"team_message", "Message to teams", "players", func(lang, org string) (string, Content) {
		body := strings.ReplaceAll(T(lang, "Hi {{team}}!\n\nWe start from the old lighthouse — look for the **red flag**.\nBring a charged phone and good shoes.\n\nSee you there!"), "{{team}}", T(lang, "The Seagulls"))
		return TeamMessage(lang, T(lang, "Meet at the lighthouse at 14:00"), body, T(lang, "Harbour Mystery"), org)
	}},
	{"game_email", "Email during the game", "players", func(lang, org string) (string, Content) {
		return GameEmail(lang, T(lang, "A letter has arrived"), T(lang, "Dear detectives,\n\nThe keeper's last entry mentions a tide table. Find it before the water rises.\n\n— A friend"), T(lang, "Harbour Mystery"), org)
	}},
	{"team_link", "Ticket: team link", "players", func(lang, org string) (string, Content) {
		return TeamLink(lang, "https://example.org/team/x7y8z9", "https://example.org/play", "X7Y8Z9", org)
	}},
	{"play_links", "Lost links", "players", func(lang, org string) (string, Content) {
		return PlayLinks(lang, []PlayerLink{{Team: T(lang, "The Seagulls"), Player: "Anna", Event: T(lang, "Harbour Mystery"), URL: "https://example.org/play?t=a1b2c3"}}, org)
	}},
	{"mention", "Mentioned in a conversation", "designers", func(lang, _ string) (string, Content) {
		return Mention(lang, "anna", sampleWork(lang), T(lang, "@ben could you make the second hint a bit less obvious?"))
	}},
	{"step_assigned", "Given a step", "designers", func(lang, _ string) (string, Content) {
		return StepAssigned(lang, "anna", sampleWork(lang))
	}},
	{"suggestion", "Suggestion on your step", "designers", func(lang, _ string) (string, Content) {
		return SuggestionMade(lang, "anna", sampleWork(lang), T(lang, "A clearer title, so teams know where to look."))
	}},
	{"review_requested", "Review requested", "designers", func(lang, _ string) (string, Content) {
		return ReviewRequested(lang, "anna", sampleWork(lang))
	}},
}

// KindByID finds a preview kind.
func KindByID(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}
