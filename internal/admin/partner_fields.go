package admin

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// partnerField is one code a partner's boxes carry, as the buyer types
// it. A partner has one (stored as security_code) or two (box_number,
// then security_code). See migration 0166.
type partnerField struct {
	// Label is what the input is called, by language; "en" is the fallback.
	Label   map[string]string `json:"label"`
	Example string            `json:"example"`
	Min     int               `json:"min"`
	Max     int               `json:"max"`
	// Digits allows digits only; otherwise letters and digits.
	Digits bool `json:"digits"`
}

const (
	maxPartnerName   = 60
	maxPartnerLabel  = 60
	maxPartnerHint   = 300
	maxPartnerCode   = 64
	maxPartnerFields = 2
	maxPartnerText   = 200
	maxPartnerURL    = 500
)

// partnerLangs are the languages a partner's labels and hint may be in:
// those the redeem page is offered in.
var partnerLangs = []string{"en", "sv"}

// partnerSetup is what an organiser defines a partner by.
type partnerSetup struct {
	Name   string            `json:"name"`
	Fields []partnerField    `json:"fields"`
	Hint   map[string]string `json:"hint"`
	// Who to ask at the partner, and where its codes are checked; for
	// the organisation only, never on /redeem. All optional.
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	PortalURL    string `json:"portal_url"`
}

// clean trims the setup and checks it, returning why it is unusable.
func (s *partnerSetup) clean() error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || utf8.RuneCountInString(s.Name) > maxPartnerName {
		return fmt.Errorf("a partner needs a name of at most %d characters", maxPartnerName)
	}
	if len(s.Fields) < 1 || len(s.Fields) > maxPartnerFields {
		return fmt.Errorf("a partner has one or two codes")
	}
	for i := range s.Fields {
		f := &s.Fields[i]
		var err error
		if f.Label, err = cleanLangText(f.Label, maxPartnerLabel); err != nil {
			return err
		}
		if f.Label["en"] == "" {
			return fmt.Errorf("every code needs an English label")
		}
		f.Example = strings.TrimSpace(f.Example)
		if utf8.RuneCountInString(f.Example) > maxPartnerCode {
			return fmt.Errorf("an example is at most %d characters", maxPartnerCode)
		}
		if f.Min < 1 || f.Max < f.Min || f.Max > maxPartnerCode {
			return fmt.Errorf("a code's length is between 1 and %d characters, the least no more than the most", maxPartnerCode)
		}
	}
	s.ContactName = strings.TrimSpace(s.ContactName)
	if utf8.RuneCountInString(s.ContactName) > maxPartnerText {
		return fmt.Errorf("a contact's name is at most %d characters", maxPartnerText)
	}
	s.ContactEmail = strings.TrimSpace(s.ContactEmail)
	if s.ContactEmail != "" {
		if a, err := mail.ParseAddress(s.ContactEmail); err != nil || a.Address != s.ContactEmail || len(s.ContactEmail) > maxPartnerText {
			return fmt.Errorf("the contact email is not a valid address")
		}
	}
	s.PortalURL = strings.TrimSpace(s.PortalURL)
	if s.PortalURL != "" {
		u, err := url.Parse(s.PortalURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(s.PortalURL) > maxPartnerURL {
			return fmt.Errorf("the portal link must be a web address starting with https://")
		}
	}
	var err error
	s.Hint, err = cleanLangText(s.Hint, maxPartnerHint)
	return err
}

// cleanLangText keeps the known languages' trimmed, non-empty texts.
func cleanLangText(in map[string]string, max int) (map[string]string, error) {
	out := map[string]string{}
	for lang, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !slices.Contains(partnerLangs, lang) {
			return nil, fmt.Errorf("unknown language %q", lang)
		}
		if utf8.RuneCountInString(v) > max {
			return nil, fmt.Errorf("a text is at most %d characters", max)
		}
		out[lang] = v
	}
	return out, nil
}

// langText is m's text in lang, else its English one.
func langText(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	return m["en"]
}

// check normalises a typed code and reports whether it fits f.
func (f partnerField) check(s string) (string, bool) {
	s = normalizeCode(s)
	if n := len(s); n < f.Min || n > f.Max {
		return "", false
	}
	for _, r := range s {
		digit := r >= '0' && r <= '9'
		if !digit && (f.Digits || r < 'A' || r > 'Z') {
			return "", false
		}
	}
	return s, true
}

// checkPartnerCode normalises a box's codes and checks them against the
// partner's fields: with two, box is the first and code the second; with
// one, code is it and box stays empty.
func checkPartnerCode(fields []partnerField, box, code string) (string, string, bool) {
	switch len(fields) {
	case 1:
		if code, ok := fields[0].check(code); ok && normalizeCode(box) == "" {
			return "", code, true
		}
	case 2:
		box, okBox := fields[0].check(box)
		code, okCode := fields[1].check(code)
		if okBox && okCode {
			return box, code, true
		}
	}
	return "", "", false
}

// scanPartnerFields decodes partners.fields.
func scanPartnerFields(raw []byte) []partnerField {
	var fields []partnerField
	_ = json.Unmarshal(raw, &fields)
	return fields
}
