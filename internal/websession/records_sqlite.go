package websession

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLiteRecords implements session commands with explicit SQLite SQL. It borrows
// the opened state DB. Other backends implement Records rather than this schema.
type SQLiteRecords struct{ db *sql.DB }

func NewSQLiteRecords(db *sql.DB) (*SQLiteRecords, error) {
	if db == nil {
		return nil, ErrSession
	}
	return &SQLiteRecords{db: db}, nil
}

var _ Records = (*SQLiteRecords)(nil)

func (s *SQLiteRecords) FindSession(ctx context.Context, token string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM sessions WHERE token=? AND expiry>julianday('now') AND NOT EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=sessions.token)`, token).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}
func (s *SQLiteRecords) CommitSession(ctx context.Context, record SessionData) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token,data,expiry) VALUES(?,?,julianday(?)) ON CONFLICT(token) DO UPDATE SET data=excluded.data,expiry=excluded.expiry`, record.TokenHash, record.Data, record.Expiry.UTC().Format("2006-01-02T15:04:05.999"))
	return err
}
func (s *SQLiteRecords) RotateLogin(ctx context.Context, grant LoginGrantRecord, retention time.Duration) error {
	if !validLoginKind(grant.Kind, grant.CredentialVersion) {
		return ErrSession
	}
	token := grant.SourceHash
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkLoginIdentity(ctx, tx, grant); err != nil {
		return err
	}
	now := time.Now()

	var active int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions s WHERE token=? AND expiry>julianday('now') AND NOT EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=s.token)`, token).Scan(&active); err != nil {
		return ErrSession
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO session_login_grants(source_hash,grant_nonce,created_at_ms,expires_at_ms) VALUES(?,?,?,?)", token, grant.Nonce, now.UnixMilli(), now.Add(retention).UnixMilli()); err != nil {
		return err
	}
	if err = s.revokeTx(ctx, tx, token, now, retention); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
func (s *SQLiteRecords) RevokeSession(ctx context.Context, token string, retention time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()

	var successor sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT target_hash FROM session_login_grants WHERE source_hash=?", token).Scan(&successor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE session_login_grants SET cancelled_at_ms=COALESCE(cancelled_at_ms,MAX(?,created_at_ms)) WHERE source_hash=? OR target_hash=?", now.UnixMilli(), token, token); err != nil {
		return err
	}
	if err = s.revokeTx(ctx, tx, token, now, retention); err != nil {
		return err
	}
	if successor.Valid {
		if err = s.revokeTx(ctx, tx, successor.String, now, retention); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *SQLiteRecords) CommitLogin(ctx context.Context, record SessionData, grant LoginGrantRecord) error {
	if !validLoginKind(grant.Kind, grant.CredentialVersion) {
		return ErrSession
	}
	token, data, expiry := record.TokenHash, record.Data, record.Expiry
	if token == grant.SourceHash {
		return ErrSession
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkLoginIdentity(ctx, tx, grant); err != nil {
		return err
	}
	var active int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM session_login_grants g WHERE g.source_hash=? AND g.grant_nonce=? AND g.target_hash IS NULL AND g.cancelled_at_ms IS NULL AND g.expires_at_ms>?`, grant.SourceHash, grant.Nonce, time.Now().UnixMilli()).Scan(&active)
	if err != nil {
		return ErrSession
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(token,data,expiry) VALUES(?,?,julianday(?))", token, data, expiry.UTC().Format("2006-01-02T15:04:05.999")); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE session_login_grants SET target_hash=? WHERE source_hash=? AND grant_nonce=? AND target_hash IS NULL AND cancelled_at_ms IS NULL", token, grant.SourceHash, grant.Nonce)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrSession
	}
	if grant.Kind == "simple" {
		if _, err := tx.ExecContext(ctx, "INSERT INTO simple_session_credentials(session_hash,principal_id,credential_version) VALUES(?,?,?)", token, grant.PrincipalID, grant.CredentialVersion); err != nil {
			return ErrSession
		}
	}
	return tx.Commit()
}
func (s *SQLiteRecords) revokeTx(ctx context.Context, tx *sql.Tx, token string, now time.Time, retention time.Duration) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_revocations(token,revoked_at_ms,retain_until_ms) VALUES(?,?,?) ON CONFLICT(token) DO NOTHING`, token, now.UnixMilli(), now.Add(retention).UnixMilli()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE token=?", token)
	return err
}
func (s *SQLiteRecords) KnownSession(ctx context.Context, token string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE token=?) OR EXISTS(SELECT 1 FROM session_revocations WHERE token=?)", token, token).Scan(&count)
	return count != 0, err
}
func (s *SQLiteRecords) CreateGuest(ctx context.Context, record GuestStartRecord) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var principal string
	var previousKey []byte
	var expires int64
	var disabled sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT g.principal_id,g.key_hash,g.expires_at_ms,p.disabled_at_ms FROM guest_starts g JOIN principals p ON p.id=g.principal_id WHERE g.binding_hash=?`, record.BindingHash[:]).Scan(&principal, &previousKey, &expires, &disabled)
	now := time.Now()
	if err == nil {
		if disabled.Valid || expires <= now.UnixMilli() {
			return "", ErrGone
		}
		if string(previousKey) != string(record.KeyHash[:]) {
			return "", ErrConflict
		}
		return principal, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	principal = record.ProposedPrincipalID
	if _, err = tx.ExecContext(ctx, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',?)", principal, now.UnixMilli()); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO guest_starts(binding_hash,key_hash,principal_id,created_at_ms,expires_at_ms) VALUES(?,?,?,?,?)", record.BindingHash[:], record.KeyHash[:], principal, now.UnixMilli(), now.Add(record.Retention).UnixMilli()); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return principal, nil
}
func (s *SQLiteRecords) DisableGuest(ctx context.Context, principal string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, "UPDATE principals SET disabled_at_ms=COALESCE(disabled_at_ms,?) WHERE id=? AND kind='guest'", now, principal)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrSession
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO principal_cleanup_requests(principal_id,reason,requested_at_ms) VALUES(?,'guest_end',?) ON CONFLICT(principal_id) DO NOTHING`, principal, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteRecords) PrincipalKind(ctx context.Context, principal string) (string, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, "SELECT kind FROM principals WHERE id=? AND disabled_at_ms IS NULL", principal).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSession
	}
	return kind, err
}
func (s *SQLiteRecords) AuthorizeSession(ctx context.Context, hash, principal, kind string) error {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions s JOIN principals p ON p.id=? AND p.kind=? AND p.disabled_at_ms IS NULL WHERE s.token=? AND s.expiry>julianday('now') AND NOT EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=s.token) AND (p.kind!='simple' OR EXISTS(SELECT 1 FROM simple_session_credentials b JOIN local_credentials c ON c.principal_id=b.principal_id AND c.credential_version=b.credential_version WHERE b.session_hash=s.token AND b.principal_id=p.id))`, principal, kind, hash).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSession
	}
	return err
}

// New preserves the SQLite convenience constructor. Composition roots may wire
// any Records implementation explicitly with NewWithRecords.
func New(db *sql.DB, options Options) (*Handler, error) {
	records, err := NewSQLiteRecords(db)
	if err != nil {
		return nil, err
	}
	return NewWithRecords(records, options)
}

func checkLoginIdentity(ctx context.Context, tx *sql.Tx, grant LoginGrantRecord) error {
	var active int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM principals p WHERE p.id=? AND p.kind=? AND p.disabled_at_ms IS NULL
 AND (p.kind!='simple' OR EXISTS(SELECT 1 FROM local_credentials c WHERE c.principal_id=p.id AND c.credential_version=?))`, grant.PrincipalID, grant.Kind, grant.CredentialVersion).Scan(&active)
	if err != nil {
		return ErrSession
	}
	return nil
}
