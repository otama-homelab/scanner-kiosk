-- A mutable heartbeat cannot identify when one-time physical work started.
ALTER TABLE jobs ADD COLUMN started_at_ms INTEGER CHECK(started_at_ms IS NULL OR started_at_ms>=created_at_ms);
CREATE TRIGGER job_start_immutable BEFORE UPDATE OF started_at_ms ON jobs
WHEN OLD.started_at_ms IS NOT NULL AND NEW.started_at_ms IS NOT OLD.started_at_ms
BEGIN SELECT RAISE(ABORT,'immutable job start'); END;
