CREATE TABLE page_corrections (
 suggestion_id TEXT PRIMARY KEY CHECK(length(suggestion_id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 page_id TEXT NOT NULL,
 original_artifact_id TEXT NOT NULL,
 correction_artifact_id TEXT NOT NULL,
 source_sha256 BLOB NOT NULL CHECK(length(source_sha256)=32),
 correction_sha256 BLOB NOT NULL CHECK(length(correction_sha256)=32),
 transform_json TEXT NOT NULL CHECK(json_valid(transform_json) AND json_type(transform_json)='object'),
 confidence_kind TEXT NOT NULL CHECK(length(confidence_kind) BETWEEN 1 AND 64),
 confidence_value REAL,
 risk_flags TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(risk_flags) AND json_type(risk_flags)='array'),
 state TEXT NOT NULL DEFAULT 'suggested' CHECK(state IN ('suggested','applied','discarded')),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 applied_at_ms INTEGER CHECK(applied_at_ms IS NULL OR applied_at_ms>=created_at_ms),
 CHECK((state='applied' AND applied_at_ms IS NOT NULL) OR (state!='applied' AND applied_at_ms IS NULL)),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(page_id) REFERENCES pages(id) ON DELETE CASCADE,
 FOREIGN KEY(original_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT,
 FOREIGN KEY(correction_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
-- The artifacts table's closed kind enum predates correction. The page_corrections
-- row supplies the correction role while referencing an immutable intermediate PNG.
CREATE UNIQUE INDEX page_corrections_one_applied ON page_corrections(document_id,owner_id,page_id) WHERE state='applied';
CREATE INDEX page_corrections_page_lookup ON page_corrections(owner_id,document_id,page_id,state,created_at_ms);
CREATE TRIGGER correction_suggestion_insert BEFORE INSERT ON page_corrections
WHEN NEW.state!='suggested'
 OR NOT EXISTS(
  SELECT 1
  FROM pages p
  JOIN artifacts original ON original.id=p.original_artifact_id AND original.document_id=p.document_id AND original.owner_id=p.owner_id
  JOIN artifacts corrected ON corrected.id=NEW.correction_artifact_id AND corrected.document_id=p.document_id AND corrected.owner_id=p.owner_id
  WHERE p.id=NEW.page_id AND p.document_id=NEW.document_id AND p.owner_id=NEW.owner_id
   AND p.source_type='scan' AND p.original_artifact_id=NEW.original_artifact_id
   AND p.pixel_width IS NOT NULL AND p.pixel_height IS NOT NULL
   AND original.kind='original' AND original.state='ready' AND original.sha256=NEW.source_sha256
   AND corrected.kind='intermediate' AND corrected.state='ready' AND corrected.media_type='image/png'
   AND corrected.sha256=NEW.correction_sha256
 )
BEGIN SELECT RAISE(ABORT,'invalid correction suggestion'); END;
CREATE TRIGGER correction_suggestion_immutable BEFORE UPDATE ON page_corrections
WHEN NEW.suggestion_id!=OLD.suggestion_id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id
 OR NEW.page_id!=OLD.page_id OR NEW.original_artifact_id!=OLD.original_artifact_id
 OR NEW.correction_artifact_id!=OLD.correction_artifact_id
 OR NEW.source_sha256!=OLD.source_sha256 OR NEW.correction_sha256!=OLD.correction_sha256
 OR NEW.transform_json!=OLD.transform_json OR NEW.confidence_kind!=OLD.confidence_kind
 OR NEW.confidence_value IS NOT OLD.confidence_value OR NEW.risk_flags!=OLD.risk_flags
 OR NEW.created_at_ms!=OLD.created_at_ms
 OR NOT ((OLD.state='suggested' AND NEW.state IN ('applied','discarded'))
      OR (OLD.state='applied' AND NEW.state='discarded'))
BEGIN SELECT RAISE(ABORT,'immutable correction suggestion'); END;
-- Reuse the existing request-key/result tables for correction mutations.
DROP TRIGGER edit_result_owner;
CREATE TRIGGER edit_result_owner BEFORE INSERT ON edit_results
WHEN NEW.document_id IS NULL OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND owner_id=NEW.owner_id)
 OR NOT EXISTS(SELECT 1 FROM request_receipts WHERE owner_id=NEW.owner_id AND key_hash=NEW.key_hash AND operation IN ('rotate','reorder','delete_page','apply_correction','undo_correction') AND result_job_id IS NULL)
BEGIN SELECT RAISE(ABORT,'invalid edit result binding'); END;
