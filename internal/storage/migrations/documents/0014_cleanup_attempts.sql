-- Physical cleanup may outlive receipt retention. A bounded execution attempt
-- has its own deadline; immutable job/document deadlines remain unchanged.
CREATE TABLE cleanup_attempts (
 job_id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL,
 runner_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation>=1),
 started_at_ms INTEGER NOT NULL CHECK(started_at_ms>=0),
 deadline_at_ms INTEGER NOT NULL CHECK(deadline_at_ms>started_at_ms AND deadline_at_ms-started_at_ms<=900000),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER cleanup_attempt_insert BEFORE INSERT ON cleanup_attempts
WHEN NEW.generation!=1 OR NOT EXISTS(SELECT 1 FROM jobs j WHERE j.id=NEW.job_id AND j.owner_id=NEW.owner_id AND j.kind='cleanup' AND j.state='running' AND j.runner_id=NEW.runner_id AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=NEW.started_at_ms)
BEGIN SELECT RAISE(ABORT,'invalid cleanup attempt'); END;
CREATE TRIGGER cleanup_attempt_update BEFORE UPDATE ON cleanup_attempts
WHEN NEW.job_id!=OLD.job_id OR NEW.owner_id!=OLD.owner_id OR NEW.generation!=OLD.generation+1 OR NEW.runner_id=OLD.runner_id OR NEW.started_at_ms<OLD.started_at_ms OR NOT EXISTS(SELECT 1 FROM jobs j WHERE j.id=NEW.job_id AND j.owner_id=NEW.owner_id AND j.kind='cleanup' AND j.state='running' AND j.runner_id=NEW.runner_id AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=NEW.started_at_ms)
BEGIN SELECT RAISE(ABORT,'invalid cleanup attempt transition'); END;
