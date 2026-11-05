package documents

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type CleanupFile struct {
	ArtifactID, RelativePath string
	Size                     int64
	SHA256                   [32]byte
}
type CleanupWork struct {
	Job       Job
	Files     []CleanupFile
	Remaining int64
}

// cleanupBarrier is per-document: other documents may have independent work.
// A terminal job with a retained reservation/lease is still unresolved.
func cleanupBarrier(ctx context.Context, tx *sql.Tx, document, owner string) error {
	var blocked int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE document_id=? AND owner_id=? AND kind!='cleanup' AND state IN ('queued','running','cancelling','reconciling')) OR EXISTS(SELECT 1 FROM reservations r JOIN jobs j ON j.id=r.job_id AND j.owner_id=r.owner_id WHERE j.document_id=? AND j.owner_id=?) OR EXISTS(SELECT 1 FROM device_leases l JOIN jobs j ON j.id=l.job_id WHERE j.document_id=? AND j.owner_id=?)`, document, owner, document, owner, document, owner).Scan(&blocked)
	if err != nil {
		return ErrStorage
	}
	if blocked != 0 {
		return ErrConflict
	}
	return nil
}

// ClaimCleanup is worker-internal and works for disabled/expired owners. Caller
// holds the local worker guard. It freezes a manifest only after every producer
// has terminalized and relinquished reservations/leases. It never removes files.
// After a lost commit acknowledgement, LoadCleanupWork reads back this same
// known job/runner; the claim must not be retried as a new execution.
func (s *Store) ClaimCleanup(ctx context.Context, owner, jobID, runnerID string) (Job, error) {
	return s.claimCleanup(ctx, owner, jobID, "", runnerID, 0)
}

// ClaimCleanupAttempt starts or resumes physical GC under the exclusive worker
// guard. previousRunner is empty only for a never-started queued job. Takeover
// compares the exact observed old runner; it never renews the same runner. An
// attempt deadline bounds IO independently of immutable content/job retention.
// Unknown commit is reconciled by loading this known job/new runner, not retry.
func (s *Store) ClaimCleanupAttempt(ctx context.Context, owner, jobID, previousRunner, runnerID string, budget time.Duration) (Job, error) {
	if budget < time.Second || budget > 15*time.Minute || (previousRunner != "" && (!id(previousRunner) || previousRunner == runnerID)) {
		return Job{}, ErrInvalid
	}
	return s.claimCleanup(ctx, owner, jobID, previousRunner, runnerID, budget)
}

func (s *Store) claimCleanup(ctx context.Context, owner, jobID, previousRunner, runnerID string, budget time.Duration) (Job, error) {
	if !id(owner) || !id(jobID) || !id(runnerID) {
		return Job{}, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, ErrStorage
	}
	defer tx.Rollback()
	var job Job
	now := s.now().UnixMilli()
	condition := `j.state='queued' AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL`
	values := []any{jobID, owner, now}
	if previousRunner != "" {
		condition = `j.state='running' AND j.runner_id=? AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=? AND j.heartbeat_at_ms IS NOT NULL`
		values = append(values, previousRunner, now)
	}
	err = tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.configuration_json,j.input_revision,j.deadline_at_ms FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='cleanup' AND j.created_at_ms<=? AND d.state IN ('deleting','expired') AND `+condition, values...).Scan(&job.ID, &job.DocumentID, &job.Kind, &job.Configuration, &job.Revision, &job.DeadlineAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrConflict
	}
	if err != nil {
		return Job{}, ErrStorage
	}
	if job.Configuration != `{"version":1}` || (budget == 0 && job.DeadlineAtMS <= now) {
		return Job{}, ErrConflict
	}
	if err = cleanupBarrier(ctx, tx, job.DocumentID, owner); err != nil {
		return Job{}, err
	}
	if previousRunner == "" {
		var invalid int
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE document_id=? AND owner_id=? AND (state NOT IN ('ready','deleting') OR sha256 IS NULL OR length(sha256)!=32)) OR EXISTS(SELECT 1 FROM cleanup_attempts WHERE job_id=?) OR EXISTS(SELECT 1 FROM cleanup_files WHERE job_id=?)`, job.DocumentID, owner, jobID, jobID).Scan(&invalid); err != nil {
			return Job{}, ErrStorage
		}
		if invalid != 0 {
			return Job{}, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='running',runner_id=?,started_at_ms=?,heartbeat_at_ms=? WHERE id=? AND owner_id=?`, runnerID, now, now, jobID, owner); err != nil {
			return Job{}, ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `UPDATE artifacts SET state='deleting' WHERE document_id=? AND owner_id=?`, job.DocumentID, owner); err != nil {
			return Job{}, ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cleanup_files(job_id,owner_id,artifact_id,relative_path,byte_size,sha256) SELECT ?,owner_id,id,relative_path,byte_size,sha256 FROM artifacts WHERE document_id=? AND owner_id=?`, jobID, job.DocumentID, owner); err != nil {
			return Job{}, ErrStorage
		}
	} else {
		if err = cleanupManifest(ctx, tx, job.DocumentID, owner, jobID); err != nil {
			return Job{}, err
		}
		var invalid int
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cleanup_attempts WHERE job_id=? AND (owner_id!=? OR runner_id!=? OR started_at_ms>?))`, jobID, owner, previousRunner, now).Scan(&invalid); err != nil {
			return Job{}, ErrStorage
		}
		if invalid != 0 {
			return Job{}, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET runner_id=?,heartbeat_at_ms=? WHERE id=? AND owner_id=?`, runnerID, now, jobID, owner); err != nil {
			return Job{}, ErrStorage
		}
	}
	if budget > 0 {
		deadline, ok := addMillis(now, budget)
		if !ok || now < 0 {
			return Job{}, ErrInvalid
		}
		var generation int64
		err = tx.QueryRowContext(ctx, `SELECT generation FROM cleanup_attempts WHERE job_id=?`, jobID).Scan(&generation)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO cleanup_attempts(job_id,owner_id,runner_id,generation,started_at_ms,deadline_at_ms) VALUES(?,?,?,1,?,?)`, jobID, owner, runnerID, now, deadline)
		} else if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE cleanup_attempts SET runner_id=?,generation=generation+1,started_at_ms=?,deadline_at_ms=? WHERE job_id=? AND owner_id=?`, runnerID, now, deadline, jobID, owner)
		}
		if err != nil {
			return Job{}, ErrStorage
		}
		job.DeadlineAtMS = deadline
	}
	if s.now().UnixMilli() >= job.DeadlineAtMS {
		return Job{}, ErrConflict
	}
	if err = s.commit(tx); err != nil {
		return Job{}, ErrCommitUncertain
	}
	return job, nil
}

// cleanupManifest requires exact coverage, including already acknowledged files.
func cleanupManifest(ctx context.Context, tx *sql.Tx, document, owner, job string) error {
	var mismatch int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts a WHERE a.document_id=? AND a.owner_id=? AND (a.state!='deleting' OR NOT EXISTS(SELECT 1 FROM cleanup_files f WHERE f.job_id=? AND f.artifact_id=a.id AND f.owner_id=a.owner_id AND f.relative_path=a.relative_path AND f.byte_size=a.byte_size AND f.sha256=a.sha256))) OR EXISTS(SELECT 1 FROM cleanup_files f WHERE f.job_id=? AND f.owner_id=? AND NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.id=f.artifact_id AND a.document_id=? AND a.owner_id=f.owner_id))`, document, owner, job, job, owner, document).Scan(&mismatch)
	if err != nil {
		return ErrStorage
	}
	if mismatch != 0 {
		return ErrConflict
	}
	return nil
}

