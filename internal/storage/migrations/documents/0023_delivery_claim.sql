ALTER TABLE deliveries ADD COLUMN runner_id TEXT CHECK(runner_id IS NULL OR length(runner_id)>=16);
ALTER TABLE deliveries ADD COLUMN poll_count INTEGER NOT NULL DEFAULT 0 CHECK(poll_count BETWEEN 0 AND 20);
CREATE TRIGGER delivery_runner_immutable BEFORE UPDATE ON deliveries
WHEN (OLD.runner_id IS NOT NULL AND NEW.runner_id IS NOT OLD.runner_id)
 OR (NEW.runner_id IS NOT OLD.runner_id AND NOT (OLD.state='queued' AND NEW.state='attempted' AND NEW.runner_id IS NOT NULL))
 OR NEW.poll_count<OLD.poll_count OR NEW.poll_count>OLD.poll_count+1
BEGIN SELECT RAISE(ABORT,'immutable delivery attempt'); END;
