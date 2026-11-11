package storage

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historicalPhotoDB(t *testing.T) (string, *sql.DB) {
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
	for i, file := range files[:24] {
		body, err := migrations.ReadFile("migrations/documents/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	for _, owner := range []string{alice, bob} {
		must(t, db, "INSERT INTO owners VALUES(?)", owner)
	}
	for _, doc := range []string{docA, "document_legacy_import", "document_legacy_photo"} {
		purpose := "document"
		if doc == "document_legacy_photo" {
			purpose = "photo"
		}
		must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,?,'ready',1000,10000)", doc, alice, purpose)
	}
	artifact(t, db, "legacy_scan_original", docA, alice, "original", 1)
	must(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('legacy_scan_page01',?,?,0,'legacy_scan_original','a4','color',300,20,30)`, docA, alice)
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('legacy_pdf_original',?,?,'original','ready','legacy.pdf','application/pdf',100,zeroblob(32),1,1000)`, docA, alice)
	must(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points) VALUES('legacy_pdf_page001',?,?,1,'legacy_pdf_original','pdf',200,300)`, docA, alice)
	artifact(t, db, "legacy_scanner_photo", "document_legacy_photo", alice, "original", 1)
	must(t, db, pageSQL(), "legacy_photo_page01", "document_legacy_photo", alice, 0, "legacy_scanner_photo")
	artifact(t, db, "legacy_corrected_01", docA, alice, "intermediate", 1)
	must(t, db, `INSERT INTO page_corrections(suggestion_id,document_id,owner_id,page_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json,confidence_kind,created_at_ms) VALUES('legacy_correction1',?,?,'legacy_scan_page01','legacy_scan_original','legacy_corrected_01',zeroblob(32),zeroblob(32),'{}','unavailable',1500)`, docA, alice)
	must(t, db, `UPDATE page_corrections SET state='applied',applied_at_ms=1600 WHERE suggestion_id='legacy_correction1'`)
	artifact(t, db, "legacy_thumbnail01", docA, alice, "thumbnail", 1)
	// artifact helper's immutable media is PNG; thumbnails require JPEG.
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('legacy_jpeg_thumb01',?,?,'thumbnail','ready','legacy-thumb.jpg','image/jpeg',100,zeroblob(32),1,1000)`, docA, alice)
	must(t, db, `INSERT INTO page_thumbnails VALUES('legacy_scan_original',?,?,'legacy_jpeg_thumb01',20,30)`, docA, alice)
	must(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms) VALUES('legacy_render_job1',?,?,'render','running',1,'{}',1500,9000,'legacy_render_runner',1500)`, docA, alice)
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('legacy_ocr_output01',?,?,'output','ready','legacy-output.pdf','application/pdf',100,zeroblob(32),1,1500)`, docA, alice)
	must(t, db, `INSERT INTO job_artifacts VALUES('legacy_render_job1',?,?,'legacy_ocr_output01')`, docA, alice)
	must(t, db, `INSERT INTO ocr_results(id,job_id,document_id,owner_id,input_revision,output_artifact_id,bundle_json,bundle_sha256,created_at_ms) VALUES('legacy_ocr_result1','legacy_render_job1',?,?,1,'legacy_ocr_output01','{}',zeroblob(32),1600)`, docA, alice)
	must(t, db, `UPDATE jobs SET state='succeeded' WHERE id='legacy_render_job1'`)
	must(t, db, `INSERT INTO published_outputs VALUES(?,?,1,'legacy_ocr_output01')`, docA, alice)
	delivery := "delivery_" + strings.Repeat("a", 48)
	must(t, db, `INSERT INTO deliveries(id,owner_id,document_id,revision,artifact_id,relative_path,media_type,byte_size,payload_sha256,expires_at_ms,destination_id,config_fingerprint,filename,state,created_at_ms) VALUES(?,?,?,1,'legacy_ocr_output01','legacy-output.pdf','application/pdf',100,zeroblob(32),10000,'local',?,?,'queued',2000)`, delivery, alice, docA, strings.Repeat("a", 64), delivery+".pdf")
	must(t, db, `INSERT INTO delivery_receipts(owner_id,key_hash,request_hash,delivery_id,expires_at_ms) VALUES(?,zeroblob(32),zeroblob(32),?,10000)`, alice, delivery)
	must(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('legacy_import_job1','document_legacy_import',?,'import','queued',1,'{"input_artifact_id":"legacy_import_raw1"}',1500,9000)`, alice)
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,input_revision,created_at_ms) VALUES('legacy_import_raw1','document_legacy_import',?,'original','staging','legacy-intake.pdf','application/pdf',0,1,1500)`, alice)
	must(t, db, `INSERT INTO import_intakes(job_id,document_id,owner_id,artifact_id,producer_id) VALUES('legacy_import_job1','document_legacy_import',?,'legacy_import_raw1','legacy_import_producer')`, alice)
	must(t, db, `INSERT INTO reservations VALUES('legacy_import_job1',?,200)`, alice)
	key := sha256.Sum256([]byte("legacy import request"))
	must(t, db, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,?,'pdf_import',zeroblob(32),'legacy_import_job1',10000)`, alice, key[:])
	return path, db
}
func snapshotTable(t *testing.T, db *sql.DB, table string) []byte {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table + " ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var records [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		records = append(records, values)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	result, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestPhotoSourceSchemaUpgrade(t *testing.T) {
	path, db := historicalPhotoDB(t)
	tables := []string{"owners", "documents", "pages", "artifacts", "jobs", "job_artifacts", "reservations", "request_receipts", "import_intakes", "page_thumbnails", "page_corrections", "ocr_results", "published_outputs", "deliveries", "delivery_receipts"}
	before := map[string][]byte{}
	for _, table := range tables {
		before[table] = snapshotTable(t, db, table)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range tables {
		if !bytes.Equal(before[table], snapshotTable(t, db, table)) {
			t.Fatal("historical graph changed", table)
		}
	}
	for query, want := range map[string]int{"SELECT count(*) FROM pragma_foreign_key_check": 0, "PRAGMA foreign_keys": 1, "PRAGMA legacy_alter_table": 0, "SELECT count(*) FROM schema_migrations": 27} {
		var n int
		if err = db.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Fatal("upgrade constraints", query, n, err)
		}
	}
	must(t, db, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_photo_new1',?,'document','review',1000,10000)`, alice)
	artifact(t, db, "photo_raw_original1", "document_photo_new1", alice, "original", 1)
	artifact(t, db, "photo_normalized001", "document_photo_new1", alice, "intermediate", 1)
	photo := `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES('photo_page_new001','document_photo_new1',?,0,'photo_raw_original1','photo',20,30)`
	must(t, db, photo, alice)
	source := `INSERT INTO photo_sources VALUES('photo_page_new001','document_photo_new1',?,'photo_raw_original1','photo_normalized001',zeroblob(32),zeroblob(32),20,30,6)`
	rejects(t, db, source, bob)
	must(t, db, source, alice)
	rejects(t, db, `UPDATE photo_sources SET normalized_artifact_id='legacy_corrected_01'`)
	rejects(t, db, `UPDATE photo_sources SET raw_sha256=randomblob(32)`)
	rejects(t, db, `UPDATE pages SET capture_dpi=300 WHERE id='photo_page_new001'`)
	rejects(t, db, `UPDATE pages SET pixel_width=30 WHERE id='photo_page_new001'`)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height,capture_dpi) VALUES('photo_page_invalid1','document_photo_new1',?,1,'photo_raw_original1','photo',20,30,300)`, alice)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES('photo_page_invalid2',?,?,2,'legacy_pdf_original','photo',20,30)`, docA, alice)
	rejects(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pixel_width,pixel_height) VALUES('photo_page_invalid3','document_legacy_photo',?,1,'legacy_scanner_photo','photo',20,30)`, alice)
	// Normalized ownership/provenance participates in normal cleanup cascades.
	must(t, db, `DELETE FROM pages WHERE id='photo_page_new001'`)
	var n int
	if err = db.QueryRow("SELECT count(*) FROM photo_sources").Scan(&n); err != nil || n != 0 {
		t.Fatal("photo lineage did not cascade", n, err)
	}
}
