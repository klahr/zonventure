-- Drafts and releases for adventure graphs, the way dazyflow does flows.
--
-- The designer autosaves into one draft per adventure. A draft may be
-- incomplete — an author halfway through wiring a branch has an invalid
-- graph, and autosave must not refuse it. Publishing validates the draft,
-- refuses it if it has errors, and appends it to adventure_graphs as a
-- new immutable version; published_graph_id then points at it.
--
-- Events start on the published version, never the draft, so an author
-- can rework an adventure on the morning of an event without the teams
-- seeing a half-finished edit. Rolling back is pointing at an older
-- version.
CREATE TABLE adventure_graph_drafts (
    adventure_id       BIGINT PRIMARY KEY REFERENCES adventures(id) ON DELETE CASCADE,
    graph              JSONB NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL
);

-- A release name, as dazyflow's "Name this release".
ALTER TABLE adventure_graphs ADD COLUMN label TEXT NOT NULL DEFAULT '';

ALTER TABLE adventures
    ADD COLUMN published_graph_id BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL,
    ADD COLUMN published_at       TIMESTAMPTZ;

-- Until now every saved version was live. Keep that true for the newest
-- one, so an adventure that played yesterday still starts today.
UPDATE adventures a
SET published_graph_id = g.id, published_at = g.created_at
FROM (
    SELECT DISTINCT ON (adventure_id) adventure_id, id, created_at
    FROM adventure_graphs
    ORDER BY adventure_id, version DESC
) g
WHERE g.adventure_id = a.id;
