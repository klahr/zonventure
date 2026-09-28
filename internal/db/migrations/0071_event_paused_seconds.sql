-- Pause accounting, so elapsed time can leave out the time an event was
-- paused. paused_seconds totals every closed pause; paused_at now marks
-- only the open one and is cleared when that pause ends (on resume, or
-- when a paused event is cancelled or completes). An event's paused time
-- so far is paused_seconds plus, while paused_at is set, now - paused_at.
--
-- Earlier releases set paused_at on pause and never cleared it, and kept
-- no total, so past pauses can't be recovered: they count as zero.

ALTER TABLE events ADD COLUMN paused_seconds INTEGER NOT NULL DEFAULT 0;

UPDATE events SET paused_at = NULL WHERE state <> 'paused';
