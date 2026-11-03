-- Edit receipts share the principal/key namespace with scan/render admission.
-- Only public metadata is retained; no document text, filenames or storage paths.
CREATE TABLE edit_results (
 owner_id TEXT NOT NULL,
 key_hash BLOB NOT NULL,
 document_id TEXT REFERENCES documents(id) ON DELETE SET NULL,
 projection_json TEXT NOT NULL CHECK(json_valid(projection_json) AND json_type(projection_json)='object'),
 PRIMARY KEY(owner_id,key_hash),
 FOREIGN KEY(owner_id,key_hash) REFERENCES request_receipts(owner_id,key_hash) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER edit_result_owner BEFORE INSERT ON edit_results
WHEN NEW.document_id IS NULL OR NOT EXISTS(SELECT 1 FROM documents WHERE id=NEW.document_id AND owner_id=NEW.owner_id)
 OR NOT EXISTS(SELECT 1 FROM request_receipts WHERE owner_id=NEW.owner_id AND key_hash=NEW.key_hash AND operation IN ('rotate','reorder','delete_page') AND result_job_id IS NULL)
BEGIN SELECT RAISE(ABORT,'invalid edit result binding'); END;
CREATE TRIGGER edit_result_immutable BEFORE UPDATE ON edit_results
WHEN NEW.owner_id!=OLD.owner_id OR NEW.key_hash!=OLD.key_hash OR NEW.projection_json!=OLD.projection_json
 OR (NEW.document_id IS NOT OLD.document_id AND NEW.document_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'immutable edit result'); END;
