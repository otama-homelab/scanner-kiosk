-- Local mirror for referential integrity; authorization still checks the durable principal store.
CREATE TABLE owners (id TEXT PRIMARY KEY CHECK(length(id)>=16)) STRICT;
CREATE TABLE documents (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
 purpose TEXT NOT NULL CHECK(purpose IN ('document','photo')),
 state TEXT NOT NULL CHECK(state IN ('scanning','review','processing','ready','failed','cancelling','expired','deleting')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms),
 UNIQUE(id,owner_id),
 UNIQUE(id,owner_id,revision)
) STRICT;
CREATE INDEX documents_owner_expiry ON documents(owner_id,expires_at_ms);
CREATE TRIGGER document_identity_immutable BEFORE UPDATE ON documents
WHEN NEW.id!=OLD.id OR NEW.owner_id!=OLD.owner_id OR NEW.purpose!=OLD.purpose OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.expires_at_ms!=OLD.expires_at_ms
BEGIN SELECT RAISE(ABORT,'immutable document identity or retention'); END;
CREATE TRIGGER document_revision_monotonic BEFORE UPDATE OF revision ON documents
WHEN NEW.revision<OLD.revision OR NEW.revision>OLD.revision+1
BEGIN SELECT RAISE(ABORT,'invalid document revision'); END;
CREATE TABLE artifacts (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('original','thumbnail','output','ocr_text','intermediate')),
 state TEXT NOT NULL CHECK(state IN ('staging','ready','deleting')),
 relative_path TEXT NOT NULL CHECK(length(relative_path)>0 AND relative_path NOT LIKE '/%' AND instr(relative_path,'..')=0 AND instr(relative_path,char(92))=0 AND instr(relative_path,char(0))=0),
 media_type TEXT NOT NULL,
 byte_size INTEGER NOT NULL CHECK(byte_size>=0),
 sha256 BLOB CHECK(sha256 IS NULL OR length(sha256)=32),
 input_revision INTEGER NOT NULL CHECK(input_revision>=1),
 created_at_ms INTEGER NOT NULL,
 CHECK(state!='ready' OR sha256 IS NOT NULL),
 UNIQUE(id,document_id,owner_id),
 UNIQUE(relative_path),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE TABLE pages (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal>=0),
 original_artifact_id TEXT NOT NULL,
 rotation INTEGER NOT NULL DEFAULT 0 CHECK(rotation IN (0,90,180,270)),
 capture_size TEXT NOT NULL CHECK(capture_size IN ('a4','a5','a6','letter','photo10x15')),
 capture_color TEXT NOT NULL CHECK(capture_color IN ('color','gray')),
 capture_dpi INTEGER NOT NULL CHECK(capture_dpi IN (75,100,150,200,300,600)),
 UNIQUE(document_id,ordinal),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE,
 FOREIGN KEY(original_artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
CREATE TRIGGER page_original_ready BEFORE INSERT ON pages
WHEN NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.original_artifact_id AND kind='original' AND state='ready')
BEGIN SELECT RAISE(ABORT,'original artifact is not ready'); END;
CREATE TRIGGER page_capture_immutable BEFORE UPDATE ON pages
WHEN NEW.id!=OLD.id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.original_artifact_id!=OLD.original_artifact_id OR NEW.capture_size!=OLD.capture_size OR NEW.capture_color!=OLD.capture_color OR NEW.capture_dpi!=OLD.capture_dpi
BEGIN SELECT RAISE(ABORT,'immutable captured page'); END;
CREATE TABLE jobs (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 document_id TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('scan','render','ocr','cleanup')),
 state TEXT NOT NULL CHECK(state IN ('queued','running','cancelling','cancelled','succeeded','failed','interrupted','reconciling')),
 input_revision INTEGER NOT NULL CHECK(input_revision>=1),
 configuration_json TEXT NOT NULL CHECK(json_valid(configuration_json) AND json_type(configuration_json)='object'),
 created_at_ms INTEGER NOT NULL,
 deadline_at_ms INTEGER NOT NULL CHECK(deadline_at_ms>created_at_ms),
 runner_id TEXT,
 heartbeat_at_ms INTEGER,
 error_code TEXT,
 UNIQUE(id,owner_id),
 FOREIGN KEY(document_id,owner_id) REFERENCES documents(id,owner_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX jobs_work ON jobs(state,created_at_ms);
CREATE UNIQUE INDEX jobs_one_active_per_document ON jobs(document_id)
 WHERE kind!='cleanup' AND state IN ('queued','running','cancelling','reconciling');
CREATE TRIGGER job_deadline_within_retention BEFORE INSERT ON jobs
WHEN NEW.kind!='cleanup' AND NEW.deadline_at_ms>(SELECT expires_at_ms FROM documents WHERE id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'job exceeds retention'); END;
CREATE TRIGGER job_snapshot_immutable BEFORE UPDATE ON jobs
WHEN NEW.id!=OLD.id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.kind!=OLD.kind OR NEW.input_revision!=OLD.input_revision OR NEW.configuration_json!=OLD.configuration_json OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.deadline_at_ms!=OLD.deadline_at_ms
BEGIN SELECT RAISE(ABORT,'immutable job input'); END;
CREATE TABLE published_outputs (
 document_id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL,
 revision INTEGER NOT NULL,
 artifact_id TEXT NOT NULL,
 FOREIGN KEY(document_id,owner_id,revision) REFERENCES documents(id,owner_id,revision) ON DELETE CASCADE ON UPDATE RESTRICT,
 FOREIGN KEY(artifact_id,document_id,owner_id) REFERENCES artifacts(id,document_id,owner_id) ON DELETE RESTRICT
) STRICT;
CREATE TRIGGER published_output_ready BEFORE INSERT ON published_outputs
WHEN NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.artifact_id AND kind='output' AND state='ready' AND input_revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'output artifact is not ready at this revision'); END;
CREATE TABLE device_leases (
 device_id TEXT PRIMARY KEY,
 epoch INTEGER NOT NULL CHECK(epoch>=1),
 job_id TEXT UNIQUE REFERENCES jobs(id) ON DELETE RESTRICT,
 state TEXT NOT NULL CHECK(state IN ('idle','held','reconciling')),
 CHECK((job_id IS NULL AND state='idle') OR (job_id IS NOT NULL AND state!='idle'))
) STRICT;
CREATE TRIGGER lease_scan_job BEFORE INSERT ON device_leases
WHEN NEW.job_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM jobs WHERE id=NEW.job_id AND kind='scan' AND state IN ('queued','running','cancelling','reconciling'))
BEGIN SELECT RAISE(ABORT,'invalid scanner lease job'); END;
CREATE TABLE reservations (
 job_id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL,
 byte_size INTEGER NOT NULL CHECK(byte_size>=0),
 FOREIGN KEY(job_id,owner_id) REFERENCES jobs(id,owner_id) ON DELETE CASCADE
) STRICT;
-- Receipts carry no document text or filenames. Null result means gone, never a new scan.
CREATE TABLE request_receipts (
 owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
 key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
 operation TEXT NOT NULL,
 request_hash BLOB NOT NULL CHECK(length(request_hash)=32),
 result_job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
 expires_at_ms INTEGER NOT NULL,
 PRIMARY KEY(owner_id,key_hash)
) STRICT;
CREATE INDEX request_receipts_expiry ON request_receipts(expires_at_ms);
CREATE TRIGGER request_result_owner BEFORE INSERT ON request_receipts
WHEN NEW.result_job_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM jobs WHERE id=NEW.result_job_id AND owner_id=NEW.owner_id)
BEGIN SELECT RAISE(ABORT,'request result owner mismatch'); END;
CREATE TRIGGER artifact_identity_immutable BEFORE UPDATE ON artifacts
WHEN NEW.id!=OLD.id OR NEW.document_id!=OLD.document_id OR NEW.owner_id!=OLD.owner_id OR NEW.kind!=OLD.kind OR NEW.relative_path!=OLD.relative_path OR NEW.input_revision!=OLD.input_revision OR NEW.created_at_ms!=OLD.created_at_ms
 OR (OLD.state!='staging' AND (NEW.byte_size!=OLD.byte_size OR NEW.sha256 IS NOT OLD.sha256 OR NEW.media_type!=OLD.media_type))
 OR (OLD.state='ready' AND NEW.state='staging')
 OR (OLD.state='deleting' AND NEW.state!='deleting')
