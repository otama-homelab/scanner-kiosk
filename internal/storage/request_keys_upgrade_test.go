package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAgedRequestMigrationPreservesLegacyAndMonotonicFence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	must(t, db, `CREATE TABLE database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL);INSERT INTO database_identity VALUES(1,'documents');CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	files, err := fs.ReadDir(migrations, "migrations/documents")
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range files[:15] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO owners(id) VALUES(?)", alice)
	must(t, db, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms) VALUES(?,zeroblob(32),'scan-new',zeroblob(32),1)`, alice)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM request_receipts WHERE key_expires_at_ms IS NULL AND expires_at_ms=1").Scan(&count); err != nil || count != 1 {
		t.Fatal("legacy tombstone changed", count, err)
	}
	must(t, db, "UPDATE request_retirement_fence SET retired_at_ms=1000")
	for _, query := range []string{"UPDATE request_retirement_fence SET retired_at_ms=999", "DELETE FROM request_retirement_fence", "UPDATE request_receipts SET key_expires_at_ms=86400000"} {
		if _, err = db.Exec(query); err == nil {
			t.Fatal("mutable retirement evidence", query)
		}
	}
}

func TestAgedRequestMigrationFailureRollsBackAddedColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	must(t, db, `CREATE TABLE database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL);INSERT INTO database_identity VALUES(1,'documents');CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	files, err := fs.ReadDir(migrations, "migrations/documents")
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range files[:15] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "CREATE TABLE request_retirement_fence(fixture INTEGER)")
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path, Documents); err == nil {
		opened.Close()
		t.Fatal("conflicting migration succeeded")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{"SELECT count(*) FROM schema_migrations": 15, "SELECT count(*) FROM pragma_table_info('request_receipts') WHERE name='key_expires_at_ms'": 0} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != want {
			t.Fatal("partial migration survived", query, count, err)
		}
	}
}
