-- Completion reconciliation/document-slot checks must not scan every artifact.
CREATE INDEX artifacts_document_owner ON artifacts(document_id,owner_id);
