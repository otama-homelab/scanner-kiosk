package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const alice = "owner_alice_00000001"
const bob = "owner_bob_0000000001"
const docA = "document_alice_00001"
const docB = "document_bob_0000001"

func must(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	r, e := db.Exec(query, args...)
	if e != nil {
		t.Fatalf("SQL failed: %v", e)
	}
	return r
}
func rejects(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, e := db.Exec(query, args...); e == nil {
		t.Fatal("invalid database mutation accepted")
	}
}
func newDocuments(t *testing.T) *sql.DB {
	t.Helper()
	db, e := Open(filepath.Join(t.TempDir(), "documents.sqlite3"), Documents)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{alice, bob} {
		must(t, db, "INSERT INTO owners(id) VALUES(?)", id)
	}
	for i, id := range []string{docA, docB} {
		owner := alice
		if i == 1 {
			owner = bob
		}
		must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,86401000)", id, owner)
	}
	return db
}
func artifact(t *testing.T, db *sql.DB, id, doc, owner, kind string, revision int) {
	t.Helper()
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,?,'ready',?,'image/png',100,zeroblob(32),?,1001)`, id, doc, owner, kind, doc+"/"+id+".bin", revision)
}
func pageSQL() string {
	return `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,?,?,'a4','color',300)`
}
func jobSQL() string {
	return `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,?,'queued',1,'{}',2000,3000)`
}
func TestMigrationIdentityChecksumAndPrivateFiles(t *testing.T) {
	for _, kind := range []Kind{State, Documents} {
		t.Run(string(kind), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", string(kind)+".sqlite3")
			db, e := Open(path, kind)
			if e != nil {
				t.Fatal(e)
			}
			info, e := os.Stat(path)
			if e != nil || info.Mode().Perm() != 0600 {
				t.Fatal("database file is not private", e)
			}
			var foreignKeys int
			if e = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); e != nil || foreignKeys != 1 {
				t.Fatal("foreign keys disabled", e)
			}
			var journal string
			if e = db.QueryRow("PRAGMA journal_mode").Scan(&journal); e != nil || journal != "wal" {
				t.Fatal("WAL disabled", e)
			}
			db.Close()
			db, e = Open(path, kind)
			if e != nil {
				t.Fatal(e)
			}
			files, e := migrations.ReadDir("migrations/" + string(kind))
			if e != nil {
				t.Fatal(e)
			}
			expected := len(files)
			var count int
			if e = db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); e != nil || count != expected {
				t.Fatal("migration not idempotent", e)
			}
			wrong := State
			if kind == State {
				wrong = Documents
			}
			if accidental, e := Open(path, wrong); e == nil {
				accidental.Close()
				t.Fatal("state/document store mix-up accepted")
			}
			must(t, db, "UPDATE schema_migrations SET sha256='tampered'")
			db.Close()
			if accidental, e := Open(path, kind); e == nil {
				accidental.Close()
				t.Fatal("modified migration checksum accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "public.sqlite3")
	if e := os.WriteFile(path, []byte{}, 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if db, e := Open(path, State); e == nil {
		db.Close()
		t.Fatal("public DB file accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	if e := os.WriteFile(target, []byte{}, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(t.TempDir(), "linked.sqlite3")
	if e := os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	if db, e := Open(link, State); e == nil {
		db.Close()
		t.Fatal("symlink DB accepted")
	}
}
func TestOwnerAndCapturedPageConstraints(t *testing.T) {
	db := newDocuments(t)
	artifact(t, db, "original_alice_00001", docA, alice, "original", 1)
	artifact(t, db, "original_bob_0000001", docB, bob, "original", 1)
	rejects(t, db, pageSQL(), "page_cross_00000001", docA, alice, 0, "original_bob_0000001")
	rejects(t, db, pageSQL(), "page_wrong_00000001", docA, bob, 0, "original_alice_00001")
	must(t, db, pageSQL(), "page_alice_00000001", docA, alice, 0, "original_alice_00001")
	rejects(t, db, pageSQL(), "page_duplicate_0001", docA, alice, 0, "original_alice_00001")
	rejects(t, db, "UPDATE pages SET capture_dpi=600 WHERE document_id=?", docA)
	rejects(t, db, "UPDATE pages SET rotation=45 WHERE document_id=?", docA)
	rejects(t, db, "UPDATE documents SET expires_at_ms=expires_at_ms+1 WHERE id=?", docA)
	rejects(t, db, "UPDATE documents SET owner_id=? WHERE id=?", bob, docA)
	rejects(t, db, "UPDATE documents SET revision=3 WHERE id=?", docA)
	must(t, db, "UPDATE pages SET rotation=90 WHERE document_id=?", docA)
	artifact(t, db, "output_not_page_001", docA, alice, "output", 1)
	rejects(t, db, pageSQL(), "page_output_0000001", docA, alice, 1, "output_not_page_001")
	rejects(t, db, "UPDATE artifacts SET relative_path='../escape' WHERE id='original_alice_00001'")
	rejects(t, db, "UPDATE artifacts SET byte_size=101 WHERE id='original_alice_00001'")
	rejects(t, db, "UPDATE artifacts SET state='staging' WHERE id='original_alice_00001'")
}
func TestSinglePhotoAndNoUnreadyOriginal(t *testing.T) {
	db := newDocuments(t)
	id := "document_photo_00001"
	must(t, db, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'photo','review',1000,86401000)", id, alice)
	artifact(t, db, "original_photo_00001", id, alice, "original", 1)
	must(t, db, pageSQL(), "page_photo_00000001", id, alice, 0, "original_photo_00001")
	rejects(t, db, pageSQL(), "page_photo_00000002", id, alice, 1, "original_photo_00001")
	must(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,input_revision,created_at_ms) VALUES('original_staging_01',?,?,'original','staging','staged/original','image/png',0,1,1001)`, docA, alice)
	rejects(t, db, pageSQL(), "page_staging_000001", docA, alice, 0, "original_staging_01")
}
func TestPublishOnlyReadyCurrentOwnedRevision(t *testing.T) {
	db := newDocuments(t)
	artifact(t, db, "output_alice_000001", docA, alice, "output", 1)
	artifact(t, db, "output_bob_00000001", docB, bob, "output", 1)
	rejects(t, db, "INSERT INTO published_outputs VALUES(?,?,1,?)", docA, alice, "output_bob_00000001")
	must(t, db, "INSERT INTO published_outputs VALUES(?,?,1,?)", docA, alice, "output_alice_000001")
	rejects(t, db, "UPDATE documents SET revision=2 WHERE id=?", docA)
	must(t, db, "DELETE FROM published_outputs WHERE document_id=?", docA)
	must(t, db, "UPDATE documents SET revision=2 WHERE id=?", docA)
	rejects(t, db, "INSERT INTO published_outputs VALUES(?,?,1,?)", docA, alice, "output_alice_000001")
	rejects(t, db, "INSERT INTO published_outputs VALUES(?,?,2,?)", docA, alice, "output_alice_000001")
	artifact(t, db, "output_alice_000002", docA, alice, "output", 2)
	must(t, db, "INSERT INTO published_outputs VALUES(?,?,2,?)", docA, alice, "output_alice_000002")
	rejects(t, db, "UPDATE published_outputs SET artifact_id='output_alice_000001' WHERE document_id=?", docA)
}
func TestJobSnapshotDeadlineAndOwnerReservation(t *testing.T) {
	db := newDocuments(t)
	rejects(t, db, jobSQL(), "job_cross_000000001", docA, bob, "scan")
	must(t, db, jobSQL(), "job_alice_000000001", docA, alice, "scan")
	rejects(t, db, jobSQL(), "job_duplicate_00001", docA, alice, "render")
	must(t, db, jobSQL(), "job_bob_00000000001", docB, bob, "scan")
	rejects(t, db, "UPDATE jobs SET configuration_json='{} ' WHERE id='job_alice_000000001'")
	rejects(t, db, "INSERT INTO reservations VALUES('job_alice_000000001',?,100)", bob)
	must(t, db, "INSERT INTO reservations VALUES('job_alice_000000001',?,100)", alice)
	must(t, db, "UPDATE jobs SET state='succeeded' WHERE id='job_alice_000000001'")
	rejects(t, db, strings.Replace(jobSQL(), "2000,3000", "2000,86401001", 1), "job_deadline_000001", docA, alice, "render")
	rejects(t, db, strings.Replace(jobSQL(), "'{}'", "'not-json'", 1), "job_json_0000000001", docA, alice, "render")
}
func TestScannerLeaseExclusionAndFencing(t *testing.T) {
	db := newDocuments(t)
	must(t, db, jobSQL(), "job_alice_000000001", docA, alice, "scan")
	must(t, db, jobSQL(), "job_bob_00000000001", docB, bob, "scan")
	must(t, db, "INSERT INTO device_leases VALUES('canon',1,'job_alice_000000001','held')")
	rejects(t, db, "INSERT INTO device_leases VALUES('canon',1,'job_bob_00000000001','held')")
	rejects(t, db, "INSERT INTO device_leases VALUES('another',1,'job_alice_000000001','held')")
	rejects(t, db, "UPDATE device_leases SET job_id='job_bob_00000000001' WHERE device_id='canon'")
	rejects(t, db, "DELETE FROM jobs WHERE id='job_alice_000000001'")
	must(t, db, "UPDATE device_leases SET job_id=NULL,state='idle' WHERE device_id='canon' AND epoch=1")
	must(t, db, "UPDATE device_leases SET job_id='job_bob_00000000001',state='held',epoch=2 WHERE device_id='canon' AND epoch=1")
	result := must(t, db, "UPDATE device_leases SET job_id=NULL,state='idle' WHERE device_id='canon' AND epoch=1")
	n, e := result.RowsAffected()
	if e != nil || n != 0 {
		t.Fatal("stale lease update succeeded", e)
	}
}
func TestReceiptOwnerBindingAndDeletionTombstone(t *testing.T) {
	db := newDocuments(t)
	must(t, db, jobSQL(), "job_alice_000000001", docA, alice, "scan")
	must(t, db, jobSQL(), "job_bob_00000000001", docB, bob, "scan")
	query := "INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,zeroblob(32),'scan',zeroblob(32),?,999999999)"
	rejects(t, db, query, alice, "job_bob_00000000001")
	must(t, db, query, alice, "job_alice_000000001")
	rejects(t, db, query, alice, "job_alice_000000001")
	must(t, db, query, bob, "job_bob_00000000001")
	must(t, db, "DELETE FROM jobs WHERE id='job_alice_000000001'")
	var result sql.NullString
	if e := db.QueryRow("SELECT result_job_id FROM request_receipts WHERE owner_id=?", alice).Scan(&result); e != nil || result.Valid {
		t.Fatal("deleted result was not tombstoned", e)
	}
	rejects(t, db, query, alice, "job_bob_00000000001")
}
func TestOIDCIdentityAndOneTimeAttempt(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "state.sqlite3"), State)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	must(t, db, "INSERT INTO principals VALUES(?,'oidc',1000,NULL)", alice)
	must(t, db, "INSERT INTO principals VALUES(?,'guest',1000,NULL)", bob)
	must(t, db, "INSERT INTO oidc_identities VALUES('https://id.example.test','subject',?)", alice)
	rejects(t, db, "INSERT INTO oidc_identities VALUES('https://other.example.test','subject',?)", bob)
	rejects(t, db, "UPDATE oidc_identities SET subject='another'")
	rejects(t, db, "UPDATE principals SET kind='oidc' WHERE id=?", bob)
	must(t, db, "INSERT INTO auth_attempts VALUES(zeroblob(32),zeroblob(32),'synthetic-nonce-0001',?,1000,2000,NULL)", strings.Repeat("v", 43))
	must(t, db, "UPDATE auth_attempts SET consumed_at_ms=1500 WHERE consumed_at_ms IS NULL")
	rejects(t, db, "UPDATE auth_attempts SET consumed_at_ms=NULL")
}
func TestConcurrentMigrationOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			db, e := Open(path, Documents)
			if e == nil {
				e = db.Close()
			}
			errors <- e
		}()
	}
	wait.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestCleanupOrderAndLeaseReconciliation(t *testing.T) {
	db := newDocuments(t)
	artifact(t, db, "original_alice_00001", docA, alice, "original", 1)
	must(t, db, pageSQL(), "page_alice_00000001", docA, alice, 0, "original_alice_00001")
	artifact(t, db, "output_alice_000001", docA, alice, "output", 1)
	must(t, db, "INSERT INTO published_outputs VALUES(?,?,1,?)", docA, alice, "output_alice_000001")
	must(t, db, jobSQL(), "job_alice_000000001", docA, alice, "scan")
	must(t, db, "INSERT INTO device_leases VALUES('canon',1,'job_alice_000000001','held')")
	must(t, db, "INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,zeroblob(32),'scan',zeroblob(32),'job_alice_000000001',999999999)", alice)
	rejects(t, db, "DELETE FROM documents WHERE id=?", docA)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM pages WHERE document_id=?", docA).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed cleanup damaged document", err)
	}
	// A real worker must wait for process/stream termination before this release.
	must(t, db, "UPDATE device_leases SET job_id=NULL,state='idle' WHERE device_id='canon' AND epoch=1")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"published_outputs", "pages", "artifacts", "jobs"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE document_id=? AND owner_id=?", docA, alice); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("DELETE FROM documents WHERE id=? AND owner_id=?", docA, alice); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var job sql.NullString
	if err = db.QueryRow("SELECT result_job_id FROM request_receipts WHERE owner_id=?", alice).Scan(&job); err != nil || job.Valid {
		t.Fatal("receipt lost its deletion tombstone", err)
	}
	if err = db.QueryRow("SELECT count(*) FROM documents WHERE id=?", docB).Scan(&count); err != nil || count != 1 {
		t.Fatal("another owner's document was deleted", err)
	}
}
func TestJobRequiresCurrentRevisionAtAdmission(t *testing.T) {
	db := newDocuments(t)
	must(t, db, "UPDATE documents SET revision=2 WHERE id=?", docA)
	rejects(t, db, jobSQL(), "job_stale_000000001", docA, alice, "render")
	current := strings.Replace(jobSQL(), "'queued',1,", "'queued',2,", 1)
	must(t, db, current, "job_current_0000001", docA, alice, "render")
}

