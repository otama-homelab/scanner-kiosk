package documents

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNoWork = errors.New("no runnable job")

type ClaimedRender struct {
	OwnerID string
	Job     Job
}

// ClaimNextRender atomically selects enabled-owner work and claims it under the
// principal snapshot/document transaction. Existing uncertain or running work,
// including an old boot or a pinned scan, prevents new heavy work. No scan is
// queued or resumed by this method. Caller must hold the local worker guard.
func (s *Store) ClaimNextRender(ctx context.Context, runnerID string) (ClaimedRender, error) {
	var out ClaimedRender
	if !id(runnerID) {
		return out, ErrInvalid
	}
	err := s.withPrincipalSnapshot(ctx, func(principals *sql.Conn, tx *sql.Tx) error {
		var busy int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind!='cleanup' AND (state IN ('running','cancelling','reconciling') OR (state='queued' AND (runner_id IS NOT NULL OR started_at_ms IS NOT NULL)))) OR EXISTS(SELECT 1 FROM device_leases WHERE state!='idle')`).Scan(&busy); err != nil {
			return storageOperationError(err)
		}
		if busy != 0 {
			return ErrNoWork
		}
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT j.id,j.owner_id FROM jobs j JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
 WHERE j.kind='render' AND j.state='queued' AND j.started_at_ms IS NULL AND j.runner_id IS NULL AND j.deadline_at_ms>?
 AND d.state='processing' AND d.revision=j.input_revision AND d.expires_at_ms>?
 AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id AND r.byte_size>0)
 ORDER BY j.created_at_ms,j.id`, now, now)
		if err != nil {
			return storageOperationError(err)
		}
		defer rows.Close()
		var jobID, owner string
		for rows.Next() {
			if err = rows.Scan(&jobID, &owner); err != nil {
				return storageOperationError(err)
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
				return storageOperationError(err)
			}
			out.Job, err = s.claimRenderTx(ctx, tx, owner, jobID, runnerID)
			if err != nil {
				return err
			}
			out.OwnerID = owner
			return nil
		}
		if err := rows.Err(); err != nil {
			return storageOperationError(err)
		}
		return ErrNoWork
	})
	if err != nil {
		return ClaimedRender{}, err
	}
	return out, nil
}

// ClaimedScan is worker-internal; its owner comes from the pinned principal set.
type ClaimedScan struct {
	OwnerID string
	Job     Job
}

// ClaimNextScan selects and claims an admitted physical scan in one transaction.
// Only a never-started scan pinned to this exact boot is eligible. Uncertain work
// or another held device lease blocks heavy work globally. No scan is created,
// resumed, requeued or retried. The caller must hold the live local worker guard.
func (s *Store) ClaimNextScan(ctx context.Context, workerBootID string) (ClaimedScan, error) {
	return s.ClaimNextScanWithLimit(ctx, workerBootID, 1)
}

// ClaimNextScanWithLimit selects within trusted deployment scan capacity.
func (s *Store) ClaimNextScanWithLimit(ctx context.Context, workerBootID string, limit int) (ClaimedScan, error) {
	var out ClaimedScan
	if !id(workerBootID) || limit < 1 || limit > 32 {
		return out, ErrInvalid
	}
	err := s.withPrincipalSnapshot(ctx, func(principals *sql.Conn, tx *sql.Tx) error {
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT j.id,j.owner_id FROM jobs j
 JOIN documents d ON d.id=j.document_id AND d.owner_id=j.owner_id
 JOIN device_leases l ON l.job_id=j.id AND l.state='held'
 WHERE j.kind='scan' AND j.state='queued' AND j.runner_id=? AND j.started_at_ms IS NULL AND j.deadline_at_ms>?
 AND d.state='scanning' AND d.revision=j.input_revision AND d.expires_at_ms>?
 AND json_extract(j.configuration_json,'$.version')=1 AND json_extract(j.configuration_json,'$.worker_boot_id')=?
 AND l.device_id=json_extract(j.configuration_json,'$.device_id') AND l.epoch=json_extract(j.configuration_json,'$.lease_epoch')
 AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id AND r.byte_size>0)
 ORDER BY j.created_at_ms,j.id`, workerBootID, now, now, workerBootID)
		if err != nil {
			return storageOperationError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var jobID, owner string
			if err = rows.Scan(&jobID, &owner); err != nil {
				return storageOperationError(err)
			}
			if err := scanSlotsAvailable(ctx, tx, workerBootID, limit, jobID, now); err != nil {
				if err == ErrDeviceBusy {
					return ErrNoWork
				}
				return err
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
				return storageOperationError(err)
			}
			out.Job, err = s.claimScanTx(ctx, tx, owner, jobID, workerBootID, limit)
			if err != nil {
				return err
			}
			out.OwnerID = owner
			return nil
		}
		if err := rows.Err(); err != nil {
			return storageOperationError(err)
		}
		return ErrNoWork
	})
	if err != nil {
		return ClaimedScan{}, err
	}
	return out, nil
}
