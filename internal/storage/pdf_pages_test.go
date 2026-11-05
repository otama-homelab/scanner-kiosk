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

func TestPDFPageSchemaAndCapturedUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	files, err := fs.ReadDir(migrations, "migrations/documents")
	if err != nil {
		t.Fatal(err)
	}
	must(t, db, `CREATE TABLE database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL);INSERT INTO database_identity VALUES(1,'documents');CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	for i, file := range files[:16] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO owners VALUES(?)", alice)
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,86401000)", docA, alice)
	artifact(t, db, "original_pdf_upgrade_01", docA, alice, "original", 1)
	must(t, db, pageSQL(), "page_pdf_upgrade_001", docA, alice, 0, "original_pdf_upgrade_01")
	must(t, db, jobSQL(), "job_pdf_upgrade_0001", docA, alice, "scan")
	must(t, db, "INSERT INTO reservations VALUES(?,?,100)", "job_pdf_upgrade_0001", alice)
	must(t, db, "INSERT INTO device_leases VALUES('scanner_pdf_upgrade1',1,'job_pdf_upgrade_0001','held')")
	must(t, db, "INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms,key_expires_at_ms) VALUES(?,zeroblob(32),'scan-new',zeroblob(32),'job_pdf_upgrade_0001',86402000,86402000)", alice)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{
		"SELECT count(*) FROM pages WHERE id='page_pdf_upgrade_001' AND source_type='scan' AND capture_size='a4' AND capture_dpi=300 AND pdf_width_points IS NULL": 1,
		"SELECT count(*) FROM jobs WHERE id='job_pdf_upgrade_0001' AND state='queued'":                                                                             1,
		"SELECT count(*) FROM reservations WHERE byte_size=100":                                                                                                    1,
		"SELECT count(*) FROM device_leases WHERE state='held'":                                                                                                    1,
		"SELECT count(*) FROM request_receipts WHERE result_job_id='job_pdf_upgrade_0001'":                                                                         1,
		"SELECT count(*) FROM pragma_foreign_key_check":                                                                                                            0,
	} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Fatal(query, n, err)
		}
	}
	// A PDF original is immutable once ready: create it with its real media type.
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_pdf_source1',?,?,'original','ready','pdf/source','application/pdf',100,zeroblob(32),1,1001)`, docA, alice)
	insert := `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES(?,?,?,?,'original_pdf_source1','pdf',?,?)`
	must(t, db, insert, "page_pdf_source_0001", docA, alice, 1, 200.0, 300.0)
	rejects(t, db, "UPDATE pages SET pdf_width_points=201 WHERE id='page_pdf_source_0001'")
	rejects(t, db, "UPDATE pages SET capture_size='a4' WHERE id='page_pdf_source_0001'")
	rejects(t, db, insert, "page_pdf_bad_dim_001", docA, alice, 2, 0, 300)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points,capture_size) VALUES('page_pdf_fake_0001',?,?,2,'original_pdf_source1','pdf',200,300,'a4')`, docA, alice)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id) VALUES('page_scan_missing01',?,?,2,'original_pdf_upgrade_01')`, docA, alice)
}
