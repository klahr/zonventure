// Auth + OIDC + Stripe-webhook routes — every URL whose authentication
// flow starts before the user has a session: login/signup/forgot,
// the email-verify GET, OIDC, the public Stripe webhook, and the
// simulated-provider dev routes.
package server

import (
	"github.com/go-chi/chi/v5"

	"git.sr.ht/~klahr/zv/internal/auth"
)

// registerAuthRoutes wires the staff authentication endpoints. The
// CSRF group covers every mutating call so the user's zv_csrf
// cookie (seeded by GET /arrange) gets echoed back from the form.
// /api/auth/whoami stays open: read-only, no cookies needed.
func registerAuthRoutes(r chi.Router, env *routesEnv) {
	authH := env.authH
	playerH := env.playerH

	// Staff authentication. CSRF-protected — the user gets a zv_csrf
	// cookie from the /arrange page GET that hosts the login form, so
	// the form's POST can echo it back. A cross-origin "log my victim
	// into my account" attempt has no way to read the cookie value.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireCSRF())
		r.Post("/api/auth/login", authH.Login)
		// Second leg of login when the user has TOTP enabled. The
		// challenge token issued by /api/auth/login is the only way
		// in; the endpoint mints the session cookie on success.
		r.Post("/api/auth/login/totp", authH.LoginTOTP)
		r.Post("/api/auth/signup", authH.Signup)
		r.Post("/api/auth/set-password", authH.SetPassword)
		r.Post("/api/auth/logout", authH.Logout)
		r.Post("/api/auth/resend-verification", authH.ResendVerification)
		// Forgot/reset are pre-auth: a user who can't log in is
		// exactly the audience here. Both still need CSRF (the
		// /arrange page seeds the cookie before the form submits).
		r.Post("/api/auth/forgot", authH.ForgotPassword)
		r.Post("/api/auth/reset", authH.ResetPassword)
		// Lost-magic-link recovery: pre-auth player flow that
		// re-mints magic-link tokens for any team whose contact
		// email matches the supplied address. Same CSRF + always-
		// 204 anti-enumeration shape as /api/auth/forgot.
		r.Post("/api/player/recover-link", playerH.RecoverLink)
		// Switch the calling session's active org. Caller must already
		// be authenticated AND a member of the target org — handler
		// returns 403 if not. CSRF-protected because it mutates the
		// authoritative session-state row.
		r.Post("/api/auth/switch-org", authH.SwitchOrg)
	})
	r.Get("/api/auth/whoami", authH.Whoami)
	// Public signup-config: unauthenticated GET so the login screen
	// can decide whether to render a "Sign up" link before any user
	// has a session. CSRF irrelevant — read-only, no cookies needed.
	r.Get("/api/auth/signup-config", authH.SignupConfig)
	// Email verification clicks land on a GET; not CSRF-relevant.
	r.Get("/verify", authH.VerifyEmail)

	// Single-sign-on (OIDC) flow. Both endpoints are GET because the
	// IdP redirects back via top-level navigation — there's no JS
	// fetch in the loop. CSRF for /login is bound by the state cookie
	// pinning the redirect to the issuing browser; CSRF for /callback
	// is the same state check, validated against what came back on
	// the URL. Routes 404 cleanly when SSO is not configured.
	r.Get("/auth/oidc/login", authH.OIDCLogin)
	r.Get("/auth/oidc/callback", authH.OIDCCallback)
}

// registerPaymentWebhookRoutes wires the public Stripe webhook
// (signature-verified, no CSRF) plus the dev-only simulated-provider
// routes when the operator selected that backend, and the
// CSRF-protected confirm-email-change endpoint that doubles as a
// payment-adjacent confirmation flow.
func registerPaymentWebhookRoutes(r chi.Router, env *routesEnv) {
	adminH := env.adminH
	simulatedProv := env.simulatedProv

	// Stripe webhook. Public POST: the auth IS the HMAC signature in
	// the Stripe-Signature header, verified by the handler. Sits OUTSIDE
	// the CSRF middleware because Stripe doesn't (and shouldn't) know
	// our CSRF cookie. Idempotency is handled by processed_webhook_events.
	r.Post("/api/webhooks/stripe", adminH.PaymentsWebhook)
	// When the operator picked the simulated provider, mount the
	// dev-only checkout / confirm / cancel routes. Doing this from
	// here (rather than from the simulated package itself) keeps
	// the route surface visible in one place + means the rest of
	// the code stays unaware of the provider choice.
	if simulatedProv != nil {
		simulatedProv.MountDevRoutes(r)
	}
	// Change-email confirmation: public POST (the token IS the auth),
	// CSRF-protected because the page that calls it is /arrange and
	// already has the cookie. JSON body: {"token": "..."}.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireCSRF())
		r.Post("/api/auth/confirm-email-change", adminH.ConfirmEmailChange)
	})
}
