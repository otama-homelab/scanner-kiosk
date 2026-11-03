package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"

	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
)

// ReadScannerReadiness projects all registered devices in a constant number of
// queries under the same enabled-principal/document transaction as admission.
// It never creates readiness, observes a device or reveals an owner's work.
func (s *Store) ReadScannerReadiness(ctx context.Context, owner string, ids []string, maxPixels int64) (scannerreadiness.Snapshot, error) {
	var out scannerreadiness.Snapshot
	if len(ids) > 32 || maxPixels < 1 {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !scannermetadata.ValidID(id) || seen[id] {
			return out, ErrInvalid
		}
		seen[id] = true
	}
	err := s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		var boot, payload string
		var generation, created, until, published, expires int64
		var capMin, capMax sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT w.boot_id,w.generation,w.created_at_ms,w.valid_until_ms,m.payload,m.published_at_ms,m.expires_at_ms,
   (SELECT min(scan_concurrency) FROM scanner_readiness WHERE boot_id=w.boot_id AND generation=w.generation),
   (SELECT max(scan_concurrency) FROM scanner_readiness WHERE boot_id=w.boot_id AND generation=w.generation)
   FROM processing_coordinator w JOIN scanner_metadata_snapshot m ON m.singleton=w.singleton AND m.boot_id=w.boot_id AND m.generation=w.generation
   WHERE w.singleton=1 AND w.state='active' AND w.created_at_ms<=? AND w.heartbeat_at_ms<=? AND w.valid_until_ms>?
   AND m.published_at_ms>=w.created_at_ms AND m.published_at_ms<=? AND m.expires_at_ms>? AND m.expires_at_ms<=w.valid_until_ms`, now, now, now, now, now).Scan(&boot, &generation, &created, &until, &payload, &published, &expires, &capMin, &capMax)
		if err == sql.ErrNoRows {
			return ErrUnavailable
		}
		if err != nil {
			return ErrStorage
		}
		if !id(boot) || len(payload) > 1<<20 {
			return ErrUnavailable
		}
		decoder := json.NewDecoder(strings.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&out.Metadata) != nil || decoder.Decode(new(any)) != io.EOF || out.Metadata.PublishedAtMS != published || out.Metadata.ExpiresAtMS != expires || scannermetadata.Validate(out.Metadata, time.UnixMilli(now)) != nil {
			return ErrUnavailable
		}
		supported := map[string]bool{}
		for _, device := range out.Metadata.Devices {
			for _, profile := range device.Profiles {
				if profile.CheckedAtMS < created {
					return ErrUnavailable
				}
				if int64(profile.Width)*int64(profile.Height) <= maxPixels {
					supported[device.ID] = true
				}
			}
		}
		occupied, unsafe, err := scanSlotState(ctx, tx, boot, "", now)
		if err != nil {
			return err
		}
		capacity := int(capMin.Int64)
		if capMin.Valid != capMax.Valid || capMin.Valid && (capacity < 1 || capacity > 32 || capMin.Int64 != capMax.Int64) {
			unsafe = true
		}
		out.ExpiresAtMS = min(until, expires)
		out.Devices = []scannerreadiness.DeviceStatus{}
		if len(ids) == 0 {
			return nil
		}
		args := make([]any, 0, len(ids))
		marks := make([]string, 0, len(ids))
		for _, id := range ids {
			args = append(args, id)
			marks = append(marks, "?")
		}
		rows, err := tx.QueryContext(ctx, `SELECT l.device_id,l.epoch,l.state,l.job_id,r.boot_id,r.generation,r.lease_epoch,r.ownership_confirmed_at_ms,r.local_stopped_at_ms,r.idle_observed_at_ms,r.published_at_ms,r.expires_at_ms,
   j.kind,j.state,j.runner_id,j.deadline_at_ms,json_extract(j.configuration_json,'$.worker_boot_id'),json_extract(j.configuration_json,'$.lease_epoch'),json_extract(j.configuration_json,'$.device_id')
   FROM device_leases l LEFT JOIN scanner_readiness r ON r.device_id=l.device_id LEFT JOIN jobs j ON j.id=l.job_id WHERE l.device_id IN (`+strings.Join(marks, ",")+`)`, args...)
		if err != nil {
			return ErrStorage
		}
		defer rows.Close()
		states := map[string]scannerreadiness.DeviceStatus{}
		for rows.Next() {
			var device, leaseState string
			var epoch int64
			var job, rBoot, kind, jobState, runner, sBoot, sDevice sql.NullString
			var rGen, rEpoch, ownership, stopped, idle, readyPublished, readyExpires, deadline, sEpoch sql.NullInt64
			if err = rows.Scan(&device, &epoch, &leaseState, &job, &rBoot, &rGen, &rEpoch, &ownership, &stopped, &idle, &readyPublished, &readyExpires, &kind, &jobState, &runner, &deadline, &sBoot, &sEpoch, &sDevice); err != nil {
				return ErrStorage
			}
			state := scannerreadiness.DeviceStatus{ID: device, State: scannerreadiness.Unknown, ExpiresAtMS: out.ExpiresAtMS}
			if unsafe {
				state.State = scannerreadiness.Unavailable
			} else if leaseState == "held" && job.Valid && kind.String == "scan" && (jobState.String == "queued" || jobState.String == "running") && runner.String == boot && sBoot.String == boot && sEpoch.Valid && sEpoch.Int64 == epoch && sDevice.String == device && deadline.Int64 > now {
				state.State = scannerreadiness.Busy
				state.ExpiresAtMS = min(state.ExpiresAtMS, deadline.Int64)
			} else if supported[device] && leaseState == "idle" && !job.Valid && rBoot.String == boot && rGen.Valid && rGen.Int64 == generation && rEpoch.Valid && rEpoch.Int64 == epoch && ownership.Valid && ownership.Int64 >= created && stopped.Valid && stopped.Int64 >= ownership.Int64 && idle.Valid && idle.Int64 >= stopped.Int64 && readyPublished.Valid && readyPublished.Int64 >= idle.Int64 && readyPublished.Int64 <= now && readyExpires.Valid && readyExpires.Int64 > now && readyExpires.Int64 <= until && readyExpires.Int64-idle.Int64 <= scannerreadiness.MaxIdleAge.Milliseconds() && now-idle.Int64 < scannerreadiness.MaxIdleAge.Milliseconds() && capMin.Valid {
				state.State = scannerreadiness.Idle
				state.ExpiresAtMS = min(state.ExpiresAtMS, readyExpires.Int64)
				if occupied >= capacity {
					state.State = scannerreadiness.Busy
				}
			}
			states[device] = state
		}
		if rows.Err() != nil {
			return ErrStorage
		}
		for _, id := range ids {
			if state, ok := states[id]; ok {
				out.Devices = append(out.Devices, state)
			} else {
				state := scannerreadiness.Unknown
				if unsafe {
					state = scannerreadiness.Unavailable
				}
				out.Devices = append(out.Devices, scannerreadiness.DeviceStatus{ID: id, State: state, ExpiresAtMS: out.ExpiresAtMS})
			}
		}
		current := s.now().UnixMilli()
		if current >= out.ExpiresAtMS {
			return ErrUnavailable
		}
		for i := range out.Devices {
			if current >= out.Devices[i].ExpiresAtMS {
				out.Devices[i].State = scannerreadiness.Unknown
			}
		}
		return nil
	})
	if err != nil {
		return scannerreadiness.Snapshot{}, err
	}
	return out, nil
}