BEGIN SELECT RAISE(ABORT,'immutable artifact'); END;
CREATE TRIGGER published_output_update_ready BEFORE UPDATE ON published_outputs
WHEN NOT EXISTS(SELECT 1 FROM artifacts WHERE id=NEW.artifact_id AND kind='output' AND state='ready' AND input_revision=NEW.revision)
BEGIN SELECT RAISE(ABORT,'output artifact is not ready at this revision'); END;
CREATE TRIGGER lease_update_scan_job BEFORE UPDATE ON device_leases
WHEN NEW.job_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM jobs WHERE id=NEW.job_id AND kind='scan' AND state IN ('queued','running','cancelling','reconciling'))
BEGIN SELECT RAISE(ABORT,'invalid scanner lease job'); END;
CREATE TRIGGER lease_epoch_monotonic BEFORE UPDATE ON device_leases
WHEN NEW.device_id!=OLD.device_id OR NEW.epoch<OLD.epoch OR NEW.epoch>OLD.epoch+1 OR (NEW.job_id IS NOT OLD.job_id AND NEW.job_id IS NOT NULL AND NEW.epoch!=OLD.epoch+1)
BEGIN SELECT RAISE(ABORT,'invalid lease epoch'); END;
CREATE TRIGGER request_receipt_immutable BEFORE UPDATE ON request_receipts
WHEN NEW.owner_id!=OLD.owner_id OR NEW.key_hash!=OLD.key_hash OR NEW.operation!=OLD.operation OR NEW.request_hash!=OLD.request_hash OR NEW.expires_at_ms!=OLD.expires_at_ms OR (NEW.result_job_id IS NOT OLD.result_job_id AND NEW.result_job_id IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'immutable request receipt'); END;
CREATE TRIGGER photo_single_page BEFORE INSERT ON pages
WHEN (SELECT purpose FROM documents WHERE id=NEW.document_id)='photo' AND EXISTS(SELECT 1 FROM pages WHERE document_id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'photo already has a page'); END;

CREATE TRIGGER job_input_current_at_admission BEFORE INSERT ON jobs
WHEN NEW.kind!='cleanup' AND NEW.input_revision!=(SELECT revision FROM documents WHERE id=NEW.document_id)
BEGIN SELECT RAISE(ABORT,'job input is not current'); END;
