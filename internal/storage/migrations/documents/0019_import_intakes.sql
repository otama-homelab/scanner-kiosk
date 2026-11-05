CREATE TABLE import_intakes (
 job_id TEXT PRIMARY KEY,
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 artifact_id TEXT NOT NULL UNIQUE,
 producer_id TEXT NOT NULL CHECK(length(producer_id)>=16),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','receiving','ready','aborted')),
 started_at_ms INTEGER CHECK(started_at_ms IS NULL OR started_at_ms>=0),
 closed_at_ms INTEGER CHECK(closed_at_ms IS NULL OR closed_at_ms>=COALESCE(started_at_ms,0)),
 CHECK((state='pending' AND started_at_ms IS NULL AND closed_at_ms IS NULL)
 OR (state='receiving' AND started_at_ms IS NOT NULL AND closed_at_ms IS NULL)
 OR (state='ready' AND started_at_ms IS NOT NULL AND closed_at_ms IS NOT NULL)
 OR (state='aborted' AND closed_at_ms IS NOT NULL)),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER import_intake_insert BEFORE INSERT ON import_intakes
WHEN NEW.state!='pending' OR NOT EXISTS(
 SELECT 1 FROM jobs j JOIN artifacts a ON a.id=NEW.artifact_id AND a.document_id=j.document_id AND a.owner_id=j.owner_id
 WHERE j.id=NEW.job_id AND j.document_id=NEW.document_id AND j.owner_id=NEW.owner_id AND j.kind='import' AND j.state='queued'
 AND a.kind='original' AND a.state='staging' AND a.media_type='application/pdf' AND a.byte_size=0 AND a.sha256 IS NULL
 AND json_extract(j.configuration_json,'$.input_artifact_id')=NEW.artifact_id)
BEGIN SELECT RAISE(ABORT,'invalid import intake'); END;
CREATE TRIGGER import_intake_update BEFORE UPDATE ON import_intakes
WHEN NEW.job_id!=OLD.job_id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.artifact_id!=OLD.artifact_id OR NEW.producer_id!=OLD.producer_id
 OR NOT ((OLD.state='pending' AND NEW.state IN ('receiving','aborted')) OR (OLD.state='receiving' AND NEW.state IN ('ready','aborted')))
 OR (OLD.started_at_ms IS NOT NULL AND NEW.started_at_ms IS NOT OLD.started_at_ms)
 OR (NEW.state IN ('ready','aborted') AND NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.artifact_id AND state='ready' AND sha256 IS NOT NULL))
BEGIN SELECT RAISE(ABORT,'invalid import intake transition'); END;
