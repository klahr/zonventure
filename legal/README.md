# Procurement kit

> **DRAFT TEMPLATES — NOT LEGAL ADVICE.**
> Every document in this folder is a starting point for B2B sales conversations. Have them reviewed by a lawyer qualified in your jurisdiction before signing or sharing with a customer.

This folder contains the documents a corporate buyer's legal / procurement / IT-security team typically asks for before signing. Hand them out proactively to shorten review cycles.

| File | Use it for |
|---|---|
| [`MSA.md`](MSA.md) | Master Services Agreement — the commercial parent contract. Covers license, fees, warranties, liability, IP, term. |
| [`DPA.md`](DPA.md) | Data Processing Agreement — GDPR Article 28 contract. Required when Zonventure AB hosts the service on the customer's behalf. Self-host customers don't need it (they are their own controller and processor). |
| [`SECURITY.md`](SECURITY.md) | Security one-pager — summary of how the service handles data, what the architecture looks like, sub-processors, incident response. Answers ~80% of vendor security questionnaires. |
| [`SUBPROCESSORS.md`](SUBPROCESSORS.md) | Current list of sub-processors. Referenced by the DPA Annex 3. Update this whenever a vendor changes. |

## How to use

1. **Pre-sales:** point the prospect's legal/security contact at this folder (or the rendered PDFs you generate from it). Most procurement reviews accelerate dramatically when the vendor already has these prepared.
2. **Negotiation:** customers will redline. Track changes in a customer-specific copy; never edit these masters without versioning.
3. **Signing:** convert the agreed redline to PDF, sign via your e-sign tool of choice, and store the executed copy in your contracts repository.
4. **Renewal / change:** re-issue when sub-processors change, when the SaaS vs self-host model for that customer changes, or when material features that affect data handling are added (e.g., AI features, ticketing, payments).

## Self-host vs hosted

The kit covers both deployment models, but the responsibility split differs:

- **Self-host** — the customer runs Zonventure on their own infrastructure. Zonventure AB is the **software licensor** only. The DPA is generally not needed because Zonventure AB does not process the customer's player data.
- **Hosted SaaS** — Zonventure AB operates the service on the customer's behalf. Zonventure AB is a **data processor** for player and arranger data. The DPA applies.

The MSA covers both. The DPA only applies to the hosted scenario.

## Versioning

Bump the date and version field at the top of each document when you change material terms. Keep a `CHANGELOG` section in each document so customers under an older version know what changed.
