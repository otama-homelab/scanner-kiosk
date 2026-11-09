CREATE TABLE ocr_results (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 job_id TEXT NOT NULL UNIQUE,
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 input_revision INTEGER NOT NULL CHECK(input_revision>=1),
 output_artifact_id TEXT NOT NULL,
 bundle_json TEXT NOT NULL CHECK(json_valid(bundle_json) AND length(bundle_json)<=2097152),
 bundle_sha256 BLOB NOT NULL CHECK(length(bundle_sha256)=32),
 state TEXT NOT NULL DEFAULT 'candidate' CHECK(state IN ('candidate','adopted','discarded')),
 adopted_revision INTEGER,
 created_at_ms INTEGER NOT NULL,
 CHECK((state='adopted' AND adopted_revision=input_revision+1) OR (state!='adopted' AND adopted_revision IS NULL)),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(output_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX ocr_result_candidate ON ocr_results(document_id) WHERE state='candidate';
CREATE TRIGGER ocr_result_insert BEFORE INSERT ON ocr_results
WHEN NEW.state!='candidate' OR NOT EXISTS(
 SELECT 1 FROM jobs j JOIN job_artifacts x ON x.job_id=j.id AND x.owner_id=j.owner_id
 JOIN artifacts a ON a.id=x.artifact_id AND a.document_id=x.document_id AND a.owner_id=x.owner_id
 WHERE j.id=NEW.job_id AND j.document_id=NEW.document_id AND j.owner_id=NEW.owner_id
 AND j.kind='render' AND j.state='running' AND j.input_revision=NEW.input_revision
 AND a.id=NEW.output_artifact_id AND a.kind='output' AND a.state='ready')
BEGIN SELECT RAISE(ABORT,'invalid OCR result'); END;
CREATE TRIGGER ocr_result_immutable BEFORE UPDATE ON ocr_results
WHEN NEW.id!=OLD.id OR NEW.job_id!=OLD.job_id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id
 OR NEW.input_revision!=OLD.input_revision OR NEW.output_artifact_id!=OLD.output_artifact_id
 OR NEW.bundle_json!=OLD.bundle_json OR NEW.bundle_sha256!=OLD.bundle_sha256 OR NEW.created_at_ms!=OLD.created_at_ms
 OR NOT (OLD.state='candidate' AND NEW.state IN ('adopted','discarded'))
BEGIN SELECT RAISE(ABORT,'immutable OCR result'); END;
-- The exception is tied to one explicit disposition, output, owner and revision;
-- arbitrary outputs from earlier revisions remain unusable.
DROP TRIGGER published_output_ready;
DROP TRIGGER published_output_update_ready;
CREATE TRIGGER published_output_ready BEFORE INSERT ON published_outputs
WHEN NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.id=NEW.artifact_id AND a.document_id=NEW.document_id AND a.owner_id=NEW.owner_id AND a.kind='output' AND a.state='ready'
 AND (a.input_revision=NEW.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=NEW.document_id AND r.owner_id=NEW.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=NEW.revision)))
BEGIN SELECT RAISE(ABORT,'output artifact is not ready at this revision'); END;
CREATE TRIGGER published_output_update_ready BEFORE UPDATE ON published_outputs
WHEN NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.id=NEW.artifact_id AND a.document_id=NEW.document_id AND a.owner_id=NEW.owner_id AND a.kind='output' AND a.state='ready'
 AND (a.input_revision=NEW.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=NEW.document_id AND r.owner_id=NEW.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=NEW.revision)))
BEGIN SELECT RAISE(ABORT,'output artifact is not ready at this revision'); END;
DROP TRIGGER edit_result_owner;
CREATE TRIGGER edit_result_owner BEFORE INSERT ON edit_results
WHEN NEW.document_id IS NULL OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND owner_id=NEW.owner_id)
 OR NOT EXISTS(SELECT 1 FROM request_receipts WHERE owner_id=NEW.owner_id AND key_hash=NEW.key_hash AND operation IN ('rotate','reorder','delete_page','apply_correction','undo_correction','adopt_ocr_result','discard_ocr_result') AND result_job_id IS NULL)
BEGIN SELECT RAISE(ABORT,'invalid edit result binding'); END;
