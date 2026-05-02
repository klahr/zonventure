-- Per-adventure reuse mode. Authors signal at publish time whether
-- their adventure is meant to be run as-is (paid licence per event,
-- buyer can't edit) or used as a starting point (buyer forks it into
-- their org and customises). The Catalog detail page branches the
-- buy CTA on this column.
--
-- as_is    — license-to-run. Buyer pays per-player; the resulting
--            assignment lets them run one event using the original
--            adventure. Suits genuinely venue-agnostic content
--            (text trivia, image puzzles, narrative).
-- template — fork-on-buy. Buyer clones into their own org and
--            edits locations, prompts, gates. Suits the common case
--            where the author's structure is reusable but the venue-
--            specific content needs to be re-authored for the
--            buyer's location. Currently free to fork; if a future
--            pricing model wants per-fork charges, the column is
--            already in place.
--
-- Defaults to 'as_is' so the existing catalog content keeps the
-- pricing-and-licensing behaviour it had before this column existed.
-- Changing the mode on an already-published adventure is allowed —
-- authors can experiment.

ALTER TABLE adventures ADD COLUMN reuse_mode TEXT NOT NULL DEFAULT 'as_is'
  CHECK (reuse_mode IN ('as_is', 'template'));