// LoadCleanupWork returns bounded private manifest metadata; never serialize it
// to an API. No file operation is authorized unless this load succeeds.
func (s *Store) LoadCleanupWork(ctx context.Context, owner, jobID, runnerID string, limit int) (CleanupWork, error) {
	if !id(owner) || !id(jobID) || !id(runnerID) || limit < 1 || limit > 100 {
		return CleanupWork{}, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return CleanupWork{}, ErrStorage
	}
	defer tx.Rollback()
	var work CleanupWork
	now := s.now().UnixMilli()
	err = tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.configuration_json,j.input_revision,COALESCE(a.deadline_at_ms,j.deadline_at_ms) FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id LEFT JOIN cleanup_attempts a ON a.job_id=j.id AND a.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='cleanup' AND j.state='running' AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=? AND COALESCE(a.deadline_at_ms,j.deadline_at_ms)>? AND (a.job_id IS NULL OR (a.runner_id=j.runner_id AND a.started_at_ms<=?)) AND d.state IN ('deleting','expired')`, jobID, owner, runnerID, now, now, now).Scan(&work.Job.ID, &work.Job.DocumentID, &work.Job.Kind, &work.Job.Configuration, &work.Job.Revision, &work.Job.DeadlineAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return CleanupWork{}, ErrConflict
	}
	if err != nil {
		return CleanupWork{}, ErrStorage
	}
	if err = cleanupBarrier(ctx, tx, work.Job.DocumentID, owner); err != nil {
		return CleanupWork{}, err
	}
	if err = cleanupManifest(ctx, tx, work.Job.DocumentID, owner, jobID); err != nil {
		return CleanupWork{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM cleanup_files WHERE job_id=? AND owner_id=? AND state='pending'`, jobID, owner).Scan(&work.Remaining); err != nil {
		return CleanupWork{}, ErrStorage
	}
	rows, err := tx.QueryContext(ctx, `SELECT artifact_id,relative_path,byte_size,sha256 FROM cleanup_files WHERE job_id=? AND owner_id=? AND state='pending' ORDER BY artifact_id LIMIT ?`, jobID, owner, limit)
	if err != nil {
		return CleanupWork{}, ErrStorage
	}
	for rows.Next() {
		var file CleanupFile
		var digest []byte
		if err = rows.Scan(&file.ArtifactID, &file.RelativePath, &file.Size, &digest); err != nil {
			rows.Close()
			return CleanupWork{}, ErrStorage
		}
		if !id(file.ArtifactID) || !localPath(file.RelativePath) || file.Size < 0 || len(digest) != 32 {
			rows.Close()
			return CleanupWork{}, ErrStorage
		}
		copy(file.SHA256[:], digest)
		work.Files = append(work.Files, file)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return CleanupWork{}, ErrStorage
	}
	if s.now().UnixMilli() >= work.Job.DeadlineAtMS {
		return CleanupWork{}, ErrConflict
	}
	if err = s.commit(tx); err != nil {
		return CleanupWork{}, ErrCommitUncertain
	}
	return work, nil
}

