package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OpenReadOnly opens an existing private file without creating/migrating it or
// changing journal mode. The caller verifies schema inside its read snapshot.
func OpenReadOnly(path string) (*sql.DB, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private existing database required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	address := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{"mode": {"ro"}, "_txlock": {"deferred"}, "_pragma": {"query_only(1)", "foreign_keys(1)", "busy_timeout(5000)"}}
	address.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", address.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

type schemaReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// VerifyCurrent requires every current migration's exact name/checksum. Unlike
// Open, it never upgrades older schemas. Use the caller's transaction snapshot.
func VerifyCurrent(ctx context.Context, reader schemaReader, kind Kind) (int, error) {
	fail := errors.New("database schema is not the current checked schema")
	if reader == nil || kind != State && kind != Documents {
		return 0, fail
	}
	var actual string
	if err := reader.QueryRowContext(ctx, "SELECT kind FROM database_identity WHERE singleton=1").Scan(&actual); err != nil || actual != string(kind) {
		return 0, fail
	}
	names, err := fs.Glob(migrations, "migrations/"+string(kind)+"/*.sql")
	if err != nil {
		return 0, fail
	}
	rows, err := reader.QueryContext(ctx, "SELECT version,name,sha256 FROM schema_migrations ORDER BY version")
	if err != nil {
		return 0, fail
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var version int
		var name, digest string
		if rows.Scan(&version, &name, &digest) != nil || count >= len(names) {
			return 0, fail
		}
		expected := filepath.Base(names[count])
		wanted, err := strconv.Atoi(strings.SplitN(expected, "_", 2)[0])
		body, readErr := migrations.ReadFile(names[count])
		if err != nil || readErr != nil || version != wanted || name != expected || digest != fmt.Sprintf("%x", sha256.Sum256(body)) {
			return 0, fail
		}
		count++
	}
	if rows.Err() != nil || count != len(names) {
		return 0, fail
	}
	return count, nil
}
