# Sub-processors

> **Living document.** Update whenever a sub-processor is added, removed, or replaced. Customers under an active DPA must be notified at least thirty (30) days before any addition or replacement (see `legal/DPA.md` § 5.3).

**Version:** 1.0 — DRAFT
**Last updated:** [DATE]

---

The following sub-processors are authorised under § 5.3 of the [DPA](DPA.md) for the Hosted Service. Only categories of personal data necessary to provide the named service are shared with each sub-processor.

| Sub-processor | Service provided | Personal data processed | Region |
|---|---|---|---|
| [HOSTING PROVIDER NAME] | Application and database hosting | All Customer Data processed by the Hosted Service | [HOSTING REGION] |
| [EMAIL PROVIDER NAME] | Transactional email delivery (magic links, verification, account notices) | Email addresses, message content limited to operational notices | [EMAIL PROVIDER REGION] |
| [ERROR MONITORING NAME, e.g., Sentry] | Application error and crash reporting | IP address, user-agent, stack traces, request metadata; **no Customer Data payloads** by configuration | [REGION] |
| [LOG MANAGEMENT NAME] | Operational log aggregation | Application logs (no Customer Data payloads by configuration) | [REGION] |
| [PAYMENT PROCESSOR NAME, e.g., Stripe] *(when paid features are enabled)* | Payment processing | Payer email, billing address, payment-method tokens (full card data not exposed to Zonventure) | [REGION] |

## Optional / opt-in sub-processors

Sub-processors below are engaged only if the corresponding feature is enabled by Customer:

| Sub-processor | Feature | Personal data processed | Region |
|---|---|---|---|
| [SSO PROVIDER NAME] *(if Enterprise SSO is configured)* | Identity federation | Authentication assertions, user identifiers | [REGION] |
| [AI PROVIDER NAME] *(if AI puzzle generation is enabled)* | Generative model API for puzzle authoring assistance | Prompt text submitted by Arranger; **no Player Data** | [REGION] |

## Removed sub-processors

| Sub-processor | Period engaged | Reason for removal |
|---|---|---|
| *(none)* | — | — |

## Notification

Customers under an active DPA can subscribe to notifications about sub-processor changes by emailing [DPO EMAIL] with subject line `subscribe: subprocessor updates`. Notifications are sent at least thirty (30) days before any new or replacement sub-processor begins processing Customer Data, and customers may object as set out in `legal/DPA.md` § 5.3.

---

## Changelog

- **1.0 — DRAFT** ([DATE]): initial document.
