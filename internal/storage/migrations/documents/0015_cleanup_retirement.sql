-- Bounded completed-status retirement and receipt-reference checks. Key hashes
-- remain as replay tombstones: deleting them could re-execute an old scan key.
CREATE INDEX jobs_cleanup_retirement ON jobs(heartbeat_at_ms,deadline_at_ms,id)
 WHERE kind='cleanup' AND state='succeeded';
CREATE INDEX request_receipts_result_expiry ON request_receipts(result_job_id,expires_at_ms);
CREATE INDEX edit_results_document ON edit_results(document_id);
