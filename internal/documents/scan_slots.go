package documents

import (
	"context"
	"database/sql"
)

// scanSlotsAvailable runs under the existing principal/document transaction.
// Only live, boot-pinned scans with intact leases/reservations may share scan
// capacity. Unknown/foreign work remains a recovery barrier. excludeJob is the
// candidate's already reserved slot during claim, never a browser parameter.
func scanSlotsAvailable(ctx context.Context, tx *sql.Tx, boot string, limit int, excludeJob string, now int64) error {
	if limit < 1 || limit > 32 {
		return ErrInvalid
	}
	occupied, unsafe, err := scanSlotState(ctx, tx, boot, excludeJob, now)
	if err != nil {
		return err
	}
	if unsafe || occupied >= limit {
		return ErrDeviceBusy
	}
	return nil
}

// scanSlotState reads aggregate shared-slot and recovery barriers once, without
// exposing SQL to the HTTP/service ports or looping over devices.
func scanSlotState(ctx context.Context, tx *sql.Tx, boot, excludeJob string, now int64) (int, bool, error) {
	var occupied, unsafe int
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM jobs WHERE kind='scan' AND id!=? AND state IN ('queued','running','cancelling','reconciling')),
 EXISTS(SELECT 1 FROM jobs j WHERE j.id!=? AND j.kind!='cleanup' AND
 (j.state IN ('running','cancelling','reconciling') OR (j.state='queued' AND (j.kind='scan' OR j.runner_id IS NOT NULL OR j.started_at_ms IS NOT NULL)))
 AND NOT COALESCE((j.kind='scan' AND j.runner_id=? AND j.state IN ('queued','running') AND j.deadline_at_ms>?
  AND ((j.state='queued' AND j.started_at_ms IS NULL) OR (j.state='running' AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=?))
  AND json_extract(j.configuration_json,'$.version')=1 AND json_extract(j.configuration_json,'$.worker_boot_id')=?
  AND EXISTS(SELECT 1 FROM device_leases l WHERE l.job_id=j.id AND l.state='held' AND l.device_id=json_extract(j.configuration_json,'$.device_id') AND l.epoch=json_extract(j.configuration_json,'$.lease_epoch'))
  AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id AND r.byte_size>0)),0))
 OR EXISTS(SELECT 1 FROM device_leases l WHERE (l.state!='idle' OR l.job_id IS NOT NULL) AND (l.job_id IS NULL OR l.job_id!=?)
  AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.id=l.job_id AND j.kind='scan' AND j.runner_id=? AND j.state IN ('queued','running') AND j.deadline_at_ms>?
   AND ((j.state='queued' AND j.started_at_ms IS NULL) OR (j.state='running' AND j.started_at_ms IS NOT NULL AND j.started_at_ms<=?))
   AND l.state='held' AND l.device_id=json_extract(j.configuration_json,'$.device_id') AND l.epoch=json_extract(j.configuration_json,'$.lease_epoch')
   AND json_extract(j.configuration_json,'$.version')=1 AND json_extract(j.configuration_json,'$.worker_boot_id')=?
   AND EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id AND r.owner_id=j.owner_id AND r.byte_size>0)))`, excludeJob, excludeJob, boot, now, now, boot, excludeJob, boot, now, now, boot).Scan(&occupied, &unsafe)
	if err != nil {
		return 0, false, storageOperationError(err)
	}
	return occupied, unsafe != 0, nil
}
