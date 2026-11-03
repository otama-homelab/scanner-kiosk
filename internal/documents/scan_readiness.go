package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"

	scannerport "scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
)

type readyScanAuthority struct {
	boot              string
	physical, handoff string
	generation, epoch int64
	capacity          int
}

func (s *Store) scanReadiness(ctx context.Context, tx *sql.Tx, request ScanRequest, maxPixels int64, job string, now int64) (readyScanAuthority, error) {
	var out readyScanAuthority
	var created, published, expires, ownership, stopped, idle, readyPublished, readyExpires int64
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT w.boot_id,w.generation,w.created_at_ms,r.lease_epoch,r.scan_concurrency,r.ownership_confirmed_at_ms,r.local_stopped_at_ms,r.idle_observed_at_ms,r.published_at_ms,r.expires_at_ms,m.payload,m.published_at_ms,m.expires_at_ms,r.physical_id,r.handoff_id
 FROM processing_coordinator w JOIN scanner_readiness r ON r.boot_id=w.boot_id AND r.generation=w.generation
 JOIN scanner_metadata_snapshot m ON m.singleton=w.singleton AND m.boot_id=w.boot_id AND m.generation=w.generation
 JOIN device_leases l ON l.device_id=r.device_id
 WHERE w.singleton=1 AND w.state='active' AND w.created_at_ms<=? AND w.heartbeat_at_ms<=? AND w.valid_until_ms>?
 AND NOT EXISTS(SELECT 1 FROM scanner_readiness other WHERE other.boot_id=r.boot_id AND other.generation=r.generation AND other.scan_concurrency!=r.scan_concurrency)
 AND r.device_id=? AND r.published_at_ms<=? AND r.expires_at_ms>? AND r.expires_at_ms<=w.valid_until_ms
 AND m.published_at_ms>=w.created_at_ms AND m.published_at_ms<=? AND m.expires_at_ms>? AND m.expires_at_ms<=w.valid_until_ms
 AND ((?='' AND l.state='idle' AND l.job_id IS NULL AND l.epoch=r.lease_epoch) OR (?!='' AND l.state='held' AND l.job_id=? AND l.epoch=r.lease_epoch+1))`, now, now, now, request.DeviceID, now, now, now, now, job, job, job).Scan(&out.boot, &out.generation, &created, &out.epoch, &out.capacity, &ownership, &stopped, &idle, &readyPublished, &readyExpires, &payload, &published, &expires, &out.physical, &out.handoff)
	if err == sql.ErrNoRows {
		return out, ErrDeviceBusy
	}
	if err != nil {
		return out, ErrStorage
	}
	if !scannermetadata.ValidID(out.physical) || !scannermetadata.ValidID(out.handoff) || !id(out.boot) || out.capacity < 1 || out.capacity > 32 || ownership < created || stopped < ownership || idle < stopped || idle > readyPublished || readyExpires-idle > scannerreadiness.MaxIdleAge.Milliseconds() || now-idle >= scannerreadiness.MaxIdleAge.Milliseconds() || len(payload) > 1<<20 {
		return readyScanAuthority{}, ErrDeviceBusy
	}
	var metadata scannermetadata.Snapshot
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil || decoder.Decode(new(any)) != io.EOF || metadata.PublishedAtMS != published || metadata.ExpiresAtMS != expires || scannermetadata.Validate(metadata, time.UnixMilli(now)) != nil {
		return readyScanAuthority{}, ErrDeviceBusy
	}
	selected := scannerport.Capture{Size: request.Capture.Size, Color: request.Capture.Color, DPI: request.Capture.DPI}
	matched := false
	for _, device := range metadata.Devices {
		for _, profile := range device.Profiles {
			if profile.CheckedAtMS < created {
				return readyScanAuthority{}, ErrDeviceBusy
			}
			if device.ID == request.DeviceID && profile.Capture == selected && int64(profile.Width)*int64(profile.Height) <= maxPixels {
				matched = true
			}
		}
	}
	if matched {
		return out, nil
	}
	return readyScanAuthority{}, ErrDeviceBusy
}
