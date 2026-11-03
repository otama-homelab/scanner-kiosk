//go:build linux

package workercatalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"time"

	"scanner-kiosk/internal/scannermetadata"
)

// PublishScanners uses the existing coordinator generation/guard, but does not
// initialize idle leases. Native inspection must happen outside this transaction.
func (s *Store) PublishScanners(ctx context.Context, lease Lease, snapshot scannermetadata.Snapshot) error {
	if snapshot.PublishedAtMS != 0 || snapshot.ExpiresAtMS != 0 {
		return ErrUnavailable
	}
	return s.write(ctx, lease, func(tx *sql.Tx, now int64) error {
		var until, created int64
		err := tx.QueryRowContext(ctx, `SELECT valid_until_ms,created_at_ms FROM processing_coordinator WHERE singleton=1 AND boot_id=? AND generation=? AND state='active' AND heartbeat_at_ms<=? AND valid_until_ms>?`, lease.boot, lease.generation, now, now).Scan(&until, &created)
		if err != nil {
			return ErrUnavailable
		}
		for _, device := range snapshot.Devices {
			for _, profile := range device.Profiles {
				if profile.CheckedAtMS < created || profile.CheckedAtMS > now {
					return ErrUnavailable
				}
				until = min(until, profile.CheckedAtMS+scannermetadata.MaxAge.Milliseconds())
			}
		}
		snapshot.PublishedAtMS, snapshot.ExpiresAtMS = now, until
		if scannermetadata.Validate(snapshot, time.UnixMilli(now)) != nil {
			return ErrUnavailable
		}
		payload, err := json.Marshal(snapshot)
		if err != nil || len(payload) > 1<<20 {
			return ErrUnavailable
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO scanner_metadata_snapshot(singleton,boot_id,generation,published_at_ms,expires_at_ms,payload) VALUES(1,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,published_at_ms=excluded.published_at_ms,expires_at_ms=excluded.expires_at_ms,payload=excluded.payload`, lease.boot, lease.generation, now, until, string(payload))
		if err != nil || s.now().UnixMilli() >= until {
			return ErrUnavailable
		}
		return nil
	})
}

func (s *Store) ReadScannerMetadata(ctx context.Context) (scannermetadata.Snapshot, error) {
	return (&Reader{s.db, s.now}).ReadScannerMetadata(ctx)
}
func (s *Reader) ReadScannerMetadata(ctx context.Context) (scannermetadata.Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return scannermetadata.Snapshot{}, ErrUnavailable
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	var payload string
	var published, expires, created int64
	err = tx.QueryRowContext(ctx, `SELECT c.payload,c.published_at_ms,c.expires_at_ms,w.created_at_ms FROM scanner_metadata_snapshot c JOIN processing_coordinator w ON w.singleton=c.singleton AND w.boot_id=c.boot_id AND w.generation=c.generation
 WHERE c.singleton=1 AND w.state='active' AND w.created_at_ms<=? AND w.heartbeat_at_ms<=? AND w.valid_until_ms>? AND c.published_at_ms>=w.created_at_ms AND c.published_at_ms<=? AND c.expires_at_ms>? AND c.expires_at_ms<=w.valid_until_ms`, now, now, now, now, now).Scan(&payload, &published, &expires, &created)
	if err != nil || len(payload) > 1<<20 {
		return scannermetadata.Snapshot{}, ErrUnavailable
	}
	var out scannermetadata.Snapshot
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || out.PublishedAtMS != published || out.ExpiresAtMS != expires || scannermetadata.Validate(out, s.now()) != nil {
		return scannermetadata.Snapshot{}, ErrUnavailable
	}
	for _, device := range out.Devices {
		for _, profile := range device.Profiles {
			if profile.CheckedAtMS < created {
				return scannermetadata.Snapshot{}, ErrUnavailable
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return scannermetadata.Snapshot{}, ErrUnavailable
	}
	return out, nil
}
