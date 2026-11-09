package documents

import (
	"context"
	"database/sql"
	"errors"
)

// CleanupRemoval is trusted post-unlink+directory-sync worker evidence. It must
// be bound to the manifest input; no endpoint accepts it from a browser.
type CleanupRemoval struct {
	ArtifactID  string
	Size        int64
	SHA256      [32]byte
	RemovedAtMS int64
}

func cleanupIdentity(ctx context.Context, tx *sql.Tx, owner, job, runner string, now int64) (string, int64, error) {
	var document string
	var started int64
	err := tx.QueryRowContext(ctx, `SELECT j.document_id,COALESCE(a.started_at_ms,j.started_at_ms) FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id LEFT JOIN cleanup_attempts a ON a.job_id=j.id AND a.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='cleanup' AND j.state='running' AND j.started_at_ms IS NOT NULL AND (a.job_id IS NULL OR a.runner_id=j.runner_id) AND d.state IN ('deleting','expired')`, job, owner, runner).Scan(&document, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrConflict
	}
	if err != nil {
		return "", 0, ErrStorage
	}
	if err = cleanupBarrier(ctx, tx, document, owner, now); err != nil {
		return "", 0, err
	}
	return document, started, nil
}

// RecordCleanupRemoval advances durable progress only. Artifacts remain charged
// until FinishCleanup confirms every manifest file; physical IO stays outside DB.
// A lost acknowledgement is resolved by CleanupRemovalRecorded before retry.
func (s *Store) RecordCleanupRemoval(ctx context.Context, owner, job, runner string, proof CleanupRemoval) error {
	if !id(owner) || !id(job) || !id(runner) || !id(proof.ArtifactID) || proof.Size < 0 || proof.RemovedAtMS <= 0 {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	_, started, err := cleanupIdentity(ctx, tx, owner, job, runner, s.now().UnixMilli())
	if err != nil {
		return err
	}
	if proof.RemovedAtMS < started || proof.RemovedAtMS > s.now().UnixMilli() {
		return ErrConflict
	}
	var size int64
	var digest []byte
	var state string
	err = tx.QueryRowContext(ctx, `SELECT byte_size,sha256,state FROM cleanup_files WHERE job_id=? AND owner_id=? AND artifact_id=?`, job, owner, proof.ArtifactID).Scan(&size, &digest, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrStorage
	}
	if size != proof.Size || !sameHash(digest, proof.SHA256) {
		return ErrConflict
	}
	if state == "pending" {
		if _, err = tx.ExecContext(ctx, `UPDATE cleanup_files SET state='deleted',deleted_at_ms=? WHERE job_id=? AND owner_id=? AND artifact_id=? AND state='pending'`, proof.RemovedAtMS, job, owner, proof.ArtifactID); err != nil {
			return ErrStorage
		}
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}

func (s *Store) CleanupRemovalRecorded(ctx context.Context, owner, job, runner, artifact string) (bool, error) {
	if !id(owner) || !id(job) || !id(runner) || !id(artifact) {
		return false, ErrInvalid
	}
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM cleanup_files f JOIN jobs j ON j.id=f.job_id AND j.owner_id=f.owner_id WHERE f.job_id=? AND f.owner_id=? AND f.artifact_id=? AND f.state='deleted' AND j.kind='cleanup' AND j.runner_id=? AND j.state='running'`, job, owner, artifact, runner).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}

// FinishCleanup removes metadata/quota only after all files have durable removal
// acknowledgement. The minimal cleanup job/document tombstone and request hashes
// remain for receipt/status retention; no file paths, pages or payloads survive.
// Reports of already completed physical work remain usable after the IO deadline.
func (s *Store) FinishCleanup(ctx context.Context, owner, job, runner string) error {
	if !id(owner) || !id(job) || !id(runner) {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	document, _, err := cleanupIdentity(ctx, tx, owner, job, runner, s.now().UnixMilli())
	if err != nil {
		return err
	}
	var invalid int
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cleanup_files WHERE job_id=? AND owner_id=? AND state!='deleted') OR EXISTS(SELECT 1 FROM artifacts a WHERE a.document_id=? AND a.owner_id=? AND (a.state!='deleting' OR NOT EXISTS(SELECT 1 FROM cleanup_files f WHERE f.job_id=? AND f.owner_id=a.owner_id AND f.artifact_id=a.id AND f.relative_path=a.relative_path AND f.byte_size=a.byte_size AND f.sha256=a.sha256 AND f.state='deleted'))) OR EXISTS(SELECT 1 FROM cleanup_files f WHERE f.job_id=? AND f.owner_id=? AND NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.id=f.artifact_id AND a.document_id=? AND a.owner_id=f.owner_id))`, job, owner, document, owner, job, job, owner, document).Scan(&invalid)
	if err != nil {
		return ErrStorage
	}
	if invalid != 0 {
		return ErrConflict
	}
	for _, query := range []string{`DELETE FROM published_outputs WHERE document_id=? AND owner_id=?`, `DELETE FROM pages WHERE document_id=? AND owner_id=?`, `DELETE FROM page_thumbnails WHERE document_id=? AND owner_id=?`, `DELETE FROM edit_results WHERE document_id=? AND owner_id=?`, `DELETE FROM jobs WHERE document_id=? AND owner_id=? AND kind!='cleanup'`, `DELETE FROM artifacts WHERE document_id=? AND owner_id=?`} {
		if _, err = tx.ExecContext(ctx, query, document, owner); err != nil {
			return ErrStorage
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM cleanup_files WHERE job_id=? AND owner_id=?`, job, owner); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM cleanup_attempts WHERE job_id=? AND owner_id=?`, job, owner); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='succeeded',heartbeat_at_ms=?,error_code=NULL WHERE id=? AND owner_id=?`, s.now().UnixMilli(), job, owner); err != nil {
		return ErrStorage
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}

// completedCleanupPredicate is shared by completion reconciliation and admission.
// A status flag alone never releases a document slot while payload/work remains.
// It expects aliases j (the cleanup job) and d (its document).
const completedCleanupPredicate = `j.kind='cleanup' AND j.state='succeeded' AND d.state IN ('deleting','expired') AND NOT EXISTS(SELECT 1 FROM artifacts WHERE document_id=d.id) AND NOT EXISTS(SELECT 1 FROM pages WHERE document_id=d.id) AND NOT EXISTS(SELECT 1 FROM cleanup_files WHERE job_id=j.id) AND NOT EXISTS(SELECT 1 FROM cleanup_attempts WHERE job_id=j.id) AND NOT EXISTS(SELECT 1 FROM edit_results WHERE document_id=d.id) AND NOT EXISTS(SELECT 1 FROM jobs b WHERE b.document_id=d.id AND b.kind!='cleanup') AND NOT EXISTS(SELECT 1 FROM reservations r JOIN jobs b ON b.id=r.job_id WHERE b.document_id=d.id) AND NOT EXISTS(SELECT 1 FROM device_leases l JOIN jobs b ON b.id=l.job_id WHERE b.document_id=d.id)`

// CleanupFinishedRecorded is private content-free reconciliation metadata, usable
// after principal disable/TTL. It does not authorize any further file deletion.
func (s *Store) CleanupFinishedRecorded(ctx context.Context, owner, job, runner string) (bool, error) {
	if !id(owner) || !id(job) || !id(runner) {
		return false, ErrInvalid
	}
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND `+completedCleanupPredicate, job, owner, runner).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}
