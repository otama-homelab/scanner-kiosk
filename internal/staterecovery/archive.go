// Package staterecovery implements a bounded logical identity recovery format.
// Sessions, protocol material and temporary document state are never exported.
package staterecovery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"scanner-kiosk/internal/simpleauth"
	"scanner-kiosk/internal/storage"
)

const MaxArchiveBytes = 32 << 20
const maxRecords = 10000

var ErrArchive = errors.New("state recovery archive unavailable or invalid")

type principal struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Created  int64  `json:"created_at_ms"`
	Disabled *int64 `json:"disabled_at_ms"`
}
type identity struct {
	Issuer    string `json:"issuer"`
	Subject   string `json:"subject"`
	Principal string `json:"principal_id"`
}
type credential struct {
	Username  string `json:"username"`
	Principal string `json:"principal_id"`
	Hash      string `json:"password_hash"`
	Version   int64  `json:"credential_version"`
	Created   int64  `json:"created_at_ms"`
	Updated   int64  `json:"updated_at_ms"`
}
type archive struct {
	Format      string       `json:"format"`
	Version     int          `json:"version"`
	StateSchema int          `json:"state_schema"`
	Principals  []principal  `json:"principals"`
	Identities  []identity   `json:"oidc_identities"`
	Credentials []credential `json:"local_credentials"`
}

func (archive) String() string   { return "[redacted state recovery archive]" }
func (archive) GoString() string { return "[redacted state recovery archive]" }

