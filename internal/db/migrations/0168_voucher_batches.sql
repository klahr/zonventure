-- Voucher codes an organisation prints and pays for later: a publisher
-- puts one in each envelope it sells through bookstores, and the buyer
-- types it at /play. A batch belongs to a self-guided event; each code
-- starts one team of up to team_size players the first time it is used,
-- and that team keeps the code as its voucher code, so the code then
-- opens the same team for the rest of the players.
--
-- Only organisations with invoice billing may create batches. Each use
-- is charged once, at the price in force when it was used (price_cents,
-- currency), and waits on the platform's invoice until a platform admin
-- marks it billed with the invoice's number. Unused codes cost nothing.
CREATE TABLE voucher_batches (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    label       TEXT NOT NULL DEFAULT '',
    team_size   INTEGER NOT NULL CHECK (team_size BETWEEN 1 AND 20),
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Voided: its unused codes no longer start a team (a pulped print
    -- run). Teams already started keep playing.
    voided_at   TIMESTAMPTZ
);
CREATE INDEX idx_voucher_batches_event ON voucher_batches(event_id);

CREATE TABLE voucher_codes (
    code           TEXT PRIMARY KEY,
    batch_id       BIGINT NOT NULL REFERENCES voucher_batches(id) ON DELETE CASCADE,
    team_id        BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    activated_at   TIMESTAMPTZ,
    price_cents    INTEGER,
    currency       TEXT,
    billed_at      TIMESTAMPTZ,
    invoice_number TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_voucher_codes_batch ON voucher_codes(batch_id);
CREATE INDEX idx_voucher_codes_unbilled ON voucher_codes(batch_id)
    WHERE activated_at IS NOT NULL AND billed_at IS NULL;
