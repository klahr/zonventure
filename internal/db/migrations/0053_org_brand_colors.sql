-- Brand colors at the org level — accent, accent secondary, surface
-- background, and text/ink. Mirrors the per-adventure brand columns
-- introduced earlier (adventures.accent_color etc.) so an arranger
-- who already learned the per-adventure picker recognises the
-- shape, but applies once per org so the landing (`/o/{slug}`) and
-- store (`/o/{slug}/store`) inherit a consistent palette without
-- the operator having to recolour each adventure individually.
--
-- All columns nullable so existing rows stay on the platform-wide
-- defaults (the renderer falls back to the built-in palette when a
-- column is empty). New orgs default to NULL too — they pick up
-- the platform default until the org_admin opens the branding
-- card and saves a custom palette.
--
-- Hex stored as `#RRGGBB` text; validation lives in the handler
-- (a CHECK constraint here would tie the regex to one Postgres
-- version's `~` semantics and we already validate at the boundary).

ALTER TABLE organizations
    ADD COLUMN accent_color           TEXT,
    ADD COLUMN accent_secondary_color TEXT,
    ADD COLUMN background_color       TEXT,
    ADD COLUMN text_color             TEXT;
