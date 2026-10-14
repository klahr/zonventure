package emailtpl

// --- an organisation's own domain (admin/domains.go) ------------------------

// DomainRecord is the one DNS record an organisation adds.
type DomainRecord struct {
	Host  string // adventures.theirbrand.se
	Name  string // adventures
	Value string // theirbrand.cname.zonventure.com
}

// recordLines is the record as three labelled lines: a table renders
// unevenly across mail clients.
func recordLines(lang string, r DomainRecord) string {
	return "**" + T(lang, "Type") + ":** CNAME  \n**" + T(lang, "Name") + ":** `" + r.Name + "`  \n**" + T(lang, "Value") + ":** `" + r.Value + "`"
}

// DomainReady tells an organisation's admins they may set up their domain.
func DomainReady(lang, org, link string) (string, Content) {
	return T(lang, "Your own domain is ready to set up"), Content{
		Lang:      lang,
		Preheader: T(lang, "A few minutes and one DNS record, and %s has its own address.", org),
		Heading:   T(lang, "Your own domain is ready"),
		Body: T(lang, "%s can now have its own address, like **adventures.yourbrand.com**: your store, tickets and games on it, in your colours, under your name.", mdEscape(org)) +
			"\n\n" + T(lang, "Setting it up takes a few minutes: choose the address, add one record at your domain provider and we do the rest. The page walks you through it."),
		Buttons: []Button{{T(lang, "Set up my domain"), link}},
	}
}

// DomainInstructions is what an admin sends whoever runs the domain.
func DomainInstructions(lang, org, from string, r DomainRecord, steps, link string) (string, Content) {
	body := T(lang, "%[1]s asked you to point **%[2]s** at the adventures of %[3]s. It takes one DNS record:", mdEscape(from), r.Host, mdEscape(org)) +
		"\n\n" + recordLines(lang, r)
	if steps != "" {
		body += "\n\n" + steps
	}
	body += "\n\n" + T(lang, "Remove any other record with the name `%s` first. If the domain is on Cloudflare, leave the proxy off (grey cloud).", r.Name)
	return T(lang, "One DNS record for %s", r.Host), Content{
		Lang:      lang,
		Preheader: T(lang, "CNAME %[1]s → %[2]s", r.Name, r.Value),
		Heading:   T(lang, "Point %s at its adventures", r.Host),
		Body:      body,
		Buttons:   []Button{{T(lang, "Check whether it works"), link}},
		Footer:    T(lang, "Changes usually show within minutes, at most a few hours. The button above opens a page that says the moment it works; no account needed."),
	}
}

// DomainLive tells an organisation's admins their domain answers.
func DomainLive(lang, host, origin, link string) (string, Content) {
	return T(lang, "%s is live", host), Content{
		Lang:      lang,
		Preheader: T(lang, "Your store, tickets and games are on your own address now."),
		Heading:   T(lang, "%s is live", host),
		Body: T(lang, "Your store is at **%s** now, with its own security certificate.", origin) + "\n\n" +
			T(lang, "Every ticket, invitation and printed letter from now on links there. Links you sent before still work."),
		Buttons: []Button{{T(lang, "Open my store"), origin}, {T(lang, "Domain settings"), link}},
	}
}

// DomainDown tells them a live domain stopped pointing here.
func DomainDown(lang, host, value, link string) (string, Content) {
	return T(lang, "%s isn't reaching your store", host), Content{
		Lang:      lang,
		Preheader: T(lang, "Its DNS record has changed or gone."),
		Heading:   T(lang, "%s isn't reaching your store", host),
		Body: T(lang, "Since a while ago, **%[1]s** no longer points at `%[2]s`. Players following your links may not reach your store.", host, value) + "\n\n" +
			T(lang, "Check the record at your domain provider. The domain page shows what we see and how to fix it."),
		Buttons: []Button{{T(lang, "See what's wrong"), link}},
	}
}

// DomainEnding tells them the entitlement ended and the grace began.
func DomainEnding(lang, host, until, link string) (string, Content) {
	return T(lang, "Your own domain ends on %s", until), Content{
		Lang:      lang,
		Preheader: T(lang, "Renew to keep %s.", host),
		Heading:   T(lang, "Your own domain ends on %s", until),
		Body: T(lang, "Your subscription for **%[1]s** has ended. It keeps working until **%[2]s**; after that every link to it is forwarded to your store on our address, so tickets already sent keep working.", host, until) + "\n\n" +
			T(lang, "To keep it, renew from the domain page before then. If you won't, remove its DNS record after that date."),
		Buttons: []Button{{T(lang, "Domain settings"), link}},
	}
}

// OrgChangeRolledBack tells an organisation's admins a change to their
// public page was put back, and why. field is name, tagline,
// public_website, logo or cover.
func OrgChangeRolledBack(lang, field, reason, link string) (string, Content) {
	what := map[string]string{
		"name": T(lang, "your organisation's name"), "tagline": T(lang, "your tagline"), "public_website": T(lang, "your website"),
		"logo": T(lang, "your logo"), "cover": T(lang, "your cover picture"),
	}[field]
	return T(lang, "A change to your page was put back"), Content{
		Lang:      lang,
		Preheader: T(lang, "The previous version of %s is showing again.", what),
		Heading:   T(lang, "A change to your page was put back"),
		Body:      T(lang, "We reviewed the recent change to %s and put the previous version back.", what),
		Quote:     reason,
		Buttons:   []Button{{T(lang, "Change it again"), link}},
		Footer:    T(lang, "Questions? Reply to this email."),
	}
}
