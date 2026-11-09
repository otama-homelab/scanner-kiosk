package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliverySchemaUpgrade(t *testing.T) {
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
	for i, file := range files[:21] {
		body, e := migrations.ReadFile("migrations/documents/" + file.Name())
		if e != nil {
			t.Fatal(e)
		}
		must(t, db, string(body))
		must(t, db, `INSERT INTO schema_migrations VALUES(?,?,?,1000)`, i+1, file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, `INSERT INTO owners(id) VALUES(?)`, alice)
	must(t, db, `INSERT INTO documents(id,owner_id,purpose,state,revision,created_at_ms,expires_at_ms) VALUES(?,?,'document','ready',1,1000,10000)`, docA, alice)
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('delivery_output_0001',?,?,'output','ready','output.pdf','application/pdf',100,zeroblob(32),1,1000)`, docA, alice)
	must(t, db, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,'delivery_output_0001')`, docA, alice)
	must(t, db, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms) VALUES(?,zeroblob(32),'scan-new',zeroblob(32),10000)`, alice)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]int{`SELECT count(*) FROM schema_migrations`: 22, `SELECT count(*) FROM documents`: 1, `SELECT count(*) FROM artifacts`: 1, `SELECT count(*) FROM published_outputs`: 1, `SELECT count(*) FROM request_receipts`: 1, `PRAGMA foreign_keys`: 1} {
		var got int
		if err = db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatal("upgrade lost old data/constraints", query, got, err)
		}
	}
	deliveryID := "delivery_" + strings.Repeat("a", 48)
	insert := `INSERT INTO deliveries(id,owner_id,document_id,revision,artifact_id,relative_path,media_type,byte_size,payload_sha256,expires_at_ms,destination_id,config_fingerprint,filename,state,created_at_ms) VALUES(?,?,?,1,'delivery_output_0001','output.pdf','application/pdf',100,zeroblob(32),10000,'local',?,?, 'queued',2000)`
	must(t, db, insert, deliveryID, alice, docA, strings.Repeat("a", 64), deliveryID+".pdf")
	for _, query := range []string{
		`UPDATE deliveries SET owner_id='other_owner_000001'`,
		`UPDATE deliveries SET revision=2`,
		`UPDATE deliveries SET relative_path='different.pdf'`,
		`UPDATE deliveries SET payload_sha256=randomblob(32)`,
		`UPDATE deliveries SET expires_at_ms=20000`,
		`UPDATE deliveries SET destination_id='other'`,
		`UPDATE deliveries SET config_fingerprint='` + strings.Repeat("b", 64) + `'`,
		`UPDATE deliveries SET state='confirmed'`,
		`UPDATE deliveries SET attempted_at_ms=2000`,
	} {
		rejects(t, db, query)
	}
	second := "delivery_" + strings.Repeat("b", 48)
	rejects(t, db, insert, second, alice, docA, strings.Repeat("a", 64), second+".pdf")
	must(t, db, `UPDATE deliveries SET state='attempted',attempted_at_ms=2000`)
	rejects(t, db, `UPDATE deliveries SET state='queued',attempted_at_ms=NULL`)
	rejects(t, db, `UPDATE deliveries SET attempted_at_ms=2001`)
	must(t, db, `UPDATE deliveries SET state='accepted',task_id='12345678-1234-4234-8234-123456789abc'`)
	rejects(t, db, `UPDATE deliveries SET task_id='22345678-1234-4234-8234-123456789abc'`)
	rejects(t, db, `UPDATE deliveries SET state='confirmed',remote_document_id=7,confirmed_sha256=randomblob(32)`)
	must(t, db, `UPDATE deliveries SET state='confirmed',remote_document_id=7,confirmed_sha256=zeroblob(32)`)
	// Removing evidence must never reopen the semantic send fence.
	if _, err = db.Exec(`DELETE FROM deliveries`); err == nil {
		t.Fatal("delivery evidence deletion reopened semantic send fence")
	}
}
