package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
)

// RecoveryScan is internal, read-only discovery under the exclusive worker
// guard. Its old boot/epoch are bindings, never evidence that work has stopped.
// Disabled owners and expired documents still hold physical authority.
type RecoveryScan struct {
	OwnerID, DocumentID, JobID string
	CreatedAtMS, StartedAtMS   int64
	Snapshot                   ScanSnapshot
}

func (s *Store) RecoveryScans(ctx context.Context) ([]RecoveryScan, error) {
	rows, err := s.documents.QueryContext(ctx, `SELECT j.owner_id,j.document_id,j.id,j.created_at_ms,j.started_at_ms,j.heartbeat_at_ms,
 COALESCE(j.runner_id,''),j.configuration_json,COALESCE(l.device_id,''),COALESCE(l.epoch,0),COALESCE(r.byte_size,-1)
 FROM jobs j LEFT JOIN device_leases l ON l.job_id=j.id LEFT JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
 WHERE j.kind='scan' AND j.state IN ('queued','running','cancelling','reconciling') ORDER BY j.created_at_ms,j.id LIMIT 101`)
	if err != nil {
		return nil, storageOperationError(err)
	}
	defer rows.Close()
	out := []RecoveryScan{}
	for rows.Next() {
		var item RecoveryScan
		var start, heartbeat sql.NullInt64
		var boot, configuration, device string
		var epoch, reserved int64
		if err = rows.Scan(&item.OwnerID, &item.DocumentID, &item.JobID, &item.CreatedAtMS, &start, &heartbeat, &boot, &configuration, &device, &epoch, &reserved); err != nil {
			return nil, ErrStorage
		}
		if len(out) == 100 || len(configuration) > 65536 || !id(item.OwnerID) || !id(item.DocumentID) || !id(item.JobID) || !id(boot) || !id(device) || reserved < 0 || item.CreatedAtMS < 0 || (!start.Valid && heartbeat.Valid) {
			return nil, ErrStorage
		}
		decoder := json.NewDecoder(strings.NewReader(configuration))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&item.Snapshot) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, ErrStorage
		}
		snapshot := item.Snapshot
		if snapshot.Version != 1 || !validScanPlacement(snapshot.InsertBeforePageID, snapshot.ReplacePageID) || snapshot.WorkerBootID != boot || snapshot.DeviceID != device || snapshot.LeaseEpoch != epoch || epoch < 1 || !validCapture(snapshot.Capture) || !validScanBinding(snapshot) {
			return nil, ErrStorage
		}
		if start.Valid {
			if start.Int64 < item.CreatedAtMS {
				return nil, ErrStorage
			}
			item.StartedAtMS = start.Int64
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, storageOperationError(err)
	}
	return out, nil
}

// RecoveryRender carries no authority to retry processing or publish output.
type RecoveryRender struct {
	OwnerID, JobID, RunnerID string
	StartedAtMS              int64
}

func (s *Store) RecoveryRenders(ctx context.Context) ([]RecoveryRender, error) {
	rows, err := s.documents.QueryContext(ctx, `SELECT j.owner_id,j.id,COALESCE(j.runner_id,''),j.started_at_ms,COALESCE(r.byte_size,-1),
 EXISTS(SELECT 1 FROM job_artifacts a WHERE a.job_id=j.id),EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id)
 FROM jobs j LEFT JOIN reservations r ON r.job_id=j.id AND r.owner_id=j.owner_id
 WHERE j.kind='render' AND (j.state IN ('running','cancelling','reconciling') OR (j.state='queued' AND (j.runner_id IS NOT NULL OR j.started_at_ms IS NOT NULL)))
 ORDER BY j.created_at_ms,j.id LIMIT 101`)
	if err != nil {
		return nil, storageOperationError(err)
	}
	defer rows.Close()
	out := []RecoveryRender{}
	for rows.Next() {
		var item RecoveryRender
		var started sql.NullInt64
		var reserved int64
		var produced, leased bool
		if rows.Scan(&item.OwnerID, &item.JobID, &item.RunnerID, &started, &reserved, &produced, &leased) != nil {
			return nil, ErrStorage
		}
		if len(out) == 100 || !id(item.OwnerID) || !id(item.JobID) || !id(item.RunnerID) || !started.Valid || started.Int64 <= 0 || reserved < 0 || produced || leased {
			return nil, ErrStorage
		}
		item.StartedAtMS = started.Int64
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, storageOperationError(err)
	}
	return out, nil
}

// RecoveryDirectoryUnreferenced protects ALL registered artifacts regardless
// of owner enablement, document expiry, or publication state during offline GC.
func (s *Store) RecoveryDirectoryUnreferenced(ctx context.Context, directory string) (bool, error) {
	if !id(directory) {
		return false, ErrInvalid
	}
	prefix := directory + "/"
	var found int
	err := s.documents.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE substr(relative_path,1,?)=?)`, len(prefix), prefix).Scan(&found)
	if err != nil {
		return false, storageOperationError(err)
	}
	return found == 0, nil
}
