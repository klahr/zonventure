# Legal settings in the console

Status: spec, not built. Same approach as Settings → Pricing
(PLATFORM_PRICING.md): values in the `settings` table, held in memory,
edited by the platform admin, no restart.

## The problem

Who runs the platform, how to reach them and how long data is kept are
deploy config today: 14 env vars (plus the map's three) read once at start-up
(`cmd/server/main.go`, `server.LegalConfig`). Changing an address or a
retention period means editing the server's env file and restarting,
and nothing shows what `/privacy` and `/terms` will say until they are
live. Until the three required values are set, both pages answer 503
with a list of env var names.

The values reach more than the two pages: the seller line next to every
buy button and in store footers (e-handelslagen), the landing page's
schema.org publisher, the sitemap and the footer links (hidden while
the pages are unavailable), and the retention sweepers.

## What we build

**Settings → Legal**, a tab for the platform `admin` role only. One
form in four parts, saved together:

1. **Company**: name\*, organisation number, VAT number, address.
   Shown on the legal pages and as the seller line by every buy button.
2. **Contact**: contact email\*, data protection email (blank uses the
   contact email), jurisdiction\* (e.g. "Sweden").
3. **Sub-processors** named in the privacy policy: hosting provider,
   email provider.
4. **Data kept**: days before player locations, login history and a
   finished party are deleted (default 30, 90, 14; 0 keeps them).

\* Required. Until all three are set, the tab says "Privacy and terms
are not published yet" and names what is missing; the pages keep
answering 503, now pointing to Settings → Legal instead of env vars.

Beside the form:

- **Preview**: the privacy policy and the terms, as they will read with
  the values typed so far, before saving (a tab for each, in an iframe).
- **Reviewed by a lawyer**: a checkbox that removes the yellow "draft
  template" notes from both pages. Off by default.
- **Last updated**: the date the pages show. Set to today whenever a
  save changes something the pages print; can be edited.
- Links to the live `/privacy` and `/terms`.

## How it works

- One key per value in `settings` (`legal_company_name`, …), loaded at
  start-up into an in-memory copy that a save replaces, like pricing.
- `LegalConfig` stops being a start-up value: everything that took
  `cfg.Legal` (legal pages, seller line splicing, hidden links,
  sitemap, landing page, `SellerIdentity`, the Installed components
  entry) reads the current copy per request. Cached renders key on it.
- The retention sweepers read the days on each run, so the policy text
  and what is actually deleted can't disagree. The
  `--retention-*-days` flags go with the env vars.
- Validation: emails must look like emails, days are 0–3650, text
  fields have a length limit. Required fields may be saved empty (the
  pages then stay unpublished).
- Every save is logged (`admin.legal`) with who and what changed.
- `POST /api/admin/legal/preview {values, page}` renders a page from
  unsaved values for the preview; it never writes.

## Upgrading

The first start after the release copies each legal env var that is
set into `settings`, once, when that key isn't there yet, and logs
"ZV_COMPANY_NAME copied to Settings → Legal; remove it from the env".
After that the env vars are ignored with the same warning as the
pricing ones. Production keeps its pages without retyping anything.

Removed: `ZV_COMPANY_NAME`, `ZV_COMPANY_ORG_NUMBER`,
`ZV_COMPANY_ADDRESS`, `ZV_COMPANY_VAT_NUMBER`, `ZV_CONTACT_EMAIL`,
`ZV_DPO_EMAIL`, `ZV_JURISDICTION`, `ZV_HOSTING_PROVIDER`,
`ZV_EMAIL_PROVIDER`, `ZV_LEGAL_REVIEWED`, `ZV_LEGAL_LAST_UPDATED`,
`ZV_LOCATION_RETENTION_DAYS`, `ZV_AUDIT_RETENTION_DAYS`,
`ZV_PARTY_RETENTION_DAYS`. Docs, `.env.example` and compose files
follow.

## Not in this spec

- **Maps** (`ZV_MAP_STYLE_URL`, the optional raster `ZV_MAP_TILE_URL`
  and `_ATTRIBUTION`, `ZV_MAP_PROVIDER`). They sit in `LegalConfig`
  because the privacy policy names the map hosts, but the style and
  tile URLs also build the Content-Security-Policy at start-up. Next batch, with the site settings.
- Editing the page texts themselves. The templates stay in the code
  (or `ZV_WEB_OVERRIDE_DIR`); this page fills in their blanks.
- Secrets and start-up settings stay env vars (see the earlier split).

## Decided

- The values are the same in every language (English is fine on the
  Swedish pages).
- Saving a change never unticks "Reviewed by a lawyer": the review is of
  the template wording, not the values filled in.
