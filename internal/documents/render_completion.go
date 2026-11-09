package documents

import (
	"context"
	"database/sql"
	"errors"
)

// RenderStop is trusted worker evidence, never client input. The coordinator must
// have ended all owned processing and removed unpublished work before reporting.
type RenderStop struct {
	RunnerID                                 string
	ProcessStoppedAtMS, WorkspaceCleanedAtMS int64
}

// FinishStoppedRender terminalizes claimed render work after confirmed cleanup.
// It is worker-internal authority and remains usable for disabled/expired owners.
// It does not stop work or remove files; uncertain publication must resolve first.
func (s *Store) FinishStoppedRender(ctx context.Context, owner, jobID string, stop RenderStop, outcome string) error {
	if !id(owner) || !id(jobID) || !id(stop.RunnerID) || (outcome != "failed" && outcome != "interrupted") {
		return ErrInvalid
	}
	tx, err := s.documents.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var jobState, document, documentState string
	var started, jobRevision, docRevision, expiry int64
	err = tx.QueryRowContext(ctx, `SELECT j.state,j.document_id,j.started_at_ms,j.input_revision,d.revision,d.expires_at_ms,d.state
 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
 WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='render' AND j.state IN ('running','cancelling','reconciling') AND j.started_at_ms IS NOT NULL
 AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id)
 AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id)
 AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)`, jobID, owner, stop.RunnerID).Scan(&jobState, &document, &started, &jobRevision, &docRevision, &expiry, &documentState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return ErrStorage
	}
	now := s.now().UnixMilli()
	if stop.ProcessStoppedAtMS < started || stop.ProcessStoppedAtMS <= 0 || stop.WorkspaceCleanedAtMS < stop.ProcessStoppedAtMS || stop.WorkspaceCleanedAtMS > now {
		return ErrConflict
	}
	terminal := outcome
	if jobState == "cancelling" {
		terminal = "cancelled"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,error_code=?,heartbeat_at_ms=? WHERE id=? AND owner_id=?", terminal, terminal, now, jobID, owner); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM reservations WHERE job_id=? AND owner_id=?", jobID, owner); err != nil {
		return ErrStorage
	}
	if documentState != "deleting" && documentState != "expired" {
		if expiry <= now {
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state='expired' WHERE id=? AND owner_id=?", document, owner); err != nil {
				return ErrStorage
			}
		} else if jobRevision == docRevision && (documentState == "processing" || documentState == "cancelling") {
			var pages int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE document_id=? AND owner_id=?", document, owner).Scan(&pages); err != nil {
				return ErrStorage
			}
			state := "failed"
			if pages > 0 {
				state = "review"
			}
			var current int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM published_outputs p JOIN artifacts a ON a.id=p.artifact_id AND a.document_id=p.document_id AND a.owner_id=p.owner_id WHERE p.document_id=? AND p.owner_id=? AND p.revision=? AND a.state='ready' AND a.kind='output' AND (a.input_revision=p.revision OR EXISTS(SELECT 1 FROM ocr_results r WHERE r.document_id=p.document_id AND r.owner_id=p.owner_id AND r.output_artifact_id=a.id AND r.input_revision=a.input_revision AND r.state='adopted' AND r.adopted_revision=p.revision))`, document, owner, docRevision).Scan(&current); err != nil {
				return ErrStorage
			}
			if current == 1 {
				state = "ready"
			}
			if _, err = tx.ExecContext(ctx, "UPDATE documents SET state=? WHERE id=? AND owner_id=?", state, document, owner); err != nil {
				return ErrStorage
			}
		}
	}
	if err = s.commit(tx); err != nil {
		return ErrCommitUncertain
	}
	return nil
}

// RenderStoppedRecorded exposes only internal reconciliation metadata. It must
// never be used to authorize an endpoint or to infer that processes have stopped.
func (s *Store) RenderStoppedRecorded(ctx context.Context, owner, jobID, runnerID string) (bool, error) {
	if !id(owner) || !id(jobID) || !id(runnerID) {
		return false, ErrInvalid
	}
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT 1 FROM jobs j WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='render' AND j.state IN ('failed','cancelled','interrupted')
 AND NOT EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id)
 AND NOT EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id)
 AND NOT EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)`, jobID, owner, runnerID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	return true, nil
}
