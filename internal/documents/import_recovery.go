package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
)

// AbortUnstartedImports serializes with producer start and worker claim. Only
// pending (no file producer) and ready (closed producer) intakes can be released.
// Receiving intakes always require the actual producer's stop evidence.
func (s *Store) AbortUnstartedImports(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	count := 0
	err := s.withPrincipalSnapshot(ctx, func(principals *sql.Conn, tx *sql.Tx) error {
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT j.id,j.owner_id,j.state,j.created_at_ms,j.deadline_at_ms,d.expires_at_ms,d.state,j.input_revision,d.revision,i.state,i.artifact_id,a.state,a.byte_size,a.sha256 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN import_intakes i ON i.job_id=j.id AND i.owner_id=j.owner_id JOIN artifacts a ON a.id=i.artifact_id AND a.document_id=j.document_id AND a.owner_id=j.owner_id WHERE j.kind='import' AND j.state IN ('queued','cancelling') AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL AND i.state IN ('pending','ready') AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id) AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id) AND NOT EXISTS(SELECT 1 FROM job_artifacts x WHERE x.job_id=j.id AND x.artifact_id!=i.artifact_id) ORDER BY j.created_at_ms,j.id`)
		if err != nil {
			return storageOperationError(err)
		}
		defer rows.Close()
		type candidate struct{ job, owner, intake, artifact, state string }
		var selected []candidate
		for rows.Next() {
			var c candidate
			var created, deadline, expiry, revision, current, size int64
			var documentState, artifactState string
			var digest []byte
			if err = rows.Scan(&c.job, &c.owner, &c.state, &created, &deadline, &expiry, &documentState, &revision, &current, &c.intake, &c.artifact, &artifactState, &size, &digest); err != nil {
				return ErrStorage
			}
			var enabled int
			err = principals.QueryRowContext(ctx, "SELECT 1 FROM principals WHERE id=? AND disabled_at_ms IS NULL", c.owner).Scan(&enabled)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return storageOperationError(err)
			}
			if err == nil && c.state != "cancelling" && deadline > now && expiry > now && documentState == "processing" && revision == current {
				continue
			}
			if created > now {
				return ErrConflict
			}
			if c.intake == "pending" && (artifactState != "staging" || size != 0 || len(digest) != 0) || c.intake == "ready" && (artifactState != "ready" || size < 1 || len(digest) != 32) {
				return ErrStorage
			}
			selected = append(selected, c)
			if len(selected) == limit {
				break
			}
		}
		if err = rows.Err(); err != nil {
			return storageOperationError(err)
		}
		if err = rows.Close(); err != nil {
			return storageOperationError(err)
		}
		for _, c := range selected {
			if c.intake == "pending" {
				empty := sha256.Sum256(nil)
				if _, err = tx.ExecContext(ctx, "UPDATE artifacts SET state='ready',sha256=? WHERE id=? AND owner_id=?", empty[:], c.artifact, c.owner); err != nil {
					return ErrStorage
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) SELECT id,document_id,owner_id,? FROM jobs WHERE id=? AND owner_id=?`, c.artifact, c.job, c.owner); err != nil {
					return ErrStorage
				}
				if _, err = tx.ExecContext(ctx, "UPDATE import_intakes SET state='aborted',closed_at_ms=? WHERE job_id=? AND owner_id=?", now, c.job, c.owner); err != nil {
					return ErrStorage
				}
			}
			outcome := "failed"
			if c.state == "cancelling" {
				outcome = "cancelled"
			}
			if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,error_code='not_started',heartbeat_at_ms=? WHERE id=? AND owner_id=?", outcome, now, c.job, c.owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", c.job, c.owner); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, `UPDATE documents SET state=CASE WHEN expires_at_ms<=? THEN 'expired' ELSE 'failed' END WHERE id=(SELECT document_id FROM jobs WHERE id=? AND owner_id=?) AND owner_id=? AND state IN ('processing','cancelling') AND revision=(SELECT input_revision FROM jobs WHERE id=? AND owner_id=?)`, now, c.job, c.owner, c.owner, c.job, c.owner); err != nil {
				return ErrStorage
			}
		}
		count = len(selected)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
