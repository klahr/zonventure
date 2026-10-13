-- TOTP replay protection: the 30 s time step of the last code a user
-- redeemed. A code for that step (or an earlier one) is refused, so one
-- code can't open two sessions inside its validity window.
ALTER TABLE users ADD COLUMN totp_last_step BIGINT;
