//go:build linux

package workercatalog

import (
	"context"
	"database/sql"
	"errors"

	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
)

// PublishScannerReadiness trusts the supplied reconciler evidence, not metadata.
// No production evidence producer is wired yet. This boundary never contacts or
// stops a device, revokes another writer, or clears held/uncertain work.
func (s *Store) PublishScannerReadiness(ctx context.Context, lease Lease, proof scannerreadiness.Evidence) (int64, error) {
	if !scannermetadata.ValidID(proof.DeviceID) || !scannermetadata.ValidID(proof.HandoffID) || !scannermetadata.ValidID(proof.PhysicalID) || proof.LeaseEpoch < 0 || proof.ScanConcurrency < 1 || proof.ScanConcurrency > 32 {
		return 0, ErrUnavailable
	}
	var epoch int64
	err := s.write(ctx, lease, func(tx *sql.Tx, now int64) error {
		var created, until int64
		if err := tx.QueryRowContext(ctx, `SELECT created_at_ms,valid_until_ms FROM processing_coordinator WHERE singleton=1 AND boot_id=? AND generation=? AND state='active' AND heartbeat_at_ms<=? AND valid_until_ms>?`, lease.boot, lease.generation, now, now).Scan(&created, &until); err != nil {
			return ErrUnavailable
		}
		if proof.OwnershipConfirmedAtMS < created || proof.LocalStoppedAtMS < proof.OwnershipConfirmedAtMS || proof.IdleObservedAtMS < proof.LocalStoppedAtMS || proof.IdleObservedAtMS > now || now-proof.IdleObservedAtMS >= scannerreadiness.MaxIdleAge.Milliseconds() {
			return ErrUnavailable
		}
		until = min(until, proof.IdleObservedAtMS+scannerreadiness.MaxIdleAge.Milliseconds())
		// Capacity is immutable across all readiness publications for this boot.
		var changed int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scanner_readiness WHERE boot_id=? AND generation=? AND (scan_concurrency!=? OR (device_id=? AND physical_id!=?)))`, lease.boot, lease.generation, proof.ScanConcurrency, proof.DeviceID, proof.PhysicalID).Scan(&changed); err != nil || changed != 0 {
			return ErrUnavailable
		}
		var state string
		var job sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT epoch,state,job_id FROM device_leases WHERE device_id=?", proof.DeviceID).Scan(&epoch, &state, &job)
		if errors.Is(err, sql.ErrNoRows) {
			if proof.LeaseEpoch != 0 {
				return ErrUnavailable
			}
			var uncertain int
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='scan' AND json_extract(j.configuration_json,'$.device_id')=? AND (j.state IN ('queued','running','cancelling','reconciling') OR EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id)))`, proof.DeviceID).Scan(&uncertain); err != nil || uncertain != 0 {
				return ErrUnavailable
			}
			epoch = 1
			if _, err = tx.ExecContext(ctx, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", proof.DeviceID); err != nil {
				return ErrUnavailable
			}
		} else if err != nil || state != "idle" || job.Valid || epoch != proof.LeaseEpoch {
			return ErrUnavailable
		}
		var unclean int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='scan' AND json_extract(j.configuration_json,'$.device_id')=? AND json_extract(j.configuration_json,'$.lease_epoch')=? AND (j.state IN ('queued','running','cancelling','reconciling') OR EXISTS(SELECT 1 FROM reservations r WHERE r.job_id=j.id) OR max(j.created_at_ms,COALESCE(j.started_at_ms,j.created_at_ms),COALESCE(j.heartbeat_at_ms,j.created_at_ms))>?))`, proof.DeviceID, epoch, proof.LocalStoppedAtMS).Scan(&unclean); err != nil || unclean != 0 {
			return ErrUnavailable
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO scanner_readiness(device_id,boot_id,generation,lease_epoch,handoff_id,physical_id,scan_concurrency,ownership_confirmed_at_ms,local_stopped_at_ms,idle_observed_at_ms,published_at_ms,expires_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(device_id) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,lease_epoch=excluded.lease_epoch,handoff_id=excluded.handoff_id,physical_id=excluded.physical_id,scan_concurrency=excluded.scan_concurrency,ownership_confirmed_at_ms=excluded.ownership_confirmed_at_ms,local_stopped_at_ms=excluded.local_stopped_at_ms,idle_observed_at_ms=excluded.idle_observed_at_ms,published_at_ms=excluded.published_at_ms,expires_at_ms=excluded.expires_at_ms`, proof.DeviceID, lease.boot, lease.generation, epoch, proof.HandoffID, proof.PhysicalID, proof.ScanConcurrency, proof.OwnershipConfirmedAtMS, proof.LocalStoppedAtMS, proof.IdleObservedAtMS, now, until)
		if err != nil || s.now().UnixMilli() >= until {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return 0, ErrUnavailable
	}
	return epoch, nil
}
