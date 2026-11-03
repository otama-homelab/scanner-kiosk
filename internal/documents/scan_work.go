package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ScanWork is worker-internal metadata for one already claimed physical scan.
// Geometry must still be resolved against backend capabilities before capture.
type ScanWork struct {
	Job          Job
	Snapshot     ScanSnapshot
	ReserveBytes int64
}

// LoadScanWork never claims or retries physical work. Every read is fenced by
// enabled owner, current document, exact boot and held device lease epoch.
func (s *Store) LoadScanWork(ctx context.Context, owner, jobID, workerBootID string) (ScanWork, error) {
	if !id(jobID) || !id(workerBootID) {
		return ScanWork{}, ErrInvalid
	}
	var out ScanWork
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		var err error
		out, err = s.loadScanWork(ctx, tx, owner, jobID, workerBootID)
		return err
	})
	if err != nil {
		return ScanWork{}, err
	}
	return out, nil
}

func (s *Store) loadScanWork(ctx context.Context, tx *sql.Tx, owner, jobID, boot string) (ScanWork, error) {
	var out ScanWork
	var device, purpose string
	var epoch, expiry, started int64
	now := s.now().UnixMilli()
	err := tx.QueryRowContext(ctx, `SELECT j.id,j.document_id,j.kind,j.configuration_json,j.input_revision,j.deadline_at_ms,r.byte_size,l.device_id,l.epoch,d.purpose,d.expires_at_ms,j.started_at_ms
 FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
 JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
 JOIN device_leases l ON l.job_id=j.id AND l.state='held'
 WHERE j.id=? AND j.owner_id=? AND j.runner_id=? AND j.kind='scan' AND j.state='running' AND j.started_at_ms IS NOT NULL
 AND d.revision=j.input_revision AND d.state='scanning' AND j.deadline_at_ms>? AND d.expires_at_ms>?`, jobID, owner, boot, now, now).Scan(&out.Job.ID, &out.Job.DocumentID, &out.Job.Kind, &out.Job.Configuration, &out.Job.Revision, &out.Job.DeadlineAtMS, &out.ReserveBytes, &device, &epoch, &purpose, &expiry, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanWork{}, ErrConflict
	}
	if err != nil {
		return ScanWork{}, ErrStorage
	}
	if len(out.Job.Configuration) > 65536 || out.ReserveBytes < 1 || started < 0 || started > now {
		return ScanWork{}, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(out.Job.Configuration))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out.Snapshot) != nil || decoder.Decode(new(any)) != io.EOF {
		return ScanWork{}, ErrInvalid
	}
	snapshot := out.Snapshot
	if !validScanBinding(snapshot) || snapshot.Version != 1 || !id(snapshot.DeviceID) || snapshot.DeviceID != device || snapshot.WorkerBootID != boot || snapshot.LeaseEpoch < 1 || snapshot.LeaseEpoch != epoch || !validCapture(snapshot.Capture) {
		return ScanWork{}, ErrConflict
	}
	if (purpose == "photo" && snapshot.PhotoFormat != "jpeg" && snapshot.PhotoFormat != "png") || (purpose == "document" && snapshot.PhotoFormat != "") || (purpose != "photo" && purpose != "document") {
		return ScanWork{}, ErrInvalid
	}
	current := s.now().UnixMilli()
	if current >= expiry || current >= out.Job.DeadlineAtMS {
		return ScanWork{}, ErrConflict
	}
	return out, nil
}

// PulseScan fails closed on authorization loss, cancellation, expiry or a lease
// mismatch. It only renews a heartbeat; it never proves physical idle, changes
// the immutable start, releases a lease/reservation or repeats a capture.
func (s *Store) PulseScan(ctx context.Context, owner, jobID, workerBootID string) error {
	if !id(jobID) || !id(workerBootID) {
		return ErrInvalid
	}
	return s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		if _, err := s.loadScanWork(ctx, tx, owner, jobID, workerBootID); err != nil {
			return err
		}
		now := s.now().UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE jobs SET heartbeat_at_ms=? WHERE id=? AND owner_id=? AND runner_id=? AND kind='scan' AND state='running' AND deadline_at_ms>?
 AND EXISTS(SELECT 1 FROM documents d WHERE d.id=jobs.document_id AND d.owner_id=jobs.owner_id AND d.expires_at_ms>?)`, now, jobID, owner, workerBootID, now, now)
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
