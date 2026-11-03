//go:build linux

// Package workercatalog persists short-lived processing metadata authority.
// It does not reconcile jobs, release device leases, or establish physical idle.
package workercatalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workeridentity"
)

var ErrUnavailable = errors.New("worker catalog unavailable")

type Store struct {
	db       *sql.DB
	now      func() time.Time
	lifetime time.Duration
}
type Lease struct {
	guard      *workeridentity.Guard
	boot       string
	generation int64
}

func New(db *sql.DB, now func() time.Time, lifetime time.Duration) (*Store, error) {
	if db == nil || now == nil || lifetime < time.Second || lifetime > 5*time.Minute {
		return nil, ErrUnavailable
	}
	return &Store{db, now, lifetime}, nil
}
func (s *Store) deadline(now int64) (int64, error) {
	delta := s.lifetime.Milliseconds()
	if now < 0 || now > math.MaxInt64-delta {
		return 0, ErrUnavailable
	}
	return now + delta, nil
}

// Begin requires the existing private-root coordinator flock. The caller must
// use the same document volume and pass recovery gates before publishing.
func (s *Store) Begin(ctx context.Context, guard *workeridentity.Guard) (Lease, error) {
	var lease Lease
	if guard == nil {
		return lease, ErrUnavailable
	}
	err := guard.WithActive(func(boot string) error {
		now := s.now().UnixMilli()
		until, err := s.deadline(now)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		var previous string
		var generation, heartbeat int64
		err = tx.QueryRowContext(ctx, "SELECT boot_id,generation,heartbeat_at_ms FROM processing_coordinator WHERE singleton=1").Scan(&previous, &generation, &heartbeat)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ErrUnavailable
		}
		if previous == boot || generation == math.MaxInt64 || heartbeat > now {
			return ErrUnavailable
		}
		generation++
		if _, err = tx.ExecContext(ctx, "DELETE FROM ocr_catalog_snapshot; DELETE FROM scanner_metadata_snapshot; DELETE FROM scanner_readiness"); err != nil {
			return ErrUnavailable
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,?,?,'active',?,?,?) ON CONFLICT(singleton) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,state='active',created_at_ms=excluded.created_at_ms,heartbeat_at_ms=excluded.heartbeat_at_ms,valid_until_ms=excluded.valid_until_ms`, boot, generation, now, now, until)
		if err != nil {
			return ErrUnavailable
		}
		if err = tx.Commit(); err != nil {
			return ErrUnavailable
		}
		lease = Lease{guard, boot, generation}
		return nil
	})
	if err != nil {
		return Lease{}, ErrUnavailable
	}
	return lease, nil
}
func (s *Store) write(ctx context.Context, lease Lease, action func(*sql.Tx, int64) error) error {
	if lease.guard == nil || lease.generation < 1 {
		return ErrUnavailable
	}
	err := lease.guard.WithActive(func(boot string) error {
		if boot != lease.boot {
			return ErrUnavailable
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrUnavailable
		}
		defer tx.Rollback()
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrUnavailable
		}
		if err = action(tx, now); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) Pulse(ctx context.Context, lease Lease) error {
	return s.write(ctx, lease, func(tx *sql.Tx, now int64) error {
		until, err := s.deadline(now)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE processing_coordinator SET heartbeat_at_ms=?,valid_until_ms=? WHERE singleton=1 AND boot_id=? AND generation=? AND state='active' AND heartbeat_at_ms<=? AND valid_until_ms>?`, now, until, lease.boot, lease.generation, now, now)
		if err != nil {
			return ErrUnavailable
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return ErrUnavailable
		}
		return nil
	})
}

