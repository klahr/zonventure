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

## Deferred

Recorded once with a rationale; revisit only on a fresh signal.

- [m] Email-change flow re-banners — Arranger.
  Existing copy is decent; defer until a confirmed user complaint.
- [m] `autocomplete="off"` on answer input — Player.
  Trade-off between auto-fill nuisance and accessibility; debatable
  either way.
