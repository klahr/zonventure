# Security overview

> **DRAFT — verify against current production reality before sharing externally.**
> This document is intended to answer the security questions a corporate buyer's IT-security team will ask before signing. Keep it concrete, accurate, and current.

**Version:** 1.0 — DRAFT
**Last updated:** [DATE]
**Owner:** [SECURITY CONTACT NAME], [SECURITY EMAIL]

---

## At a glance

| Topic | Posture |
|---|---|
| Vendor | Zonventure AB (Swedish limited company, organisation number [ZONVENTURE ORG NUMBER]) |
| Hosting region (Hosted Service) | [HOSTING REGION], [HOSTING PROVIDER NAME] |
| Self-host option | Yes — full feature parity, customer-controlled infrastructure |
| Encryption in transit | TLS 1.2+ enforced; HSTS on all customer-facing endpoints |
| Encryption at rest | Provider-managed disk encryption for the production database and backups |
| Authentication | Email + password (Argon2id-class hashing); TOTP second factor; magic-link join for Players |
| SSO | Roadmap — Google Workspace OIDC first, Microsoft Entra OIDC next, SAML on customer request |
| Data residency | EU (default) — see "Sub-processors" |
| GDPR data processor | Yes (when Hosted) — see `legal/DPA.md` |
| Audit cooperation | Annual remote audit, written questionnaires, on-demand documentation |
| Incident notification | Within 72 hours of confirmed breach |
| Backups | Daily automated; off-site; periodic restore tests |
| Pen-testing | [Internal review annually / external pen-test cadence to confirm] |
| Security questionnaire | Pre-completed CAIQ-Lite available on request |

---

## 1. Architecture summary

Zonventure is a single-tenant or multi-tenant Go application running over PostgreSQL. The same artifact powers both the Hosted Service operated by Zonventure and the Self-Hosted Service installed by customers on their own infrastructure.

The application has three principal trust boundaries:

1. **Public web tier** — serves the player-facing `/play` interface, the arranger-facing `/arrange` console, and the public landing/marketing pages. Authenticated arranger sessions are cookie-based; player sessions are bound to a slot token issued by the arranger.
2. **Application tier** — handles authentication, authorisation, event state, WebSocket fan-out, and integration with sub-processors (email, optionally payment).
3. **Data tier** — PostgreSQL stores all persistent state (accounts, adventures, events, runs, location pings, audit logs). No customer data is stored on application-tier disks except in transient logs.

Static assets are served from the application binary (Go `embed.FS`) — there is no separate CDN dependency for code-path assets. Customer-uploaded media (puzzle images, audio) is stored as Postgres blobs, served through application-mediated routes that enforce authorisation.

## 2. Data handling

### 2.1 What we store

See `legal/DPA.md` Annex 1 for the controller-side description. In summary:

- **Arranger accounts:** email, organisation name, hashed password, IP and user-agent at login.
- **Player Data during an event:** slot name, GPS location pings while consent is active, answer text, puzzle progression timestamps.
- **Operational logs:** login outcomes, audit-log entries, request metadata.
- **Tokens:** email verification and password reset tokens (one-shot, ≤1 hour TTL, deleted on use).

### 2.2 What we do not store

- **No analytics or advertising trackers** in any first-party page.
- **No payment-card data** — payments (when configured) flow through Stripe; Zonventure receives only non-sensitive event metadata.
- **No location data from arrangers** — only from players who opt into geolocation when prompted by the play page.
- **No special categories of personal data** under Article 9 GDPR. Customer agrees not to submit such data.

### 2.3 Encryption