// Publish assigns time bounds from current authority; caller times are forbidden.
func (s *Store) Publish(ctx context.Context, lease Lease, snapshot renderadmission.CatalogSnapshot) error {
	if snapshot.PublishedAtMS != 0 || snapshot.ExpiresAtMS != 0 {
		return ErrUnavailable
	}
	return s.write(ctx, lease, func(tx *sql.Tx, now int64) error {
		var until int64
		err := tx.QueryRowContext(ctx, `SELECT valid_until_ms FROM processing_coordinator WHERE singleton=1 AND boot_id=? AND generation=? AND state='active' AND heartbeat_at_ms<=? AND valid_until_ms>?`, lease.boot, lease.generation, now, now).Scan(&until)
		if err != nil {
			return ErrUnavailable
		}
		snapshot.PublishedAtMS = now
		snapshot.ExpiresAtMS = until
		if err = renderadmission.ValidateCatalogSnapshot(snapshot, time.UnixMilli(now)); err != nil {
			return ErrUnavailable
		}
		payload, err := json.Marshal(snapshot)
		if err != nil || len(payload) > 1<<20 {
			return ErrUnavailable
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ocr_catalog_snapshot(singleton,boot_id,generation,published_at_ms,expires_at_ms,payload) VALUES(1,?,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET boot_id=excluded.boot_id,generation=excluded.generation,published_at_ms=excluded.published_at_ms,expires_at_ms=excluded.expires_at_ms,payload=excluded.payload`, lease.boot, lease.generation, now, until, string(payload))
		if err != nil || s.now().UnixMilli() >= until {
			return ErrUnavailable
		}
		return nil
	})
}
func (s *Store) Stop(ctx context.Context, lease Lease) error {
	return s.write(ctx, lease, func(tx *sql.Tx, now int64) error {
		result, err := tx.ExecContext(ctx, `UPDATE processing_coordinator SET state='stopped' WHERE singleton=1 AND boot_id=? AND generation=? AND state='active'`, lease.boot, lease.generation)
		if err != nil {
			return ErrUnavailable
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return ErrUnavailable
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM ocr_catalog_snapshot WHERE boot_id=? AND generation=?", lease.boot, lease.generation); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM scanner_readiness WHERE boot_id=? AND generation=?", lease.boot, lease.generation); err != nil {
			return ErrUnavailable
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM scanner_metadata_snapshot WHERE boot_id=? AND generation=?", lease.boot, lease.generation); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

// Reader exposes only capability reads to the API composition root.
type Reader struct {
	db  *sql.DB
	now func() time.Time
}

func NewReader(db *sql.DB, now func() time.Time) (*Reader, error) {
	if db == nil || now == nil {
		return nil, ErrUnavailable
	}
	return &Reader{db, now}, nil
}
func (s *Store) ReadOCRCatalog(ctx context.Context) (renderadmission.CatalogSnapshot, error) {
	return (&Reader{s.db, s.now}).ReadOCRCatalog(ctx)
}
func (s *Reader) ReadOCRCatalog(ctx context.Context) (renderadmission.CatalogSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	var payload string
	var published, expires int64
	err = tx.QueryRowContext(ctx, `SELECT c.payload,c.published_at_ms,c.expires_at_ms FROM ocr_catalog_snapshot c JOIN processing_coordinator w ON w.singleton=c.singleton AND w.boot_id=c.boot_id AND w.generation=c.generation WHERE c.singleton=1 AND w.state='active' AND w.created_at_ms<=? AND w.heartbeat_at_ms<=? AND w.valid_until_ms>? AND c.published_at_ms>=w.created_at_ms AND c.published_at_ms<=? AND c.expires_at_ms>? AND c.expires_at_ms<=w.valid_until_ms`, now, now, now, now, now).Scan(&payload, &published, &expires)
	if err != nil || len(payload) > 1<<20 {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	var out renderadmission.CatalogSnapshot
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&out); err != nil {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	if _, err = decoder.Token(); err != io.EOF {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	if out.PublishedAtMS != published || out.ExpiresAtMS != expires || renderadmission.ValidateCatalogSnapshot(out, s.now()) != nil {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return renderadmission.CatalogSnapshot{}, ErrUnavailable
	}
	return out, nil
}
