-- The migration runner disables FK actions on its private connection and checks
-- the complete graph before commit; children must not cascade or be nulled here.
PRAGMA legacy_alter_table=ON;
CREATE TABLE jobs_next (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('scan','render','ocr','cleanup','import')),
 state TEXT NOT NULL CHECK(state IN ('queued','running','cancelling','cancelled','succeeded','failed','interrupted','reconciling')),
 input_revision INTEGER NOT NULL CHECK(input_revision>=1),
 configuration_json TEXT NOT NULL CHECK(json_valid(configuration_json) AND json_type(configuration_json)='object'),
 created_at_ms INTEGER NOT NULL,
 deadline_at_ms INTEGER NOT NULL CHECK(deadline_at_ms>created_at_ms),
 runner_id TEXT,
 heartbeat_at_ms INTEGER,
 error_code TEXT,
 started_at_ms INTEGER CHECK(started_at_ms IS NULL OR started_at_ms>=created_at_ms),
 UNIQUE(id,owner_id),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE
) STRICT;
INSERT INTO jobs_next(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,heartbeat_at_ms,error_code,started_at_ms)
 SELECT id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,heartbeat_at_ms,error_code,started_at_ms FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_next RENAME TO jobs;
PRAGMA legacy_alter_table=OFF;
CREATE INDEX jobs_work ON jobs(state,created_at_ms);
CREATE UNIQUE INDEX jobs_one_active_per_document ON jobs(document_id)
 WHERE kind!='cleanup' AND state IN ('queued','running','cancelling','reconciling');
CREATE INDEX jobs_document_revision ON jobs(document_id,owner_id,input_revision DESC,created_at_ms DESC,id DESC);
CREATE UNIQUE INDEX jobs_one_cleanup_per_document ON jobs(document_id) WHERE kind='cleanup';
CREATE INDEX jobs_cleanup_retirement ON jobs(heartbeat_at_ms,deadline_at_ms,id)
 WHERE kind='cleanup' AND state='succeeded';
CREATE TRIGGER job_deadline_within_retention BEFORE INSERT ON jobs
WHEN NEW.kind!='cleanup' AND NEW.deadline_at_ms>(SELECT expires_at_ms FROM documents WHERE id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'job exceeds retention'); END;
CREATE TRIGGER job_snapshot_immutable BEFORE UPDATE ON jobs
WHEN NEW.id!=OLD.id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.kind!=OLD.kind OR NEW.input_revision!=OLD.input_revision OR NEW.configuration_json!=OLD.configuration_json OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.deadline_at_ms!=OLD.deadline_at_ms
BEGIN SELECT RAISE(ABORT,'immutable job input'); END;
CREATE TRIGGER job_input_current_at_admission BEFORE INSERT ON jobs
WHEN NEW.kind!='cleanup' AND NEW.input_revision!=(SELECT revision FROM documents WHERE id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'job input is not current'); END;
CREATE TRIGGER job_start_immutable BEFORE UPDATE OF started_at_ms ON jobs
WHEN OLD.started_at_ms IS NOT NULL AND NEW.started_at_ms IS NOT OLD.started_at_ms
BEGIN SELECT RAISE(ABORT,'immutable job start'); END;