- **In transit:** TLS 1.2+ enforced on all customer-facing endpoints. HSTS preloaded. WebSocket connections use `wss://`.
- **At rest:** the production database and backups use provider-managed disk encryption (AES-256). Application-level encryption for selected fields (passwords via Argon2id; verification/reset tokens stored hashed where applicable).
- **Secrets management:** all production secrets are environment-injected; no secrets in source control. See [Operational security](#5-operational-security).

### 2.4 Retention

| Data | Default retention | Configurable |
|---|---|---|
| Arranger account data | Until deletion (7-day grace window) | Yes (by Customer) |
| Player location pings | 30 days post-event | Yes — `ZV_LOCATION_RETENTION_DAYS` |
| Audit logs | 90 days | Yes — `ZV_AUDIT_RETENTION_DAYS` |
| Email verification / reset tokens | 1 hour or single use | Hard-coded |
| Backups | 30 days rolling | Yes (Hosted Service operations) |

### 2.5 Deletion

- Arrangers can delete their account from `Settings → Delete account` (7-day grace window).
- Players can wipe their per-event data via the `Forget me` menu inside `/play`.
- Customers can request bulk deletion via [SECURITY EMAIL]; standard turnaround is five (5) business days.
- On termination of a hosted contract, deletion follows `legal/DPA.md` § 5.7.

## 3. Authentication and access control

### 3.1 End-user authentication

- **Arrangers:** email + password (Argon2id-class hashing). Optional TOTP second factor — required for administrative roles in customer organisations on roadmap.
- **Players:** magic-link join, single-use, slot-token-bound. Magic links are single-use after migration `0027_slot_token_single_use`.

### 3.2 Internal access control

- Role-based access in the production environment.
- Multi-factor authentication required for administrative access to production systems.
- Access provisioned and revoked through documented onboarding/offboarding procedures.
- Principle of least privilege: support staff do not have direct database access to customer data; debugging happens through audited tooling.

### 3.3 Audit logging

The application records security-relevant events (login successes/failures, password changes, role changes, administrative actions on events) to an audit log retained per Section 2.4.

## 4. Sub-processors and data residency

The current sub-processor list is maintained at `legal/SUBPROCESSORS.md` and is referenced by `legal/DPA.md` Annex 3. Categories include hosting, email delivery, operational tooling (error monitoring, logging), and payment processing (when configured).

By default, customer data for the Hosted Service is processed in the [HOSTING REGION] region. Cross-region transfers are governed by the SCCs as set out in the DPA.

Customers are notified at least thirty (30) days before any new sub-processor is added or replaced.

## 5. Operational security

### 5.1 Network and platform

- Application runs on hardened container images. Distroless / static-debian12 base for the Go binary.
- HTTP responses include a strict baseline Content Security Policy, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, and `X-Content-Type-Options: nosniff`.
- Frame embedding is denied (`frame-ancestors 'none'`).

### 5.2 Application security

- Security-relevant code paths (authentication, authorisation, payment, data deletion, file upload, geolocation) are subject to additional review on changes.
- Image uploads are always re-encoded by `internal/imageutil.Shrink` to defeat polyglot/MIME-confusion uploads.
- CSV import/export defangs spreadsheet formula injection (`internal/teamimport.SanitizeCellValue`).
- Outbound webhooks validate destination hosts against an SSRF blocklist (`internal/webhooks/ssrf.go: ValidateHost`).
- Server-side rate limiting on authentication, hint reveals, and answer submission.

### 5.3 Dependency hygiene

- Dependencies are pinned in `go.mod` / `go.sum`.
- Vulnerability advisories for Go modules and base images are monitored; security patches are prioritised based on exploitability and exposure.

### 5.4 Backups and disaster recovery

- Daily automated database backups, encrypted, stored in a separate region from production.
- Backups are retained for 30 days rolling.
- Restore procedures are documented and tested at least quarterly.
- Recovery Time Objective (RTO): [4 hours — confirm].
- Recovery Point Objective (RPO): [24 hours — confirm].

### 5.5 Logging and monitoring

- Application emits structured logs to a managed logging sub-processor.
- Operational dashboards monitor error rates, latency, and availability.
- Alerting on critical paths (authentication, payment if configured, data deletion).

## 6. Incident response

### 6.1 Definition

A **personal data breach** is a breach of security leading to the accidental or unlawful destruction, loss, alteration, unauthorised disclosure of, or access to, personal data transmitted, stored, or otherwise processed.

### 6.2 Process

1. **Detection** — incidents are detected via monitoring, support reports, or third-party disclosure.
2. **Triage** — the on-call engineer assesses scope and severity.
3. **Containment and recovery** — affected systems are isolated; recovery proceeds with documented runbooks.
4. **Notification** — confirmed personal data breaches are notified to affected customers within seventy-two (72) hours of confirmation, with the information required under Article 33(3) GDPR to the extent then known. Updates follow as the investigation continues.
5. **Post-incident review** — root-cause analysis and remediation are documented; preventive actions are tracked to closure.

### 6.3 Customer reporting channel

Suspected security issues should be reported to [SECURITY EMAIL]. Coordinated vulnerability disclosure is welcome and will be acknowledged within two (2) business days.

## 7. Compliance posture

### 7.1 GDPR

Zonventure operates as a data processor under the Hosted Service. The DPA at `legal/DPA.md` reflects Article 28 GDPR obligations. Sub-processor changes follow the notice procedure in DPA § 5.3.

### 7.2 ePrivacy and cookies

The Service does not set non-essential cookies. The only cookies issued by first-party domains are session/CSRF cookies strictly necessary to operate the platform.

### 7.3 Accessibility

The player and arranger interfaces are designed against WCAG 2.1 AA where applicable, including keyboard navigation, screen-reader-friendly markup, and configurable colour themes. [Confirm conformance level before declaring publicly.]

### 7.4 Standards roadmap

The following are on the roadmap and not currently certified:

- SOC 2 Type II — [planned / scoped — confirm before sharing].
- ISO/IEC 27001 — [under consideration].

Customers requiring these certifications should ask about current status before signing; we do not represent the Service as currently certified to these standards.

## 8. Self-host as a security option

For customers with strict data-residency or sovereignty requirements, Zonventure offers a Self-Hosted deployment option. Under self-host:

- All customer data resides on infrastructure controlled by the customer.
- Zonventure AB is **not** a data processor — the customer is the controller and processor of its own data.
- The DPA does not apply.
- Zonventure provides software updates, documentation (`OPERATIONS.md`, `SELFHOSTING.md`), and (under a support arrangement) operational guidance.
- The same security features (TLS, hashed passwords, audit logging, retention controls, image re-encoding, SSRF blocking) are present in the binary regardless of where it runs; the customer is responsible for the surrounding environment (TLS termination, firewalls, backups, OS patching).

## 9. Customer responsibilities

Security is a shared model. Customers are responsible for:

- Securing arranger credentials (strong passwords, TOTP enrolment for staff, prompt offboarding).
- Configuring data-retention windows appropriate to their use case.
- Vetting Adventure content before public events, including the safety and lawfulness of physical locations referenced by gates.
- Obtaining lawful bases and consents for processing player data under Data Protection Laws.
- For the Self-Hosted Service, all infrastructure-level security controls (TLS termination, network controls, backups, OS and dependency patching, monitoring).

## 10. Contact

Security questions, vulnerability reports, and audit requests:

- **Email:** [SECURITY EMAIL]
- **PGP key:** [optional — link or fingerprint]
- **Response SLA:** acknowledgement within 2 business days; substantive response within 10 business days.

---

## Changelog

- **1.0 — DRAFT** ([DATE]): initial document.