// Export serializes only durable identities/credentials from one consistent
// read transaction. The returned secret bytes belong in a private backup file.
func Export(ctx context.Context, db *sql.DB) ([]byte, error) {
	if db == nil {
		return nil, ErrArchive
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrArchive
	}
	defer tx.Rollback()
	version, err := storage.VerifyCurrent(ctx, tx, storage.State)
	if err != nil || version != 5 {
		return nil, ErrArchive
	}
	a := archive{Format: "scanner-kiosk-state", Version: 1, StateSchema: 5, Principals: []principal{}, Identities: []identity{}, Credentials: []credential{}}
	rows, err := tx.QueryContext(ctx, "SELECT id,kind,created_at_ms,disabled_at_ms FROM principals WHERE kind!='guest' ORDER BY id LIMIT 10001")
	if err != nil {
		return nil, ErrArchive
	}
	for rows.Next() {
		var p principal
		if rows.Scan(&p.ID, &p.Kind, &p.Created, &p.Disabled) != nil {
			rows.Close()
			return nil, ErrArchive
		}
		a.Principals = append(a.Principals, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, ErrArchive
	}
	rows, err = tx.QueryContext(ctx, "SELECT issuer,subject,principal_id FROM oidc_identities ORDER BY issuer,subject LIMIT 10001")
	if err != nil {
		return nil, ErrArchive
	}
	for rows.Next() {
		var i identity
		if rows.Scan(&i.Issuer, &i.Subject, &i.Principal) != nil {
			rows.Close()
			return nil, ErrArchive
		}
		a.Identities = append(a.Identities, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, ErrArchive
	}
	rows, err = tx.QueryContext(ctx, "SELECT username,principal_id,password_hash,credential_version,created_at_ms,updated_at_ms FROM local_credentials ORDER BY username LIMIT 10001")
	if err != nil {
		return nil, ErrArchive
	}
	for rows.Next() {
		var c credential
		if rows.Scan(&c.Username, &c.Principal, &c.Hash, &c.Version, &c.Created, &c.Updated) != nil {
			rows.Close()
			return nil, ErrArchive
		}
		a.Credentials = append(a.Credentials, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || validate(a) != nil {
		return nil, ErrArchive
	}
	data, err := json.Marshal(a)
	if err != nil || len(data)+1 > MaxArchiveBytes || ctx.Err() != nil {
		return nil, ErrArchive
	}
	if tx.Commit() != nil {
		return nil, ErrArchive
	}
	return append(data, '\n'), nil
}

// Restore imports into a checked, empty state database in one transaction.
// The CLI stages this database privately before any final path is published.
func Restore(ctx context.Context, db *sql.DB, data []byte) error {
	if db == nil || len(data) > MaxArchiveBytes || !utf8.Valid(data) || uniqueKeys(data) != nil {
		return ErrArchive
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var a archive
	if decoder.Decode(&a) != nil || validate(a) != nil {
		return ErrArchive
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrArchive
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ErrArchive
	}
	defer tx.Rollback()
	version, err := storage.VerifyCurrent(ctx, tx, storage.State)
	if err != nil || version != 5 {
		return ErrArchive
	}
	// Checking every non-schema table also refuses stale sessions/attempts in an
	// otherwise empty identity database. Never merge or clear an existing state.
	for _, table := range []string{"principals", "oidc_identities", "local_credentials", "sessions", "auth_attempts", "principal_cleanup_requests", "session_revocations", "guest_starts", "oidc_start_requests", "session_login_grants", "simple_session_credentials"} {
		var count int
		if tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count) != nil || count != 0 {
			return ErrArchive
		}
	}
	for _, p := range a.Principals {
		if _, err = tx.ExecContext(ctx, "INSERT INTO principals(id,kind,created_at_ms,disabled_at_ms) VALUES(?,?,?,?)", p.ID, p.Kind, p.Created, p.Disabled); err != nil {
			return ErrArchive
		}
	}
	for _, i := range a.Identities {
		if _, err = tx.ExecContext(ctx, "INSERT INTO oidc_identities(issuer,subject,principal_id) VALUES(?,?,?)", i.Issuer, i.Subject, i.Principal); err != nil {
			return ErrArchive
		}
	}
	for _, c := range a.Credentials {
		if _, err = tx.ExecContext(ctx, "INSERT INTO local_credentials(username,principal_id,password_hash,credential_version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?)", c.Username, c.Principal, c.Hash, c.Version, c.Created, c.Updated); err != nil {
			return ErrArchive
		}
	}
	if tx.Commit() != nil {
		return ErrArchive
	}
	return nil
}

func validate(a archive) error {
	if a.Format != "scanner-kiosk-state" || a.Version != 1 || a.StateSchema != 5 || len(a.Principals) > maxRecords || len(a.Identities) > maxRecords || len(a.Credentials) > maxRecords {
		return ErrArchive
	}
	principals := map[string]principal{}
	bound := map[string]bool{}
	identities := map[string]bool{}
	names := map[string]bool{}
	for _, p := range a.Principals {
		if len(p.ID) < 16 || len(p.ID) > 256 || p.Created < 0 || (p.Disabled != nil && *p.Disabled < p.Created) || (p.Kind != "oidc" && p.Kind != "simple") {
			return ErrArchive
		}
		for _, r := range p.ID {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return ErrArchive
			}
		}
		if _, found := principals[p.ID]; found {
			return ErrArchive
		}
		principals[p.ID] = p
	}
	for _, i := range a.Identities {
		p, found := principals[i.Principal]
		key := i.Issuer + "\x00" + i.Subject
		if !found || p.Kind != "oidc" || bound[i.Principal] || identities[key] || !text(i.Issuer) || !text(i.Subject) {
			return ErrArchive
		}
		bound[i.Principal] = true
		identities[key] = true
	}
	for _, c := range a.Credentials {
		p, found := principals[c.Principal]
		name, err := simpleauth.NormalizeUsername(c.Username)
		if !found || p.Kind != "simple" || bound[c.Principal] || names[c.Username] || err != nil || name != c.Username || c.Version < 1 || c.Created < p.Created || c.Updated < c.Created {
			return ErrArchive
		}
		if _, err = simpleauth.ParsePasswordHash(c.Hash); err != nil {
			return ErrArchive
		}
		bound[c.Principal] = true
		names[c.Username] = true
	}
	if len(bound) != len(principals) {
		return ErrArchive
	}
	return nil
}
func text(s string) bool {
	return s != "" && len(s) <= 4096 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}

// Reject ambiguous duplicate JSON keys as well as excessive nesting before
// decoding credentials. This is a versioned archive, not a general JSON API.
func uniqueKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	values := 0
	walk = func(depth int) error {
		values++
		if depth > 8 || values > 250000 {
			return ErrArchive
		}
		token, err := d.Token()
		if err != nil {
			return ErrArchive
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					s, ok := key.(string)
					if err != nil || !ok || seen[s] {
						return ErrArchive
					}
					// Prevent encoding/json's case-folded struct field aliases.
					// Recovery archives use exact lowercase ASCII wire keys.
					for _, r := range s {
						if r != '_' && (r < 'a' || r > 'z') {
							return ErrArchive
						}
					}
					seen[s] = true
					if walk(depth+1) != nil {
						return ErrArchive
					}
				}
				if t, err := d.Token(); err != nil || t != json.Delim('}') {
					return ErrArchive
				}
			case '[':
				for d.More() {
					if walk(depth+1) != nil {
						return ErrArchive
					}
				}
				if t, err := d.Token(); err != nil || t != json.Delim(']') {
					return ErrArchive
				}
			default:
				return ErrArchive
			}
		}
		return nil
	}
	if walk(0) != nil {
		return ErrArchive
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrArchive
	}
	return nil
}
