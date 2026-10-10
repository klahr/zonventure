-- Tamper-evident results (internal/results). Every event keeps a hash
-- chain of what decides its results: each answer a team gives, right or
-- wrong, each hint, each step, each score change from outside the game
-- (a photo's verdict), each organiser action that bears on the standings
-- (withdrawing a team, unlocking it, finishing it by hand), each team's
-- finish and the event's end. Every entry carries the server's time and
-- the team's standing after it, and its SHA-256 covers the entry before
-- it, so an entry can't be changed, removed or slipped in afterwards
-- without breaking every hash after it.
--
-- A team that finishes gets a receipt: the hash of its finish entry,
-- which covers everything the event recorded up to then, its own
-- answers included. The event's fingerprint is the hash of its last
-- entry once it is over. Neither the organiser nor anyone with the
-- database can change who answered what, when, or the scores without
-- it showing against receipts the teams already hold.
--
-- payload is the entry's JSON exactly as hashed. Names are copied in;
-- nothing but the event is referenced. The log goes with its event —
-- deleting the event (or its organisation, or the retention sweeper
-- deleting a party) deletes it — and otherwise a trigger refuses
-- updates and deletes.

CREATE TABLE event_result_log (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    seq         BIGINT NOT NULL,
    team_id     BIGINT,
    team_name   TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    payload     TEXT NOT NULL,
    server_time TIMESTAMPTZ NOT NULL,
    prev_hash   TEXT NOT NULL,
    entry_hash  TEXT NOT NULL UNIQUE,
    UNIQUE (event_id, seq)
);

CREATE INDEX idx_event_result_log_team ON event_result_log (team_id, seq) WHERE team_id IS NOT NULL;

CREATE FUNCTION event_result_log_append_only() RETURNS trigger AS $$
BEGIN
    -- The event's own deletion takes its log along; nothing else may.
    IF TG_OP = 'DELETE' AND NOT EXISTS (SELECT 1 FROM events WHERE id = OLD.event_id) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'event_result_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER event_result_log_append_only
    BEFORE UPDATE OR DELETE ON event_result_log
    FOR EACH ROW EXECUTE FUNCTION event_result_log_append_only();