func TestProducingJobArtifactIdentity(t *testing.T) {
	db := newDocuments(t)
	must(t, db, jobSQL(), "render_link_job_0001", docA, alice, "render")
	artifact(t, db, "linked_output_00001", docA, alice, "output", 1)
	artifact(t, db, "wrong_revision_0001", docA, alice, "output", 2)
	rejects(t, db, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", "render_link_job_0001", docA, alice, "wrong_revision_0001")
	rejects(t, db, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", "render_link_job_0001", docB, bob, "linked_output_00001")
	must(t, db, "INSERT INTO job_artifacts(job_id,document_id,owner_id,artifact_id) VALUES(?,?,?,?)", "render_link_job_0001", docA, alice, "linked_output_00001")
	rejects(t, db, "UPDATE job_artifacts SET artifact_id=? WHERE job_id=?", "wrong_revision_0001", "render_link_job_0001")
}

func TestCapturedDimensionsArePairedBoundedAndImmutable(t *testing.T) {
	db := newDocuments(t)
	artifact(t, db, "original_dimensions_01", docA, alice, "original", 1)
	query := `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_dimensions_01',?,?,0,'original_dimensions_01','a4','color',300,?,?)`
	for _, dims := range [][2]any{{nil, 100}, {80, nil}, {0, 100}, {80, -1}, {1000001, 100}} {
		rejects(t, db, query, docA, alice, dims[0], dims[1])
	}
	must(t, db, query, docA, alice, 80, 100)
	rejects(t, db, "UPDATE pages SET pixel_width=81 WHERE document_id=?", docA)
	rejects(t, db, "UPDATE pages SET pixel_width=NULL,pixel_height=NULL WHERE document_id=?", docA)
	must(t, db, "UPDATE pages SET rotation=90 WHERE document_id=?", docA)
}
