package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oldStateFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.sqlite3")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	must(t, db, "PRAGMA foreign_keys=ON")
	must(t, db, `CREATE TABLE database_identity(singleton INTEGER PRIMARY KEY CHECK(singleton=1),kind TEXT NOT NULL);INSERT INTO database_identity VALUES(1,'state');CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,sha256 TEXT NOT NULL,applied_at_ms INTEGER NOT NULL);`)
	for i, name := range []string{"0001_initial.sql", "0002_session_revocations.sql", "0003_oidc_browser_flow.sql"} {
		body, err := migrations.ReadFile("migrations/state/" + name)
		if err != nil {
			t.Fatal(err)
		}
		must(t, db, string(body))
		must(t, db, "INSERT INTO schema_migrations VALUES(?,?,?,1000)", i+1, name, fmt.Sprintf("%x", sha256.Sum256(body)))
	}
	must(t, db, "INSERT INTO principals VALUES(?,'oidc',1000,NULL)", alice)
	must(t, db, "INSERT INTO principals VALUES(?,'guest',1000,2000)", bob)
	must(t, db, "INSERT INTO oidc_identities VALUES('https://issuer.example.test','subject-old',?)", alice)
	must(t, db, "INSERT INTO guest_starts VALUES(zeroblob(32),zeroblob(32),?,1000,10000)", bob)
	must(t, db, "INSERT INTO principal_cleanup_requests VALUES(?,'guest_end',2000,NULL)", bob)
	must(t, db, "INSERT INTO sessions VALUES(?,x'010203',julianday('now','+1 hour'))", strings.Repeat("A", 43))
	must(t, db, "INSERT INTO session_revocations VALUES(?,1000,10000)", strings.Repeat("B", 43))
	must(t, db, "INSERT INTO session_login_grants VALUES(?,?,?,1000,10000,NULL)", strings.Repeat("C", 43), strings.Repeat("D", 43), strings.Repeat("E", 43))
	must(t, db, "INSERT INTO auth_attempts VALUES(zeroblob(32),zeroblob(32),?,?,1000,10000,NULL)", strings.Repeat("F", 43), strings.Repeat("G", 43))
	must(t, db, "INSERT INTO oidc_start_requests VALUES(zeroblob(32),zeroblob(32),zeroblob(32),zeroblob(32),1000,10000)")
	return path
}

func TestLocalCredentialMigrationPreservesExistingIdentityAndSessions(t *testing.T) {
	path := oldStateFixture(t)
	db, err := Open(path, State)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for table, count := range map[string]int{"principals": 2, "oidc_identities": 1, "guest_starts": 1, "principal_cleanup_requests": 1, "sessions": 1, "session_revocations": 1, "session_login_grants": 1, "auth_attempts": 1, "oidc_start_requests": 1, "local_credentials": 0} {
		var actual int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&actual); err != nil || actual != count {
			t.Fatal("migration lost rows", table, actual, err)
		}
	}
	var subject, guest, cleanup, data string
	if err := db.QueryRow("SELECT subject FROM oidc_identities WHERE principal_id=?", alice).Scan(&subject); err != nil || subject != "subject-old" {
		t.Fatal(err, subject)
	}
	if err := db.QueryRow("SELECT principal_id FROM guest_starts").Scan(&guest); err != nil || guest != bob {
		t.Fatal(err, guest)
	}
	if err := db.QueryRow("SELECT reason FROM principal_cleanup_requests WHERE principal_id=?", bob).Scan(&cleanup); err != nil || cleanup != "guest_end" {
		t.Fatal(err, cleanup)
	}
	if err := db.QueryRow("SELECT hex(data) FROM sessions").Scan(&data); err != nil || data != "010203" {
		t.Fatal(err, data)
	}
	var violations, enabled int
	if err := db.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
		t.Fatal("broken foreign keys", violations, err)
	}
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("migration disabled FK checks", err)
	}
	rejects(t, db, "UPDATE oidc_identities SET subject='changed'")
	rejects(t, db, "UPDATE guest_starts SET expires_at_ms=20000")
	rejects(t, db, "UPDATE principals SET kind='simple' WHERE id=?", alice)
	rejects(t, db, "DELETE FROM principals WHERE id=?", bob)
	must(t, db, "INSERT INTO principals VALUES('principal_simple_0001','simple',2000,NULL)")
	must(t, db, "INSERT INTO local_credentials VALUES('alice','principal_simple_0001',?,1,2000,2000)", strings.Repeat("h", 97))
	rejects(t, db, "UPDATE local_credentials SET username='renamed',credential_version=2")
	rejects(t, db, "UPDATE local_credentials SET password_hash=?", strings.Repeat("x", 97))
	rejects(t, db, "INSERT INTO local_credentials VALUES('oidc-mapped',?,?,1,2000,2000)", alice, strings.Repeat("h", 97))
	must(t, db, "UPDATE local_credentials SET password_hash=?,credential_version=2,updated_at_ms=3000", strings.Repeat("x", 97))
	db.Close()
	db, err = Open(path, State)
	if err != nil {
		t.Fatal("reopen migration", err)
	}
	db.Close()
}

func TestLocalCredentialMigrationFailureRollsBackExistingData(t *testing.T) {
	path := oldStateFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	must(t, db, "CREATE TABLE local_credentials(sentinel TEXT)")
	if failed, err := Open(path, State); err == nil {
		failed.Close()
		t.Fatal("colliding schema did not fail")
	}
	var count int
	for table, wanted := range map[string]int{"principals": 2, "oidc_identities": 1, "guest_starts": 1, "principal_cleanup_requests": 1, "schema_migrations": 3} {
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != wanted {
			t.Fatal("failed migration lost original data", table, count, err)
		}
	}
	rejects(t, db, "INSERT INTO principals VALUES('principal_simple_bad','simple',3000,NULL)")
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name LIKE 'migration_%' OR name='principals_next'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial migration tables committed", count, err)
	}
}
