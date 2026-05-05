-- Adventure starting location. The first gated puzzle is the de-facto
-- "go here first" point, but authors often want to nudge players to a
-- nearby meeting spot — a metro exit, a bench by the trailhead, the
-- shaded corner where a courier hands out kits — that doesn't fit
-- inside a puzzle's gate radius. With the lanes feature any of P1's
-- assumptions can also evaporate (a shuffled lane has no "first"
-- puzzle from the author's perspective), so we promote the start
-- location to its own first-class field.
--
-- All three columns are optional. NULL coordinates mean "no explicit
-- start — fall back to the first gated puzzle's location" so existing
-- adventures keep working without a backfill. The label is a short
-- free-text hint ("Monteliusvägen, ovanför bänken vid trappan") that
-- the player intro overlay and the catalog detail page surface
-- alongside a map link.
ALTER TABLE adventures
    ADD COLUMN start_lat   DOUBLE PRECISION,
    ADD COLUMN start_lng   DOUBLE PRECISION,
    ADD COLUMN start_label TEXT NOT NULL DEFAULT '';
