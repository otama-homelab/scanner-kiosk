package simpleauth

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"scanner-kiosk/internal/authport"
)

// SQLiteRecords borrows the state database. Transactions never contain KDF work.
type SQLiteRecords struct {
	db     *sql.DB
	now    func() time.Time
	commit func(*sql.Tx) error
}

func NewSQLiteRecords(db *sql.DB, now func() time.Time) (*SQLiteRecords, error) {
	if db == nil || now == nil {
		return nil, authport.ErrUnavailable
	}
	return &SQLiteRecords{db: db, now: now, commit: func(tx *sql.Tx) error { return tx.Commit() }}, nil
}

var _ Records = (*SQLiteRecords)(nil)

func (s *SQLiteRecords) Lookup(ctx context.Context, name string) (Credential, bool, error) {
	var out Credential
	var encoded string
	var disabled sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT p.id,c.password_hash,c.credential_version,p.disabled_at_ms FROM local_credentials c JOIN principals p ON p.id=c.principal_id AND p.kind='simple' WHERE c.username=?`, name).Scan(&out.PrincipalID, &encoded, &out.Version, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, false, nil
	}
	if err != nil {
		return Credential{}, false, authport.ErrUnavailable
	}
	out.Hash, err = ParsePasswordHash(encoded)
	if err != nil {
		return Credential{}, false, err
	}
	out.Disabled = disabled.Valid
	return out, true, nil
}

func (s *SQLiteRecords) Create(ctx context.Context, name, principal string, hash PasswordHash) (Credential, error) {
	canonical, err := NormalizeUsername(name)
	if err != nil || canonical != name {
		return Credential{}, authport.ErrDenied
	}
	if _, err := ParsePasswordHash(hash.Encoded()); err != nil {
		return Credential{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM local_credentials WHERE username=?)", name).Scan(&exists); err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	if exists != 0 {
		return Credential{}, authport.ErrConflict
	}
	now := s.now().UnixMilli()
	if now < 0 {
		return Credential{}, authport.ErrUnavailable
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'simple',?)", principal, now); err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO local_credentials(username,principal_id,password_hash,credential_version,created_at_ms,updated_at_ms) VALUES(?,?,?,1,?,?)", name, principal, hash.Encoded(), now, now); err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	if err := s.commit(tx); err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	return Credential{PrincipalID: principal, Version: 1, Hash: hash}, nil
}

func (s *SQLiteRecords) Replace(ctx context.Context, name string, hash PasswordHash) (Credential, error) {
	canonical, err := NormalizeUsername(name)
	if err != nil || canonical != name {
		return Credential{}, authport.ErrDenied
	}
	if _, err := ParsePasswordHash(hash.Encoded()); err != nil {
		return Credential{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	defer tx.Rollback()
	var out Credential
	var updated int64
	err = tx.QueryRowContext(ctx, `SELECT c.principal_id,c.credential_version,c.updated_at_ms FROM local_credentials c JOIN principals p ON p.id=c.principal_id AND p.kind='simple' AND p.disabled_at_ms IS NULL WHERE c.username=?`, name).Scan(&out.PrincipalID, &out.Version, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, authport.ErrDenied
	}
	if err != nil || out.Version == math.MaxInt64 {
		return Credential{}, authport.ErrUnavailable
	}
	now := s.now().UnixMilli()
	if now < updated {
		return Credential{}, authport.ErrUnavailable
	}
	result, err := tx.ExecContext(ctx, "UPDATE local_credentials SET password_hash=?,credential_version=credential_version+1,updated_at_ms=? WHERE username=? AND credential_version=?", hash.Encoded(), now, name, out.Version)
	if err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return Credential{}, authport.ErrUnavailable
	}
	if err := s.commit(tx); err != nil {
		return Credential{}, authport.ErrUnavailable
	}
	out.Version++
	out.Hash = hash
	return out, nil
}

func (s *SQLiteRecords) Current(ctx context.Context, principal string, version int64) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM local_credentials c JOIN principals p ON p.id=c.principal_id AND p.kind='simple' AND p.disabled_at_ms IS NULL WHERE c.principal_id=? AND c.credential_version=?)`, principal, version).Scan(&exists)
	if err != nil {
		return false, authport.ErrUnavailable
	}
	return exists != 0, nil
}
