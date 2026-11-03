package documents

import (
	"context"
	"errors"
	"time"
)

const cleanupRetirementQuery = `SELECT d.id,d.owner_id FROM jobs j INDEXED BY jobs_cleanup_retirement JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE ` + completedCleanupPredicate + ` AND j.runner_id IS NOT NULL AND j.started_at_ms IS NOT NULL AND j.heartbeat_at_ms>=j.started_at_ms AND j.heartbeat_at_ms<=? AND j.deadline_at_ms<=? AND NOT EXISTS(SELECT 1 FROM request_receipts r WHERE r.result_job_id=j.id AND r.expires_at_ms>?) ORDER BY j.heartbeat_at_ms,j.deadline_at_ms,j.id LIMIT ?`

// PurgeCompletedCleanup retires only content-free completed document/job status.
// The caller holds the exclusive worker guard. Retention starts at confirmed
// completion and cannot end before original job or linked receipt deadlines.
// Receipt hashes stay as gone-key tombstones; unknown commits are not retried.
func (s *Store) PurgeCompletedCleanup(ctx context.Context, limit int, retention time.Duration) (int, error) {
	if limit < 1 || limit > 100 || retention < time.Second || retention > 365*24*time.Hour {
		return 0, ErrInvalid
	}
	now := s.now().UnixMilli()
	if now < 0 {
		return 0, ErrInvalid
	}
	if now < retention.Milliseconds() {
		return 0, ctx.Err()
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return 0, storageOperationError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, cleanupRetirementQuery, now-retention.Milliseconds(), now, now, limit)
	if err != nil {
		return 0, storageOperationError(err)
	}
	type candidate struct{ document, owner string }
	var targets []candidate
	for rows.Next() {
		var target candidate
		if err = rows.Scan(&target.document, &target.owner); err != nil {
			rows.Close()
			return 0, storageOperationError(err)
		}
		targets = append(targets, target)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return 0, errors.Join(ErrStorage, err, closeErr)
	}
	for _, target := range targets {
		if _, err = tx.ExecContext(ctx, `DELETE FROM documents WHERE id=? AND owner_id=?`, target.document, target.owner); err != nil {
			return 0, storageOperationError(err)
		}
	}
	if err = s.commit(tx); err != nil {
		return 0, ErrCommitUncertain
	}
	return len(targets), nil
}
