package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func importUpgradeFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	must(t, db, `CREATE TABLE database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL);INSERT INTO database_identity VALUES(1,'documents');CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	files, err := fs.ReadDir(migrations, "migrations/documents")
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range files[:17] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO owners VALUES(?)", alice)
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,10000)", docA, alice)
	artifact(t, db, "original_import_upgrade", docA, alice, "original", 1)
	must(t, db, pageSQL(), "page_import_upgrade1", docA, alice, 0, "original_import_upgrade")
	must(t, db, jobSQL(), "job_import_upgrade01", docA, alice, "scan")
	must(t, db, "INSERT INTO reservations VALUES(?,?,100)", "job_import_upgrade01", alice)
	must(t, db, "INSERT INTO device_leases VALUES('scanner_import_upgrade',1,'job_import_upgrade01','held')")
	must(t, db, "INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,zeroblob(32),'scan-new',zeroblob(32),'job_import_upgrade01',10000)", alice)
	must(t, db, "INSERT INTO job_artifacts VALUES(?,?,?,?)", "job_import_upgrade01", docA, alice, "original_import_upgrade")
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_cleanup_upgrade',?,'document','deleting',1000,10000)", alice)
	must(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms) VALUES('job_cleanup_upgrade1','document_cleanup_upgrade',?,'cleanup','running',1,'{}',2000,20000,'runner_cleanup_upgrade',2000)`, alice)
	artifact(t, db, "cleanup_import_artifact", "document_cleanup_upgrade", alice, "original", 1)
	must(t, db, "UPDATE artifacts SET state='deleting' WHERE id='cleanup_import_artifact'")
	must(t, db, "INSERT INTO cleanup_files(job_id,owner_id,artifact_id,relative_path,byte_size,sha256) SELECT 'job_cleanup_upgrade1',owner_id,id,relative_path,byte_size,sha256 FROM artifacts WHERE id='cleanup_import_artifact'")
	must(t, db, "INSERT INTO cleanup_attempts VALUES('job_cleanup_upgrade1',?,'runner_cleanup_upgrade',1,2000,3000)", alice)
	return path, db
}
func TestImportJobUpgradeRetainsCompleteGraph(t *testing.T) {
	path, db := importUpgradeFixture(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{
		"SELECT count(*) FROM jobs": 2,
		"SELECT count(*) FROM reservations WHERE job_id='job_import_upgrade01' AND byte_size=100":    1,
		"SELECT count(*) FROM device_leases WHERE job_id='job_import_upgrade01' AND state='held'":    1,
		"SELECT count(*) FROM request_receipts WHERE result_job_id='job_import_upgrade01'":           1,
		"SELECT count(*) FROM job_artifacts WHERE job_id='job_import_upgrade01'":                     1,
		"SELECT count(*) FROM cleanup_files WHERE job_id='job_cleanup_upgrade1' AND state='pending'": 1,
		"SELECT count(*) FROM cleanup_attempts WHERE job_id='job_cleanup_upgrade1' AND generation=1": 1,
		"SELECT count(*) FROM pragma_foreign_key_check":                                              0,
		"PRAGMA foreign_keys":       1,
		"PRAGMA legacy_alter_table": 0,
	} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Fatal(query, n, err)
		}
	}
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_import_new1',?,'document','processing',1000,10000)", alice)
	must(t, db, jobSQL(), "job_import_new_0001", "document_import_new1", alice, "import")
	rejects(t, db, "DELETE FROM jobs WHERE id='job_import_upgrade01'")
	rejects(t, db, "UPDATE jobs SET kind='scan' WHERE id='job_import_new_0001'")
	rejects(t, db, "UPDATE jobs SET started_at_ms=2100 WHERE id='job_cleanup_upgrade1'")
	must(t, db, "DELETE FROM jobs WHERE id='job_cleanup_upgrade1'")
	for _, table := range []string{"cleanup_files", "cleanup_attempts"} {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("cascade not restored", table, n, err)
		}
	}
}
func TestImportJobUpgradeRollsBackInvalidGraph(t *testing.T) {
	path, db := importUpgradeFixture(t)
	must(t, db, "PRAGMA foreign_keys=OFF")
	must(t, db, "INSERT INTO reservations VALUES('missing_job_upgrade1',?,100)", alice)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if upgraded, err := Open(path, Documents); err == nil {
		upgraded.Close()
		t.Fatal("invalid graph accepted")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = apply(context.Background(), db, Documents); err == nil {
		t.Fatal("invalid graph accepted on existing connection")
	}
	var foreignKeys, legacy int
	if err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("failed migration left FK checks disabled", foreignKeys, err)
	}
	if err = db.QueryRow("PRAGMA legacy_alter_table").Scan(&legacy); err != nil || legacy != 0 {
		t.Fatal("failed migration left legacy rename enabled", legacy, err)
	}
	for query, want := range map[string]int{"SELECT count(*) FROM schema_migrations": 17, "SELECT count(*) FROM jobs": 2, "SELECT count(*) FROM reservations": 2, "SELECT count(*) FROM request_receipts WHERE result_job_id='job_import_upgrade01'": 1, "SELECT count(*) FROM sqlite_master WHERE name='jobs_next'": 0} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Fatal("failed migration changed data", query, n, err)
		}
	}
}
