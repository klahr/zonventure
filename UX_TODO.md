# UX TODO — admins / arrangers / players

Open items from the in-depth UX review (last refresh 2026-04-29).
Ordered by impact: top items move the needle, bottom items are polish.
Format: `- [ ] [SEV] short — role — file:line — note`.

Severity: **B** blocker · **M** major · **m** minor · **n** nit.

---

## Major

- [ ] [M] Privacy/Terms pages aren't localized — Player.
  Legal-facing copy ships in English only; Swedish players hit English
  text on the consent flow. Customer-visible, small lift.
- [ ] [M] No PWA manifest / installable shell for `/play` — Player.
  Adds offline shell + home-screen install. Players currently launch
  via mobile browser only; first-event retention suffers.

## Visual consistency (sweep 2026-10-07)

The sweep's items are done. Left over, by choice:

- [n] Console trail creator keeps trail-btn pills, unlike other console
  pages: it's the same editor as the public site.
- [n] The org's party-offer list carries no look (only the single offer
  behind Book does), to avoid a query per offer.
- [n] Privacy/Terms topbar has no language picker: the content is
  English-only.
- [n] The adventure's own cover is validated by the same subquery in
  internal/admin/lookcover.go and internal/public/look.go (they return
  a media id vs a public URL). Merge if a third caller appears.

## Deferred

Recorded once with a rationale; revisit only on a fresh signal.

- [m] Email-change flow re-banners — Arranger.
  Existing copy is decent; defer until a confirmed user complaint.
- [m] `autocomplete="off"` on answer input — Player.
  Trade-off between auto-fill nuisance and accessibility; debatable
  either way.
