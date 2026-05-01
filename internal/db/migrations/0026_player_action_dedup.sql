-- Idempotency dedup for player-side mutating actions (answer submit,
-- hint reveal, location ping). Without this, a retry that fires when
-- a flaky network blip caused the client to think its first request
-- was lost — perfectly normal for offline-then-back replay — can
-- double-process: two answer_attempts rows on a wrong answer, or a
-- correct answer evaluated twice against now-different team state.
--
-- The dedup is keyed by (slot_id, client_action_id). The client-side
-- queue (offline replay) generates a stable UUID per user action and
-- attaches it to every retry of the same intent. The server stores
-- the original response and replays it on subsequent calls.
--
-- ON DELETE CASCADE on slot_id keeps the table from outliving its
-- referent — when a slot rotates (player_slots is reset between
-- events, or a slot is deleted), its dedup history goes too.
CREATE TABLE player_action_dedup (
    slot_id          BIGINT      NOT NULL REFERENCES player_slots(id) ON DELETE CASCADE,
    client_action_id TEXT        NOT NULL,
    response_status  INT         NOT NULL,
    response_body    TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (slot_id, client_action_id)
);

-- Sweep index for the retention job — actions older than a day or two
-- are far past any plausible offline-replay window.
CREATE INDEX idx_player_action_dedup_created ON player_action_dedup(created_at);
