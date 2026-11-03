// Package oidcidentity maps verified issuer/subject to durable application identity.
package oidcidentity

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

// SQLiteRecords owns the atomic browser-active/disabled check and issuer/sub
// mapping. It borrows the state database and does no network verification.
type SQLiteRecords struct {
	db     *sql.DB
	commit func(*sql.Tx) error
}

func NewSQLiteRecords(db *sql.DB) (*SQLiteRecords, error) {
	if db == nil {
		return nil, ErrStorage
	}
	return &SQLiteRecords{db: db, commit: func(tx *sql.Tx) error { return tx.Commit() }}, nil
}

// New preserves the SQLite constructor for existing callers. New compositions
// should wire Records explicitly through NewWithRecords.
func New(db *sql.DB) (*Store, error) {
	records, err := NewSQLiteRecords(db)
	if err != nil {
		return nil, err
	}
	return NewWithRecords(records, time.Now)
}
func (s *SQLiteRecords) Resolve(ctx context.Context, command ResolveRecord) (string, error) {
	if command.Issuer == "" || command.Subject == "" || !principalID(command.ProposedPrincipalID) || command.CreatedAtMS < 0 {
		return "", ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", ErrStorage
	}
	defer tx.Rollback()
	binding := command.BrowserHash
	var active int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions s WHERE token=? AND expiry>julianday('now') AND NOT EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=s.token)`, base64.RawURLEncoding.EncodeToString(binding[:])).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", ErrStorage
	}
	var id string
	var disabled sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT p.id,p.disabled_at_ms FROM oidc_identities i JOIN principals p ON p.id=i.principal_id WHERE i.issuer=? AND i.subject=? AND p.kind='oidc'", command.Issuer, command.Subject).Scan(&id, &disabled)
	if err == nil {
		if disabled.Valid {
			return "", ErrDenied
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", ErrStorage
	} else {
		id = command.ProposedPrincipalID
		if _, err = tx.ExecContext(ctx, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',?)", id, command.CreatedAtMS); err != nil {
			return "", ErrStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO oidc_identities(issuer,subject,principal_id) VALUES(?,?,?)", command.Issuer, command.Subject, id); err != nil {
			return "", ErrStorage
		}
	}
	if err = s.commit(tx); err != nil {
		return "", ErrUncertain
	}
	return id, nil
}

var _ Records = (*SQLiteRecords)(nil)
