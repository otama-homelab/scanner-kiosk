package storage

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhotoImportPurposeUpgradeFrom26(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	db, e := Open(path, Documents)
	if e != nil {
		t.Fatal(e)
	}
	// Reconstruct the exact v26 trigger/ledger; v27 changes no tables or data.
	old, e := migrations.ReadFile("migrations/documents/0025_photo_sources.sql")
	if e != nil {
		t.Fatal(e)
	}
	trigger := strings.Split(strings.Split(string(old), "CREATE TRIGGER page_photo_source ")[1], "CREATE TRIGGER photo_source_ready")[0]
	must(t, db, "DROP TRIGGER page_photo_source; CREATE TRIGGER page_photo_source "+trigger)
	must(t, db, "DELETE FROM schema_migrations WHERE version=27")
	must(t, db, "INSERT INTO owners VALUES(?)", alice)
	must(t, db, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_v26_existing',?,'document','review',1000,10000)`, alice)
	before := snapshotTable(t, db, "documents")
	db.Close()
	db, e = Open(path, Documents)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = Open(path, Documents)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if !bytes.Equal(before, snapshotTable(t, db, "documents")) {
		t.Fatal("upgrade changed existing documents")
	}
	for q, want := range map[string]int{"SELECT count(*) FROM pragma_foreign_key_check": 0, "SELECT count(*) FROM schema_migrations WHERE version=27": 1} {
		var n int
		if e = db.QueryRow(q).Scan(&n); e != nil || n != want {
			t.Fatal(q, n, e)
		}
	}
	must(t, db, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_photo_import',?,'photo','review',1000,10000)`, alice)
	artifact(t, db, "photo_import_raw001", "document_photo_import", alice, "original", 1)
	artifact(t, db, "photo_import_norm01", "document_photo_import", alice, "intermediate", 1)
	must(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES('photo_import_page01','document_photo_import',?,0,'photo_import_raw001','photo',20,30)`, alice)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES('photo_import_page02','document_photo_import',?,1,'photo_import_raw001','photo',20,30)`, alice)
	rejects(t, db, `INSERT INTO photo_sources VALUES('photo_import_page01','document_photo_import',?,'photo_import_raw001','photo_import_norm01',randomblob(32),zeroblob(32),20,30,1)`, alice)
	must(t, db, `INSERT INTO photo_sources VALUES('photo_import_page01','document_photo_import',?,'photo_import_raw001','photo_import_norm01',zeroblob(32),zeroblob(32),20,30,1)`, alice)
}
