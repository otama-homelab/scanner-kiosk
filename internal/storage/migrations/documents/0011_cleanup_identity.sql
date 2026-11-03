-- One durable cleanup identity per document, including retries/recovery.
CREATE UNIQUE INDEX jobs_one_cleanup_per_document ON jobs(document_id) WHERE kind='cleanup';
