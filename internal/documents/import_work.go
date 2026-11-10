package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type ClaimedImport struct {
	OwnerID string
	Job     Job
}
type ImportWork struct {
	Job          Job
	Snapshot     ImportSnapshot
	Input        ArtifactRecord
	ReserveBytes int64
}

// ClaimNextImport accepts only sealed input with no previous worker execution.
// The caller owns the worker guard; unknown heavy work blocks a new claim.
func (s *Store) ClaimNextImport(ctx context.Context, runner string) (ClaimedImport, error) {
	if !id(runner) {
		return ClaimedImport{}, ErrInvalid
	}
	var out ClaimedImport
	err := s.withPrincipalSnapshot(ctx, func(principals *sql.Conn, tx *sql.Tx) error {
		var busy int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind!='cleanup' AND (state IN ('running','cancelling','reconciling') OR (state='queued' AND (runner_id IS NOT NULL OR started_at_ms IS NOT NULL)))) OR EXISTS(SELECT 1 FROM device_leases WHERE state!='idle')`).Scan(&busy); err != nil {
			return storageOperationError(err)
		}
		if busy != 0 {
			return ErrNoWork
		}
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT j.id,j.owner_id FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN import_intakes i ON i.job_id=j.id AND i.owner_id=j.owner_id AND i.document_id=j.document_id WHERE j.kind='import' AND j.state='queued' AND j.runner_id IS NULL AND j.started_at_ms IS NULL AND j.created_at_ms<=? AND j.deadline_at_ms>? AND d.state='processing' AND d.revision=j.input_revision AND d.expires_at_ms>? AND i.state='ready' AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id AND r.byte_size>0) ORDER BY j.created_at_ms,j.id`, now, now, now)
		if err != nil {
			return storageOperationError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var job, owner string
			if err = rows.Scan(&job, &owner); err != nil {
				return ErrStorage
			}
			var enabled int
			err = principals.QueryRowContext(ctx, "SELECT 1 FROM principals WHERE id=? AND disabled_at_ms IS NULL", owner).Scan(&enabled)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return storageOperationError(err)
			}
			if err = rows.Close(); err != nil {
				return ErrStorage
			}
			if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state='running',runner_id=?,started_at_ms=?,heartbeat_at_ms=? WHERE id=? AND owner_id=? AND kind='import' AND state='queued' AND runner_id IS NULL AND started_at_ms IS NULL", runner, now, now, job, owner); err != nil {
				return ErrStorage
			}
			work, err := s.loadImportWork(ctx, tx, owner, job, runner)
			if err != nil {
				return err
			}
			if err = s.checkExecutionCapacity(ctx, tx, owner, job); err != nil {
				return err
			}
			out = ClaimedImport{OwnerID: owner, Job: work.Job}
			return nil
		}
		if err = rows.Err(); err != nil {
			return storageOperationError(err)
		}
		return ErrNoWork
	})
	if err != nil {
		return ClaimedImport{}, err
	}
	return out, nil
}
func (s *Store) LoadImportWork(ctx context.Context, owner, job, runner string) (ImportWork, error) {
	if !id(job) || !id(runner) {
		return ImportWork{}, ErrInvalid
	}
	var out ImportWork
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out, err = s.loadImportWork(ctx, tx, owner, job, runner)
		return err
	})
	if err != nil {
		return ImportWork{}, err
	}
	return out, nil
}
func (s *Store) loadImportWork(ctx context.Context, tx *sql.Tx, owner, job, runner string) (ImportWork, error) {
	var out ImportWork
	var digest []byte
	var started int64
	now := s.now().UnixMilli()
	err := tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.configuration_json,j.input_revision,j.deadline_at_ms,j.started_at_ms,r.byte_size,a.id,a.relative_path,a.media_type,a.byte_size,a.sha256 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id JOIN import_intakes i ON i.job_id=j.id AND i.owner_id=j.owner_id AND i.document_id=j.document_id JOIN artifacts a ON a.id=i.artifact_id AND a.document_id=i.document_id AND a.owner_id=i.owner_id JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id WHERE j.id=? AND j.owner_id=? AND j.kind='import' AND j.state='running' AND j.runner_id=? AND j.started_at_ms IS NOT NULL AND d.state='processing' AND d.revision=j.input_revision AND j.deadline_at_ms>? AND d.expires_at_ms>? AND i.state='ready' AND a.kind='original' AND a.state='ready' AND EXISTS(SELECT 1 FROM job_artifacts x WHERE x.job_id=j.id AND x.owner_id=j.owner_id AND x.artifact_id=i.artifact_id) AND NOT EXISTS(SELECT 1 FROM job_artifacts x WHERE x.job_id=j.id AND x.artifact_id!=i.artifact_id)`, job, owner, runner, now, now).Scan(&out.Job.ID, &out.Job.DocumentID, &out.Job.Kind, &out.Job.Configuration, &out.Job.Revision, &out.Job.DeadlineAtMS, &started, &out.ReserveBytes, &out.Input.ID, &out.Input.RelativePath, &out.Input.MediaType, &out.Input.Size, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportWork{}, ErrConflict
	}
	if err != nil {
		return ImportWork{}, storageOperationError(err)
	}
	if len(out.Job.Configuration) > 65536 || started > now || started < 0 || out.ReserveBytes < 1 || !id(out.Input.ID) || !localPath(out.Input.RelativePath) || !validImportMedia(out.Input.MediaType) || out.Input.Size < 1 || out.Input.Size > 1<<30 || len(digest) != 32 {
		return ImportWork{}, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(out.Job.Configuration))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out.Snapshot) != nil || decoder.Decode(new(any)) != io.EOF || out.Snapshot.Version != 1 || importMedia(out.Snapshot.MediaType) != out.Input.MediaType || out.Snapshot.InputArtifactID != out.Input.ID || out.Snapshot.InputBytes != out.Input.Size || !sameHash(digest, out.Snapshot.SHA256) || out.Snapshot.MaxPages < 1 || out.Snapshot.MaxPages > 10000 {
		return ImportWork{}, ErrInvalid
	}
	copy(out.Input.SHA256[:], digest)
	return out, nil
}
func (s *Store) PulseImport(ctx context.Context, owner, job, runner string) error {
	if !id(job) || !id(runner) {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		if _, err := s.loadImportWork(ctx, tx, owner, job, runner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET heartbeat_at_ms=? WHERE id=? AND owner_id=? AND runner_id=? AND state='running'", s.now().UnixMilli(), job, owner, runner); err != nil {
			return ErrStorage
		}
		return nil
	})
}
