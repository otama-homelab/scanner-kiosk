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

func TestScannerReadinessUpgradePreservesHeldWorkWithoutCreatingAuthority(t *testing.T) {
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
	for i, file := range files[:7] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO owners(id) VALUES(?)", alice)
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','scanning',1000,10000)", docA, alice)
	must(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id) VALUES('job_ready_upgrade_001',?,?,'scan','queued',1,'{}',1000,9000,'worker_ready_upgrade')`, docA, alice)
	must(t, db, "INSERT INTO device_leases(device_id,epoch,job_id,state) VALUES('scanner_ready_upgrade',2,'job_ready_upgrade_001','held')")
	must(t, db, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES('job_ready_upgrade_001',?,50)", alice)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, wanted := range map[string]int{"SELECT count(*) FROM page_thumbnails": 0, "SELECT count(*) FROM scanner_readiness": 0, "SELECT count(*) FROM schema_migrations": 16, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2": 1, "SELECT count(*) FROM jobs WHERE state='queued' AND runner_id='worker_ready_upgrade'": 1, "SELECT count(*) FROM reservations WHERE byte_size=50": 1} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != wanted {
			t.Fatal("readiness migration changed existing work", query, n, err)
		}
	}
}
