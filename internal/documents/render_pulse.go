package documents

import (
	"context"
	"database/sql"
)

// PulseRender renews only this runner's still-authorized running render. Failure
// means stop processing; it never proves process exit or releases resources.
func (s *Store) PulseRender(ctx context.Context, owner, jobID, runnerID string) error {
	if !id(jobID) || !id(runnerID) {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=? WHERE id=? AND owner_id=? AND runner_id=? AND kind='render' AND state='running' AND started_at_ms IS NOT NULL AND deadline_at_ms>?
 AND EXISTS(SELECT 1 FROM documents d WHERE d.id=jobs.document_id AND d.owner_id=jobs.owner_id AND d.revision=jobs.input_revision AND d.state='processing' AND d.expires_at_ms>?)
 AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=jobs.id AND r.owner_id=jobs.owner_id)`, now, jobID, owner, runnerID, now, now)
		if err != nil {
			return ErrStorage
		}
		n, err := result.RowsAffected()
		if err != nil {
			return ErrStorage
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}
