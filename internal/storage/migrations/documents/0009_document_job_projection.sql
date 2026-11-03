-- Ordered per-document job metadata for persisted output preference projection.
-- Active-job lookup already has jobs_one_active_per_document.
CREATE INDEX jobs_document_revision ON jobs(document_id,owner_id,input_revision DESC,created_at_ms DESC,id DESC);
