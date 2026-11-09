CREATE INDEX deliveries_document_pin ON deliveries(document_id,owner_id,state,expires_at_ms);
CREATE INDEX deliveries_retirement ON deliveries(expires_at_ms,id) WHERE state!='retired';
CREATE INDEX delivery_receipts_aged_retirement ON delivery_receipts(expires_at_ms,key_expires_at_ms,owner_id,key_hash) WHERE key_expires_at_ms IS NOT NULL;

DROP TRIGGER delivery_snapshot_immutable;
CREATE TRIGGER delivery_snapshot_immutable BEFORE UPDATE ON deliveries
WHEN NEW.id!=OLD.id OR NEW.owner_id!=OLD.owner_id OR NEW.revision!=OLD.revision
 OR NEW.payload_sha256!=OLD.payload_sha256 OR NEW.expires_at_ms!=OLD.expires_at_ms
 OR NEW.destination_id!=OLD.destination_id OR NEW.filename!=OLD.filename OR NEW.created_at_ms!=OLD.created_at_ms
 OR (OLD.attempted_at_ms IS NOT NULL AND NEW.attempted_at_ms IS NOT OLD.attempted_at_ms)
 OR (OLD.state='retired' AND NEW.state!='retired')
 OR (NEW.state='retired' AND (NEW.document_id!='retired' OR NEW.artifact_id!='retired' OR NEW.relative_path!='retired'
     OR NEW.media_type!='application/pdf' OR NEW.byte_size!=1 OR NEW.config_fingerprint!=printf('%064d',0)
     OR NEW.task_id IS NOT NULL OR NEW.remote_document_id IS NOT NULL OR NEW.confirmed_sha256 IS NOT NULL))
 OR (NEW.state!='retired' AND (
 NEW.document_id!=OLD.document_id OR NEW.artifact_id!=OLD.artifact_id OR NEW.relative_path!=OLD.relative_path
 OR NEW.media_type!=OLD.media_type OR NEW.byte_size!=OLD.byte_size OR NEW.config_fingerprint!=OLD.config_fingerprint
 OR (OLD.task_id IS NOT NULL AND NEW.task_id IS NOT OLD.task_id)
 OR (OLD.remote_document_id IS NOT NULL AND NEW.remote_document_id IS NOT OLD.remote_document_id)
 OR (OLD.confirmed_sha256 IS NOT NULL AND NEW.confirmed_sha256 IS NOT OLD.confirmed_sha256)
 OR NOT (NEW.state=OLD.state OR (OLD.state='queued' AND NEW.state IN ('attempted','cancelled'))
 OR (OLD.state='attempted' AND NEW.state IN ('accepted','unknown','failed'))
 OR (OLD.state='accepted' AND NEW.state IN ('confirmed','unknown','failed'))
 OR (OLD.state='unknown' AND NEW.state IN ('accepted','confirmed','failed')))))
BEGIN SELECT RAISE(ABORT,'immutable delivery evidence'); END;
