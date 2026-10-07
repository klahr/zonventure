package settings

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// Legal is who runs the platform, how to reach them and how long data
// is kept: the blanks of /privacy and /terms, the seller line by every
// buy button, and the retention sweepers' periods. Edited by the
// platform admin under Settings → Legal. The same in every language.
type Legal struct {
	CompanyName      string `json:"company_name"`
	CompanyOrgNumber string `json:"company_org_number"`
	CompanyAddress   string `json:"company_address"`
	CompanyVATNumber string `json:"company_vat_number"`
	ContactEmail     string `json:"contact_email"`
	// DPOEmail falls back to ContactEmail where empty.
	DPOEmail        string `json:"dpo_email"`
	Jurisdiction    string `json:"jurisdiction"`
	HostingProvider string `json:"hosting_provider"`
	EmailProvider   string `json:"email_provider"`
	// Days before player locations, login history and a finished party
	// are deleted; 0 keeps them.
	LocationRetentionDays int `json:"location_retention_days"`
	AuditRetentionDays    int `json:"audit_retention_days"`
	PartyRetentionDays    int `json:"party_retention_days"`
	// Reviewed drops the pages' "draft template" notes.
	Reviewed bool `json:"reviewed"`
	// LastUpdated is the date the pages show, YYYY-MM-DD; empty is today.
	LastUpdated string `json:"last_updated"`
}

// DefaultLegal applies to any value never saved.
var DefaultLegal = Legal{LocationRetentionDays: 30, AuditRetentionDays: 90, PartyRetentionDays: 14}

// MaxLegalText is the longest a legal text value may be.
const MaxLegalText = 300

type legalField struct {
	key, env string
	text     *string
	days     *int
	flag     *bool
}

func (l *Legal) fields() []legalField {
	return []legalField{
		{key: "legal_company_name", env: "ZV_COMPANY_NAME", text: &l.CompanyName},
		{key: "legal_company_org_number", env: "ZV_COMPANY_ORG_NUMBER", text: &l.CompanyOrgNumber},
		{key: "legal_company_address", env: "ZV_COMPANY_ADDRESS", text: &l.CompanyAddress},
		{key: "legal_company_vat_number", env: "ZV_COMPANY_VAT_NUMBER", text: &l.CompanyVATNumber},
		{key: "legal_contact_email", env: "ZV_CONTACT_EMAIL", text: &l.ContactEmail},
		{key: "legal_dpo_email", env: "ZV_DPO_EMAIL", text: &l.DPOEmail},
		{key: "legal_jurisdiction", env: "ZV_JURISDICTION", text: &l.Jurisdiction},
		{key: "legal_hosting_provider", env: "ZV_HOSTING_PROVIDER", text: &l.HostingProvider},
		{key: "legal_email_provider", env: "ZV_EMAIL_PROVIDER", text: &l.EmailProvider},
		{key: "legal_location_retention_days", env: "ZV_LOCATION_RETENTION_DAYS", days: &l.LocationRetentionDays},
		{key: "legal_audit_retention_days", env: "ZV_AUDIT_RETENTION_DAYS", days: &l.AuditRetentionDays},
		{key: "legal_party_retention_days", env: "ZV_PARTY_RETENTION_DAYS", days: &l.PartyRetentionDays},
		{key: "legal_reviewed", env: "ZV_LEGAL_REVIEWED", flag: &l.Reviewed},
		{key: "legal_last_updated", env: "ZV_LEGAL_LAST_UPDATED", text: &l.LastUpdated},
	}
}

// LegalEnvVars are the env vars Settings → Legal replaced.
func LegalEnvVars() []string {
	var l Legal
	var out []string
	for _, f := range l.fields() {
		out = append(out, f.env)
	}
	return out
}

func (f legalField) set(v string) bool {
	switch {
	case f.text != nil:
		*f.text = strings.TrimSpace(v)
	case f.days != nil:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			return false
		}
		*f.days = n
	default:
		*f.flag = parseBool(v, false)
	}
	return true
}

func (f legalField) value() string {
	switch {
	case f.text != nil:
		return *f.text
	case f.days != nil:
		return strconv.Itoa(*f.days)
	}
	if *f.flag {
		return "1"
	}
	return "0"
}

var legal atomic.Pointer[Legal]

// CurrentLegal returns the legal values in force: the saved ones, else the defaults.
func CurrentLegal() Legal {
	if l := legal.Load(); l != nil {
		return *l
	}
	return DefaultLegal
}

// LoadLegal seeds the in-memory values from the settings table. Once,
// on the first start after the move from env vars, getenv's value of a
// var whose key was never saved is copied in; imported names the vars
// copied, for the caller to log. Tolerates a nil db.
func LoadLegal(ctx context.Context, db *sql.DB, getenv func(string) string) (imported []string) {
	l := DefaultLegal
	if db != nil {
		for _, f := range l.fields() {
			v, err := getString(ctx, db, f.key)
			if errors.Is(err, sql.ErrNoRows) && getenv != nil {
				if env := strings.TrimSpace(getenv(f.env)); env != "" && f.set(env) {
					if setString(ctx, db, f.key, f.value()) == nil {
						imported = append(imported, f.env)
					}
				}
				continue
			}
			if err == nil {
				f.set(v)
			}
		}
	}
	legal.Store(&l)
	return imported
}

// Validate checks the emails look like emails, the days are 0–3650,
// the date is a date and no text is too long. Required values may be
// empty: the pages then stay unpublished.
func (l Legal) Validate() error {
	for _, f := range l.fields() {
		switch {
		case f.text != nil && utf8.RuneCountInString(*f.text) > MaxLegalText:
			return errors.New(f.key + " is too long")
		case f.days != nil && (*f.days < 0 || *f.days > 3650):
			return errors.New("retention days must be 0–3650")
		}
	}
	for _, e := range []string{l.ContactEmail, l.DPOEmail} {
		if e == "" {
			continue
		}
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			return errors.New(e + " is not an email address")
		}
	}
	if l.LastUpdated != "" && !isDate(l.LastUpdated) {
		return errors.New("last updated must be a date, YYYY-MM-DD")
	}
	return nil
}

func isDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i != 4 && i != 7 && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// SetLegal saves every value in one transaction and puts them in force.
func SetLegal(ctx context.Context, db *sql.DB, l Legal) error {
	for _, f := range l.fields() {
		if f.text != nil {
			*f.text = strings.TrimSpace(*f.text)
		}
	}
	if err := l.Validate(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, f := range l.fields() {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at)
			VALUES ($1, $2, now_text())
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, f.key, f.value()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	legal.Store(&l)
	return nil
}
