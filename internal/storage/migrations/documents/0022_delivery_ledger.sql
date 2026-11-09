-- Retain send evidence independently of document/artifact cleanup.
CREATE TABLE deliveries (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
 document_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),
 artifact_id TEXT NOT NULL,
 relative_path TEXT NOT NULL CHECK(length(relative_path)>0 AND relative_path NOT LIKE '/%' AND instr(relative_path,'..')=0 AND instr(relative_path,char(92))=0 AND instr(relative_path,char(0))=0),
 media_type TEXT NOT NULL CHECK(media_type='application/pdf'),
 byte_size INTEGER NOT NULL CHECK(byte_size>0 AND byte_size<=134217728),
 payload_sha256 BLOB NOT NULL CHECK(length(payload_sha256)=32),
 expires_at_ms INTEGER NOT NULL,
 destination_id TEXT NOT NULL CHECK(length(destination_id) BETWEEN 1 AND 128),
 config_fingerprint TEXT NOT NULL CHECK(length(config_fingerprint)=64 AND config_fingerprint NOT GLOB '*[^0-9a-f]*'),
 filename TEXT NOT NULL UNIQUE CHECK(filename=id||'.pdf'),
 state TEXT NOT NULL CHECK(state IN ('queued','attempted','accepted','unknown','confirmed','failed','cancelled','retired')),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0 AND expires_at_ms>created_at_ms),
 attempted_at_ms INTEGER CHECK(attempted_at_ms>=created_at_ms AND attempted_at_ms<expires_at_ms),
 task_id TEXT CHECK(length(task_id)=36),
 remote_document_id INTEGER CHECK(remote_document_id>0),
 confirmed_sha256 BLOB CHECK(confirmed_sha256=payload_sha256),
 CHECK((state IN ('queued','cancelled') AND attempted_at_ms IS NULL) OR state='retired' OR (state IN ('attempted','accepted','unknown','confirmed','failed') AND attempted_at_ms IS NOT NULL)),
 CHECK(state NOT IN ('accepted','confirmed') OR task_id IS NOT NULL),
 CHECK(state!='confirmed' OR (remote_document_id IS NOT NULL AND confirmed_sha256 IS NOT NULL)),
 UNIQUE(id,owner_id),
 UNIQUE(owner_id,destination_id,payload_sha256)
) STRICT;
CREATE INDEX deliveries_work ON deliveries(state,created_at_ms);
CREATE TABLE delivery_receipts (
 owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
 key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
 request_hash BLOB NOT NULL CHECK(length(request_hash)=32),
 delivery_id TEXT NOT NULL,
 expires_at_ms INTEGER NOT NULL,
 key_expires_at_ms INTEGER CHECK(key_expires_at_ms>0),
 PRIMARY KEY(owner_id,key_hash),
 FOREIGN KEY(delivery_id,owner_id) REFERENCES deliveries(id,owner_id) ON DELETE RESTRICT
) STRICT;
CREATE TRIGGER delivery_snapshot_insert BEFORE INSERT ON deliveries
WHEN NEW.state!='queued' OR NEW.attempted_at_ms IS NOT NULL OR NEW.task_id IS NOT NULL
 OR NEW.remote_document_id IS NOT NULL OR NEW.confirmed_sha256 IS NOT NULL OR NOT EXISTS (
 SELECT 1 FROM published_outputs p JOIN documents d ON d.id=p.document_id AND d.owner_id=p.owner_id AND d.revision=p.revision
 JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=d.id AND a.owner_id=d.owner_id
 WHERE d.id=NEW.document_id AND d.owner_id=NEW.owner_id AND d.revision=NEW.revision AND d.state='ready'
 AND d.expires_at_ms=NEW.expires_at_ms AND a.id=NEW.artifact_id AND a.state='ready' AND a.kind='output'
 AND a.relative_path=NEW.relative_path AND a.media_type=NEW.media_type AND a.byte_size=NEW.byte_size AND a.sha256=NEW.payload_sha256
 AND (a.input_revision=d.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=d.id AND r.owner_id=d.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=d.revision)))
BEGIN SELECT RAISE(ABORT,'invalid delivery snapshot'); END;
CREATE TRIGGER delivery_snapshot_immutable BEFORE UPDATE ON deliveries
WHEN NEW.id!=OLD.id OR NEW.owner_id!=OLD.owner_id OR NEW.document_id!=OLD.document_id OR NEW.revision!=OLD.revision
 OR NEW.artifact_id!=OLD.artifact_id OR NEW.relative_path!=OLD.relative_path OR NEW.media_type!=OLD.media_type
 OR NEW.byte_size!=OLD.byte_size OR NEW.payload_sha256!=OLD.payload_sha256 OR NEW.expires_at_ms!=OLD.expires_at_ms
 OR NEW.destination_id!=OLD.destination_id OR NEW.config_fingerprint!=OLD.config_fingerprint OR NEW.filename!=OLD.filename OR NEW.created_at_ms!=OLD.created_at_ms
 OR (OLD.attempted_at_ms IS NOT NULL AND NEW.attempted_at_ms IS NOT OLD.attempted_at_ms)
 OR (OLD.task_id IS NOT NULL AND NEW.task_id IS NOT OLD.task_id)
 OR (OLD.remote_document_id IS NOT NULL AND NEW.remote_document_id IS NOT OLD.remote_document_id)
 OR (OLD.confirmed_sha256 IS NOT NULL AND NEW.confirmed_sha256 IS NOT OLD.confirmed_sha256)
 OR NOT (NEW.state=OLD.state OR (OLD.state='queued' AND NEW.state IN ('attempted','cancelled','retired'))
 OR (OLD.state='attempted' AND NEW.state IN ('accepted','unknown','failed','retired'))
 OR (OLD.state='accepted' AND NEW.state IN ('confirmed','unknown','failed','retired'))
 OR (OLD.state='unknown' AND NEW.state IN ('accepted','confirmed','failed','retired'))
 OR (OLD.state IN ('confirmed','failed','cancelled') AND NEW.state='retired'))
BEGIN SELECT RAISE(ABORT,'immutable delivery evidence'); END;
CREATE TRIGGER delivery_receipt_immutable BEFORE UPDATE ON delivery_receipts
BEGIN SELECT RAISE(ABORT,'immutable delivery receipt'); END;
