-- Durable immutable file manifest, retained across unlink/commit interruptions.
CREATE TABLE cleanup_files (
 job_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 artifact_id TEXT NOT NULL,
 relative_path TEXT NOT NULL,
 byte_size INTEGER NOT NULL CHECK(byte_size>=0),
 sha256 BLOB NOT NULL CHECK(length(sha256)=32),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','deleted')),
 deleted_at_ms INTEGER,
 PRIMARY KEY(job_id,artifact_id),
 UNIQUE(job_id,relative_path),
 CHECK((state='pending' AND deleted_at_ms IS NULL) OR (state='deleted' AND deleted_at_ms IS NOT NULL)),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER cleanup_file_matches_artifact BEFORE INSERT ON cleanup_files
WHEN NEW.state!='pending' OR NEW.deleted_at_ms IS NOT NULL OR NOT EXISTS(SELECT 1 FROM jobs j JOIN artifacts a ON a.id=NEW.artifact_id AND a.document_id=j.document_id AND a.owner_id=j.owner_id WHERE j.id=NEW.job_id AND j.owner_id=NEW.owner_id AND j.kind='cleanup' AND j.state='running' AND a.state='deleting' AND a.relative_path=NEW.relative_path AND a.byte_size=NEW.byte_size AND a.sha256=NEW.sha256)
BEGIN SELECT RAISE(ABORT,'invalid cleanup file'); END;
CREATE TRIGGER cleanup_file_immutable BEFORE UPDATE ON cleanup_files
WHEN NEW.job_id!=OLD.job_id OR NEW.owner_id!=OLD.owner_id OR NEW.artifact_id!=OLD.artifact_id OR NEW.relative_path!=OLD.relative_path OR NEW.byte_size!=OLD.byte_size OR NEW.sha256 IS NOT OLD.sha256 OR OLD.state='deleted' OR NEW.state!='deleted'
BEGIN SELECT RAISE(ABORT,'immutable cleanup file'); END;
