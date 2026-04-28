-- Per-team hint reveal log. Replaces the localStorage-only reveal
-- counter so a device switch (phone dies, swap to tablet), a storage
-- clear, or another team-mate revealing a hint all converge on the
-- same view. One row per (team, page, hint) gives an ON CONFLICT DO
-- NOTHING upsert that's safe for double-tap submissions.
CREATE TABLE team_hint_reveals (
    team_id     BIGINT      NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    page_id     BIGINT      NOT NULL REFERENCES adventure_pages(id) ON DELETE CASCADE,
    hint_index  INT         NOT NULL,
    revealed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, page_id, hint_index)
);

-- The play-state read path queries "how many hints has this team
-- already revealed on this page", which is COUNT(*) keyed by the
-- (team, page) prefix of the primary key. Postgres can serve that
-- directly off the PK; no secondary index is needed.
