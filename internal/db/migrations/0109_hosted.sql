-- Hosted events: a party is a privately hosted event, run from a host
-- link, that plays any number of games (rounds) until it lapses. Teams
-- join from one player link and name themselves; "Finish game" closes
-- the round and mails its summary, and the next team to join opens the
-- next one. A booked event must start its first game within 60 days of
-- purchase and is playable for 14 days from it (events.valid_until).

-- player_token is the player link's secret, printed as a QR code, so it
-- is kept in the clear. hosted_blocks is how many blocks of 15 teams a
-- game may hold. current_round is the latest round, 0 before the first.
ALTER TABLE events
    ADD COLUMN player_token   TEXT UNIQUE,
    ADD COLUMN hosted_blocks  SMALLINT NOT NULL DEFAULT 1 CHECK (hosted_blocks >= 1),
    ADD COLUMN first_game_at  TIMESTAMPTZ,
    ADD COLUMN current_round  INTEGER NOT NULL DEFAULT 0;

-- One row per game of a hosted event. graph_id is the copy the round
-- plays, pinned when it opens, so edits between games apply from the
-- next one. finished_at closes it; summary_sent_at records its mail.
CREATE TABLE event_rounds (
    event_id         BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    round            INTEGER NOT NULL,
    graph_id         BIGINT REFERENCES adventure_graphs(id) ON DELETE SET NULL,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at      TIMESTAMPTZ,
    summary_sent_at  TIMESTAMPTZ,
    PRIMARY KEY (event_id, round)
);

CREATE INDEX idx_event_rounds_open ON event_rounds (started_at) WHERE finished_at IS NULL;

-- The round a team plays in, and how many players it says it has. A
-- team name is unique within its game, so "Owls" can play every game.
ALTER TABLE teams
    ADD COLUMN round      INTEGER,
    ADD COLUMN headcount  SMALLINT CHECK (headcount BETWEEN 1 AND 50);

ALTER TABLE teams DROP CONSTRAINT teams_event_id_name_key;
CREATE UNIQUE INDEX teams_event_round_name ON teams (event_id, COALESCE(round, 0), name);

-- A round's own graph copy belongs to its event and goes with it. Its
-- version is negative, from a sequence that starts past every event id,
-- so it never meets the -eventID versions of parties pinned before.
ALTER TABLE adventure_graphs
    ADD COLUMN event_id BIGINT REFERENCES events(id) ON DELETE CASCADE;

CREATE SEQUENCE round_graph_version_seq;
SELECT setval('round_graph_version_seq', GREATEST((SELECT COALESCE(MAX(id), 0) FROM events), 1));
