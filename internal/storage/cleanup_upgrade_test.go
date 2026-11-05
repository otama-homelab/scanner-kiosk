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

func TestCleanupAttemptUpgradePreservesManifestAndOldAuthority(t *testing.T) {
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
	for i, file := range files[:13] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO owners(id) VALUES(?)", alice)
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','deleting',1000,10000)", docA, alice)
	const job = "job_cleanup_upgrade_01"
	const old = "worker_cleanup_upgrade_01"
	must(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES(?,?,?,'cleanup','running',1,'{"version":1}',2000,20000,?,2000,2100)`, job, docA, alice, old)
	for _, id := range []string{"artifact_upgrade_01", "artifact_upgrade_02"} {
		artifact(t, db, id, docA, alice, "original", 1)
		must(t, db, "UPDATE artifacts SET state='deleting' WHERE id=?", id)
	}
	must(t, db, "INSERT INTO cleanup_files(job_id,owner_id,artifact_id,relative_path,byte_size,sha256) SELECT ?,owner_id,id,relative_path,byte_size,sha256 FROM artifacts", job)
	must(t, db, "UPDATE cleanup_files SET state='deleted',deleted_at_ms=2100 WHERE artifact_id='artifact_upgrade_01'")
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	for query, wanted := range map[string]int{
		"SELECT count(*) FROM schema_migrations": 18,
		"SELECT count(*) FROM cleanup_attempts":  0,
		"SELECT count(*) FROM jobs WHERE state='running' AND runner_id='worker_cleanup_upgrade_01' AND started_at_ms=2000 AND deadline_at_ms=20000": 1,
		"SELECT count(*) FROM cleanup_files WHERE state='pending'":                                                                                  1,
		"SELECT count(*) FROM cleanup_files WHERE state='deleted' AND deleted_at_ms=2100":                                                           1,
		"SELECT COALESCE(sum(byte_size),0) FROM artifacts WHERE state='deleting'":                                                                   200,
	} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != wanted {
			t.Fatal(query, n, err)
		}
	}
}
