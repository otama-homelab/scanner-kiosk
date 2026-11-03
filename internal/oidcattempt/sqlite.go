package oidcattempt

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
)

// SQLiteRecords owns the active-browser check, request tombstone/limit and
// one-time consumption transaction. No Cookie or token exchange is performed.
type SQLiteRecords struct {
	db     *sql.DB
	commit func(*sql.Tx) error
}

func NewSQLiteRecords(db *sql.DB) (*SQLiteRecords, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &SQLiteRecords{db: db, commit: func(tx *sql.Tx) error { return tx.Commit() }}, nil
}
func New(db *sql.DB, options Options) (*Store, error) {
	records, err := NewSQLiteRecords(db)
	if err != nil {
		return nil, err
	}
	return NewWithRecords(records, options)
}
func activeSession(ctx context.Context, tx *sql.Tx, hash [32]byte) error {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions s WHERE s.token=? AND s.expiry>julianday('now') AND NOT EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=s.token)`, base64.RawURLEncoding.EncodeToString(hash[:])).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return ErrStorage
	}
	return nil
}

func (s *SQLiteRecords) Begin(ctx context.Context, command BeginRecord) (StoredMaterial, error) {
	if command.NowMS < 0 || command.MaxPending < 1 || command.MaxPending > 16 || command.Proposed.CreatedAtMS != command.NowMS || command.Proposed.ExpiresAtMS <= command.NowMS || command.Proposed.ExpiresAtMS-command.NowMS > 600000 || !token(command.Proposed.Nonce) || !token(command.Proposed.Verifier) {
		return StoredMaterial{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredMaterial{}, ErrStorage
	}
	defer tx.Rollback()
	binding := command.BrowserHash
	keyHash := command.KeyHash
	configuration := command.ConfigurationHash
	recordRequest := command.RecordRequest
	if err = activeSession(ctx, tx, binding); err != nil {
		return StoredMaterial{}, err
	}
	now := command.NowMS
	if now < 0 {
		return StoredMaterial{}, ErrInvalid
	}
	if recordRequest {
		var priorConfiguration, priorState []byte
		var created, expires int64
		var nonce, verifier sql.NullString
		var consumed sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT r.configuration_hash,r.state_hash,r.created_at_ms,r.expires_at_ms,a.nonce,a.code_verifier,a.consumed_at_ms FROM oidc_start_requests r LEFT JOIN auth_attempts a ON a.state_hash=r.state_hash WHERE r.session_hash=? AND r.key_hash=?`, binding[:], keyHash[:]).Scan(&priorConfiguration, &priorState, &created, &expires, &nonce, &verifier, &consumed)
		if err == nil {
			if subtle.ConstantTimeCompare(priorConfiguration, configuration[:]) != 1 {
				return StoredMaterial{}, ErrConflict
			}
			if created > now || expires <= now || consumed.Valid || !nonce.Valid || !verifier.Valid || !token(nonce.String) || !token(verifier.String) {
				return StoredMaterial{}, ErrGone
			}
			state := requestState(verifier.String, binding, keyHash, configuration)
			digest := sha256.Sum256([]byte(state))
			if subtle.ConstantTimeCompare(digest[:], priorState) != 1 {
				return StoredMaterial{}, ErrDenied
			}
			if err = s.commit(tx); err != nil {
				return StoredMaterial{}, ErrUncertain
			}
			return StoredMaterial{StateHash: digest, Nonce: nonce.String, Verifier: verifier.String, CreatedAtMS: created, ExpiresAtMS: expires}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return StoredMaterial{}, ErrStorage
		}
	}
	var pending int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM auth_attempts WHERE session_hash=? AND consumed_at_ms IS NULL AND expires_at_ms>?", binding[:], now).Scan(&pending); err != nil {
		return StoredMaterial{}, ErrStorage
	}
	if pending >= command.MaxPending {
		return StoredMaterial{}, ErrLimit
	}
	digest := command.Proposed.StateHash
	nonce, verifier := command.Proposed.Nonce, command.Proposed.Verifier
	if _, err = tx.ExecContext(ctx, "INSERT INTO auth_attempts(state_hash,session_hash,nonce,code_verifier,created_at_ms,expires_at_ms) VALUES(?,?,?,?,?,?)", digest[:], binding[:], nonce, verifier, now, command.Proposed.ExpiresAtMS); err != nil {
		return StoredMaterial{}, ErrStorage
	}
	if recordRequest {
		if _, err = tx.ExecContext(ctx, "INSERT INTO oidc_start_requests(session_hash,key_hash,configuration_hash,state_hash,created_at_ms,expires_at_ms) VALUES(?,?,?,?,?,?)", binding[:], keyHash[:], configuration[:], digest[:], now, command.Proposed.ExpiresAtMS); err != nil {
			return StoredMaterial{}, ErrStorage
		}
	}
	if err = s.commit(tx); err != nil {
		return StoredMaterial{}, ErrUncertain
	}
	return command.Proposed, nil
}

func (s *SQLiteRecords) Consume(ctx context.Context, command ConsumeRecord) (StoredMaterial, error) {
	if command.NowMS < 0 {
		return StoredMaterial{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredMaterial{}, ErrStorage
	}
	defer tx.Rollback()
	binding := command.BrowserHash
	configuration := command.ConfigurationHash
	requireRequest := command.RequireRequest
	if err = activeSession(ctx, tx, binding); err != nil {
		return StoredMaterial{}, err
	}
	digest := command.StateHash
	if requireRequest {
		var found int
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM oidc_start_requests WHERE session_hash=? AND state_hash=? AND configuration_hash=?", binding[:], digest[:], configuration[:]).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return StoredMaterial{}, ErrDenied
		}
		if err != nil {
			return StoredMaterial{}, ErrStorage
		}
	}
	now := command.NowMS
	var nonce, verifier string
	var created, expires int64
	err = tx.QueryRowContext(ctx, `UPDATE auth_attempts SET consumed_at_ms=? WHERE state_hash=? AND session_hash=? AND consumed_at_ms IS NULL AND created_at_ms<=? AND expires_at_ms>? RETURNING nonce,code_verifier,created_at_ms,expires_at_ms`, now, digest[:], binding[:], now, now).Scan(&nonce, &verifier, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredMaterial{}, ErrDenied
	}
	if err != nil {
		return StoredMaterial{}, ErrStorage
	}
	if !token(nonce) || !token(verifier) {
		return StoredMaterial{}, ErrDenied
	}
	if err = s.commit(tx); err != nil {
		return StoredMaterial{}, ErrUncertain
	}
	return StoredMaterial{StateHash: digest, Nonce: nonce, Verifier: verifier, CreatedAtMS: created, ExpiresAtMS: expires}, nil
}

var _ Records = (*SQLiteRecords)(nil)