// AbortUnstartedRenders releases only withdrawn never-claimed render reservations.
// Null start/runner/heartbeat plus absent produced files and leases prove that no
// conforming executor owns output. Started/uncertain work remains untouched.
func (s *Store) AbortUnstartedRenders(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return 0, storageOperationError(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT j.id,j.owner_id FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.kind='render' AND j.state IN ('queued','cancelling') AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL AND d.state IN ('deleting','expired') AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id) AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id) ORDER BY j.created_at_ms,j.id LIMIT ?`, limit)
	if err != nil {
		return 0, storageOperationError(err)
	}
	type candidate struct{ id, owner string }
	var jobs []candidate
	for rows.Next() {
		var job candidate
		if err = rows.Scan(&job.id, &job.owner); err != nil {
			rows.Close()
			return 0, storageOperationError(err)
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil && closeErr != nil {
		return 0, errors.Join(ErrStorage, err, closeErr)
	}
	if err != nil {
		return 0, storageOperationError(err)
	}
	if closeErr != nil {
		return 0, storageOperationError(closeErr)
	}
	now := s.now().UnixMilli()
	for _, job := range jobs {
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state='cancelled',error_code='not_started',heartbeat_at_ms=? WHERE id=? AND owner_id=?`, now, job.id, job.owner); err != nil {
			return 0, storageOperationError(err)
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM reservations WHERE job_id=? AND owner_id=?`, job.id, job.owner); err != nil {
			return 0, storageOperationError(err)
		}
	}
	if err = s.commit(tx); err != nil {
		return 0, ErrCommitUncertain
	}
	return len(jobs), nil
}

// NextCleanup selects eligible queued or interrupted running cleanup. Claim must
// recheck every predicate transactionally; this observation is not file authority.
type CleanupCandidate struct{ OwnerID, JobID, PreviousRunner string }

func (s *Store) NextCleanup(ctx context.Context) (CleanupCandidate, error) {
	var candidate CleanupCandidate
	err := s.documents.QueryRowContext(ctx, `SELECT j.id,j.owner_id,COALESCE(j.runner_id,'') FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.kind='cleanup' AND ((j.state='queued' AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL) OR (j.state='running' AND j.runner_id IS NOT NULL AND j.started_at_ms IS NOT NULL AND j.heartbeat_at_ms IS NOT NULL)) AND d.state IN ('deleting','expired') AND NOT EXISTS(SELECT 1 FROM jobs b WHERE b.document_id=d.id AND b.kind!='cleanup' AND b.state IN ('queued','running','cancelling','reconciling')) AND NOT EXISTS(SELECT 1 FROM reservations r JOIN jobs b ON b.id=r.job_id AND b.owner_id=r.owner_id WHERE b.document_id=d.id) AND NOT EXISTS(SELECT 1 FROM device_leases l JOIN jobs b ON b.id=l.job_id WHERE b.document_id=d.id) AND NOT EXISTS(SELECT 1 FROM artifacts a WHERE a.document_id=d.id AND (a.state NOT IN ('ready','deleting') OR a.sha256 IS NULL OR length(a.sha256)!=32)) ORDER BY j.created_at_ms,j.id LIMIT 1`).Scan(&candidate.JobID, &candidate.OwnerID, &candidate.PreviousRunner)
	if errors.Is(err, sql.ErrNoRows) {
		return CleanupCandidate{}, ErrNoWork
	}
	if err != nil {
		return CleanupCandidate{}, storageOperationError(err)
	}
	return candidate, nil
}

// AbortWithdrawnUnstartedScans discovers withdrawn or cancelled unstarted scans.
// Caller holds the worker guard. The original boot identity is retained: this
// never adopts or executes an old capture. AbortUnstartedScan rechecks immutable
// start markers, snapshot and held lease in its transaction. Unknown/started work
// remains a reconciliation barrier; no stop/idle observation is manufactured.
func (s *Store) AbortWithdrawnUnstartedScans(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	rows, err := s.documents.QueryContext(ctx, `SELECT j.id,j.owner_id,j.runner_id FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id WHERE j.kind='scan' AND j.state IN ('queued','cancelling') AND j.runner_id IS NOT NULL AND j.started_at_ms IS NULL AND j.heartbeat_at_ms IS NULL AND (d.state IN ('deleting','expired') OR j.state='cancelling') AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id) AND EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id AND l.state='held' AND l.device_id=json_extract(j.configuration_json,'$.device_id') AND l.epoch=json_extract(j.configuration_json,'$.lease_epoch')) ORDER BY j.created_at_ms,j.id LIMIT ?`, limit)
	if err != nil {
		return 0, storageOperationError(err)
	}
	type candidate struct{ job, owner, boot string }
	var jobs []candidate
	for rows.Next() {
		var job candidate
		if err = rows.Scan(&job.job, &job.owner, &job.boot); err != nil {
			rows.Close()
			return 0, storageOperationError(err)
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return 0, errors.Join(ErrStorage, err, closeErr)
	}
	for i, job := range jobs {
		if err = s.AbortUnstartedScan(ctx, job.owner, job.job, job.boot); err != nil {
			return i, err
		}
	}
	return len(jobs), nil
}
