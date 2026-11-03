-- Persist producing-job identity for publication read-back after uncertain commits.
CREATE TABLE job_artifacts (
 job_id TEXT NOT NULL,
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 artifact_id TEXT NOT NULL UNIQUE,
 PRIMARY KEY(job_id,artifact_id),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER job_artifact_matches_input BEFORE INSERT ON job_artifacts
WHEN NOT EXISTS (
 SELECT 1 FROM jobs j JOIN artifacts a ON a.id=NEW.artifact_id
 WHERE j.id=NEW.job_id AND j.document_id=NEW.document_id AND j.owner_id=NEW.owner_id
 AND a.document_id=j.document_id AND a.owner_id=j.owner_id AND a.input_revision=j.input_revision
 AND a.state='ready'
)
BEGIN SELECT RAISE(ABORT,'job artifact input mismatch'); END;
CREATE TRIGGER job_artifact_immutable BEFORE UPDATE ON job_artifacts
BEGIN SELECT RAISE(ABORT,'immutable job artifact'); END;
