// Package storage opens the two retention-separated SQLite stores and applies checked migrations.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
)

type Kind string

const (
	State     Kind = "state"
	Documents Kind = "documents"
)

//go:embed migrations/*/*.sql
var migrations embed.FS

// Open creates a private local file, enables constraints for every connection,
// and serializes migrations with an IMMEDIATE transaction. API startup is not implemented here.
func Open(path string, kind Kind) (*sql.DB, error) {
	if kind != State && kind != Documents {
		return nil, errors.New("unknown database kind")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	if info, e := os.Lstat(absolute); e == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("database must be a private regular file")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	address := url.URL{Scheme: "file", Path: absolute}
	query := url.Values{}
	query.Set("_txlock", "immediate")
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)", "secure_delete(ON)"} {
		query.Add("_pragma", pragma)
	}
	address.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", address.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = applyWithBusyRetry(ctx, db, kind); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// SQLite busy_timeout does not cover every journal-mode change during a first connection.
// Retry only SQLite lock contention; each migration attempt is its own atomic transaction.
func applyWithBusyRetry(ctx context.Context, db *sql.DB, kind Kind) error {
	delay := 10 * time.Millisecond
	for {
		err := apply(ctx, db, kind)
		if err == nil {
			return nil
		}
		var sqliteError *sqlite.Error
		if !errors.As(err, &sqliteError) || (sqliteError.Code()&0xff != 5 && sqliteError.Code()&0xff != 6) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
			if delay > 100*time.Millisecond {
				delay = 100 * time.Millisecond
			}
		}
	}
}

func apply(ctx context.Context, db *sql.DB, kind Kind) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// SQLite table replacement requires FK checks off before BEGIN; validate the
	// entire graph before commit and restore enforcement before returning the connection.
	defer func() {
		restore, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, restoreErr := conn.ExecContext(restore, "PRAGMA legacy_alter_table=OFF; PRAGMA foreign_keys=ON")
		var enabled int
		if restoreErr == nil {
			restoreErr = conn.QueryRowContext(restore, "PRAGMA foreign_keys").Scan(&enabled)
		}
		if restoreErr == nil && enabled != 1 {
			restoreErr = errors.New("foreign-key enforcement could not be restored")
		}
		if restoreErr != nil {
			err = errors.Join(err, restoreErr)
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL); CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO database_identity(singleton,kind) VALUES(1,?)", string(kind)); err != nil {
		return err
	}
	var actual string
	if err = tx.QueryRowContext(ctx, "SELECT kind FROM database_identity WHERE singleton=1").Scan(&actual); err != nil {
		return err
	}
	if actual != string(kind) {
		return errors.New("wrong database kind")
	}
	names, err := fs.Glob(migrations, "migrations/"+string(kind)+"/*.sql")
	if err != nil {
		return err
	}
	known := make(map[int]string, len(names))
	for _, name := range names {
		base := filepath.Base(name)
		version, e := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if e != nil {
			return e
		}
		if _, ok := known[version]; ok {
			return errors.New("duplicate migration version")
		}
		known[version] = name
	}
	rows, err := tx.QueryContext(ctx, "SELECT version,name,sha256 FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	existing := map[int]bool{}
	for rows.Next() {
		var version int
		var name, digest string
		if err = rows.Scan(&version, &name, &digest); err != nil {
			rows.Close()
			return err
		}
		path, ok := known[version]
		if !ok {
			rows.Close()
			return errors.New("database schema newer than executable")
		}
		body, e := migrations.ReadFile(path)
		if e != nil {
			rows.Close()
			return e
		}
		if name != filepath.Base(path) || digest != fmt.Sprintf("%x", sha256.Sum256(body)) {
			rows.Close()
			return errors.New("migration checksum mismatch")
		}
		existing[version] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range names {
		version, _ := strconv.Atoi(strings.SplitN(filepath.Base(name), "_", 2)[0])
		if existing[version] {
			continue
		}
		body, e := migrations.ReadFile(name)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %d: %w", version, err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,sha256,applied_at_ms) VALUES(?,?,?,?)", version, filepath.Base(name), fmt.Sprintf("%x", sha256.Sum256(body)), time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	rows, err = tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	checkErr := rows.Err()
	closeErr := rows.Close()
	if checkErr != nil || closeErr != nil {
		return errors.Join(checkErr, closeErr)
	}
	if invalid {
		return errors.New("migration would leave invalid foreign-key references")
	}
	return tx.Commit()
}
