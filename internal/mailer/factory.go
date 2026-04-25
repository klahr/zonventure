package mailer

import (
	"errors"
	"os"
	"strings"
)

// FromEnv builds a Mailer from $ZV_SMTP_* environment variables.
// Returns LogMailer when ZV_SMTP_HOST is unset (dev fallback), and
// SMTPMailer otherwise. Validation of required-when-host-is-set fields
// happens here so the operator gets a startup failure instead of a
// silent first-email crash.
//
// Env keys:
//
//	ZV_SMTP_HOST   smtp.example.com  (empty = LogMailer)
//	ZV_SMTP_PORT   587               (default 587)
//	ZV_SMTP_USER   apikey            (optional — sends without auth when empty)
//	ZV_SMTP_PASS   …                 (optional — only if USER set)
//	ZV_SMTP_FROM   "From <addr@…>"   (required when HOST set)
//	ZV_SMTP_TLS    starttls|tls|none (default starttls)
func FromEnv() (Mailer, error) {
	host := strings.TrimSpace(os.Getenv("ZV_SMTP_HOST"))
	if host == "" {
		return LogMailer{}, nil
	}
	from := strings.TrimSpace(os.Getenv("ZV_SMTP_FROM"))
	if from == "" {
		return nil, errors.New("ZV_SMTP_HOST is set but ZV_SMTP_FROM is empty")
	}
	mode := TLSMode(strings.ToLower(strings.TrimSpace(os.Getenv("ZV_SMTP_TLS"))))
	switch mode {
	case "", TLSStartTLS, TLSImplicit, TLSNone:
		// ok
	default:
		return nil, errors.New(`ZV_SMTP_TLS must be one of "starttls", "tls", "none"`)
	}
	port := strings.TrimSpace(os.Getenv("ZV_SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	return &SMTPMailer{
		Host: host,
		Port: port,
		User: os.Getenv("ZV_SMTP_USER"),
		Pass: os.Getenv("ZV_SMTP_PASS"),
		From: from,
		TLS:  mode,
	}, nil
}
