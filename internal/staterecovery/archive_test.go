package staterecovery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/simpleauth"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
)

const password = "Recovery password fixture 2026"
const oldToken = "sssssssssssssssssssssssssssssssssssssssssss"

func openState(t *testing.T, root string) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(root, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func service(t *testing.T, db *sql.DB) *simpleauth.Service {
	t.Helper()
	r, err := simpleauth.NewSQLiteRecords(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s, err := simpleauth.New(r)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func fixture(t *testing.T) (string, *sql.DB, string) {
	t.Helper()
	root := privateTemp(t)
	db := openState(t, root)
	s := service(t, db)
	a, err := s.Create(context.Background(), "Alice", password)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(context.Background(), "Bob", password)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, "UPDATE principals SET disabled_at_ms=created_at_ms+1 WHERE id=?", b.ID)
	execSQL(t, db, "INSERT INTO principals(id,kind,created_at_ms) VALUES('principal_recovery_oidc','oidc',1000),('principal_recovery_guest','guest',1000)")
	execSQL(t, db, "INSERT INTO oidc_identities VALUES('https://issuer.example.test','exact-subject-01','principal_recovery_oidc')")
	execSQL(t, db, "INSERT INTO sessions VALUES(?, ?, julianday('now')+1)", oldToken, []byte("SESSION_PAYLOAD_CANARY"))
	execSQL(t, db, "INSERT INTO auth_attempts VALUES(?,?,?, ?,1000,2000,NULL)", bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), "OIDC_NONCE_CANARY_12345", strings.Repeat("VERIFIER_CANARY_", 4))
	execSQL(t, db, "INSERT INTO oidc_start_requests VALUES(?,?,?,?,1000,2000)", bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{1}, 32))
	execSQL(t, db, "INSERT INTO guest_starts VALUES(?,?,'principal_recovery_guest',1000,2000)", bytes.Repeat([]byte{5}, 32), bytes.Repeat([]byte{6}, 32))
	execSQL(t, db, "INSERT INTO session_revocations VALUES(?,1000,2000)", strings.Repeat("r", 43))
	execSQL(t, db, "INSERT INTO session_login_grants VALUES(?,?,NULL,1000,2000,NULL)", strings.Repeat("g", 43), strings.Repeat("n", 43))
	execSQL(t, db, "INSERT INTO simple_session_credentials VALUES(?,?,1)", oldToken, a.ID)
	execSQL(t, db, "INSERT INTO principal_cleanup_requests VALUES(?,'principal_disabled',1000,NULL)", b.ID)
	return root, db, a.ID
}

func TestLogicalRecoveryPreservesCredentialsIdentityAndDisablingButNoProtocolState(t *testing.T) {
	root, source, alice := fixture(t)
	reader, err := storage.OpenReadOnly(filepath.Join(root, "state.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := Export(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SESSION_PAYLOAD_CANARY", "NONCE_CANARY", "VERIFIER_CANARY", oldToken, "principal_recovery_guest", "sessions", "auth_attempts", "guest_starts", "session_login_grants"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("excluded protocol value/table exported: %s", secret)
		}
	}
	if !bytes.Contains(data, []byte("$argon2id$")) {
		t.Fatal("explicit verifier recovery missing")
	}
	if _, err := reader.Exec("DELETE FROM sessions"); err == nil {
		t.Fatal("export reader allowed SQL mutation")
	}
	if count(t, source, "sessions") != 1 {
		t.Fatal("export changed original sessions")
	}
	target := openState(t, privateTemp(t))
	if err := Restore(context.Background(), target, data); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "auth_attempts", "session_revocations", "guest_starts", "oidc_start_requests", "session_login_grants", "simple_session_credentials", "principal_cleanup_requests"} {
		if count(t, target, table) != 0 {
			t.Fatal("transient state restored", table)
		}
	}
	sessions, _ := websession.NewSQLiteRecords(target)
	if _, found, err := sessions.FindSession(context.Background(), oldToken); err != nil || found {
		t.Fatal("old session authorized after recovery")
	}
	s := service(t, target)
	result, err := s.Authenticate(context.Background(), "ALICE", password)
	if err != nil || result.ID != alice || result.CredentialVersion != 1 {
		t.Fatal("valid local login not preserved", err)
	}
	if _, err = s.Authenticate(context.Background(), "bob", password); err == nil {
		t.Fatal("disabled credential enabled")
	}
	if reset, err := s.SetPassword(context.Background(), "alice", "Changed recovery password 2026"); err != nil || reset.CredentialVersion != 2 {
		t.Fatal("credential version reset failed", err)
	}
	var binding [32]byte
	copy(binding[:], bytes.Repeat([]byte{7}, 32))
	newToken := base64.RawURLEncoding.EncodeToString(binding[:])
	execSQL(t, target, "INSERT INTO sessions VALUES(?,CAST('fresh' AS BLOB),julianday('now')+1)", newToken)
	oidc, _ := oidcidentity.NewSQLiteRecords(target)
	id, err := oidc.Resolve(context.Background(), oidcidentity.ResolveRecord{Issuer: "https://issuer.example.test", Subject: "exact-subject-01", ProposedPrincipalID: "principal_different_oidc", CreatedAtMS: time.Now().UnixMilli(), BrowserHash: binding})
	if err != nil || id != "principal_recovery_oidc" {
		t.Fatal("exact issuer/sub mapping changed", err)
	}
}

func TestRestoreRejectsMalformedOrOverbroadArchivesWithoutPartialRows(t *testing.T) {
	_, db, _ := fixture(t)
	data, err := Export(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(fn func(map[string]any)) []byte {
		var a map[string]any
		if json.Unmarshal(data, &a) != nil {
			t.Fatal("decode fixture")
		}
		fn(a)
		b, _ := json.Marshal(a)
		return b
	}
	cases := map[string][]byte{
		"unknown version":        mutate(func(a map[string]any) { a["version"] = 2 }),
		"unknown schema":         mutate(func(a map[string]any) { a["state_schema"] = 6 }),
		"sessions field":         mutate(func(a map[string]any) { a["sessions"] = []any{} }),
		"duplicate key":          bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"case alias":             bytes.Replace(data, []byte(`"version":1`), []byte(`"VERSION":1`), 1),
		"case duplicate":         bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"Version":1`), 1),
		"unicode alias":          bytes.Replace(data, []byte(`"password_hash"`), []byte(`"password_haſh"`), 1),
		"trailing document":      append(append([]byte{}, data...), []byte(`{}`)...),
		"invalid utf8":           append(append([]byte{}, data...), 0xff),
		"unknown principal kind": mutate(func(a map[string]any) { a["principals"].([]any)[0].(map[string]any)["kind"] = "none" }),
		"missing principal":      mutate(func(a map[string]any) { a["principals"] = []any{} }),
		"missing credential":     mutate(func(a map[string]any) { a["local_credentials"] = []any{} }),
		"duplicate username": mutate(func(a map[string]any) {
			c := a["local_credentials"].([]any)
			c[1].(map[string]any)["username"] = c[0].(map[string]any)["username"]
		}),
		"uncanonical username": mutate(func(a map[string]any) { a["local_credentials"].([]any)[0].(map[string]any)["username"] = "ALICE" }),
		"unbounded hash cost": mutate(func(a map[string]any) {
			c := a["local_credentials"].([]any)[0].(map[string]any)
			c["password_hash"] = strings.Replace(c["password_hash"].(string), "m=19456", "m=999999999", 1)
		}),
		"credential version zero": mutate(func(a map[string]any) { a["local_credentials"].([]any)[0].(map[string]any)["credential_version"] = 0 }),
		"duplicate principal":     mutate(func(a map[string]any) { p := a["principals"].([]any); a["principals"] = append(p, p[0]) }),
		"oversized":               bytes.Repeat([]byte{' '}, MaxArchiveBytes+1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			target := openState(t, privateTemp(t))
			if err := Restore(context.Background(), target, bad); !errors.Is(err, ErrArchive) {
				t.Fatal("invalid archive accepted", err)
			}
			for _, table := range []string{"principals", "oidc_identities", "local_credentials"} {
				if count(t, target, table) != 0 {
					t.Fatal("partial restore", table)
				}
			}
		})
	}
}

func TestRestoreSQLFailureRollsBackAndNeverMergesExistingState(t *testing.T) {
	_, db, _ := fixture(t)
	data, err := Export(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	target := openState(t, privateTemp(t))
	execSQL(t, target, "CREATE TRIGGER fixture_fail BEFORE INSERT ON local_credentials BEGIN SELECT RAISE(ABORT,'SECRET_SQL_CANARY'); END")
	if err := Restore(context.Background(), target, data); !errors.Is(err, ErrArchive) || strings.Contains(err.Error(), "SECRET_SQL") {
		t.Fatal("failure leaked or hidden", err)
	}
	if count(t, target, "principals") != 0 || count(t, target, "oidc_identities") != 0 {
		t.Fatal("rollback left durable rows")
	}
	execSQL(t, target, "DROP TRIGGER fixture_fail")
	execSQL(t, target, "INSERT INTO sessions VALUES(?,CAST('existing' AS BLOB),julianday('now')+1)", oldToken)
	if Restore(context.Background(), target, data) == nil || count(t, target, "sessions") != 1 {
		t.Fatal("existing transient state merged/deleted")
	}
}

func TestExportRefusesModifiedOldWrongKindAndUnsupportedDurablePrincipals(t *testing.T) {
	for _, mode := range []string{"checksum", "missing migration", "newer migration", "wrong kind", "none principal"} {
		t.Run(mode, func(t *testing.T) {
			_, db, _ := fixture(t)
			switch mode {
			case "checksum":
				execSQL(t, db, "UPDATE schema_migrations SET sha256='wrong' WHERE version=5")
			case "missing migration":
				execSQL(t, db, "DELETE FROM schema_migrations WHERE version=5")
			case "newer migration":
				execSQL(t, db, "INSERT INTO schema_migrations VALUES(6,'future','future',1000)")
			case "wrong kind":
				execSQL(t, db, "UPDATE database_identity SET kind='documents'")
			case "none principal":
				execSQL(t, db, "INSERT INTO principals VALUES('principal_recovery_none','none',1000,NULL)")
			}
			if data, err := Export(context.Background(), db); !errors.Is(err, ErrArchive) || data != nil {
				t.Fatal("unsupported source exported", err)
			}
		})
	}
}

func TestStateCLIPrivateNoOverwriteFreshPublicationAndConcurrentRestores(t *testing.T) {
	source, _, _ := fixture(t)
	backups := privateTemp(t)
	path := filepath.Join(backups, "identities.json")
	lookup := func(key string) (string, bool) {
		if key != "SCANNER_STATE_DIR" {
			t.Fatal("read unrelated configuration", key)
		}
		return source, true
	}
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"export", path}, lookup, &output); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 || strings.Contains(output.String(), "$argon2id$") {
		t.Fatal("unsafe backup file/status")
	}
	if Run(context.Background(), []string{"export", path}, lookup, &output) == nil {
		t.Fatal("overwrote existing archive")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatal("existing archive changed")
	}
	target := privateTemp(t)
	targetLookup := func(key string) (string, bool) {
		if key != "SCANNER_STATE_DIR" {
			t.Fatal(key)
		}
		return target, true
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsOut <- Run(context.Background(), []string{"restore", path}, targetLookup, ioDiscard{})
		}()
	}
	wg.Wait()
	close(errorsOut)
	success := 0
	for err := range errorsOut {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("fresh publication winners", success)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 1 || entries[0].Name() != "state.sqlite3" {
		t.Fatal("staging residue or missing database", entries)
	}
	if Run(context.Background(), []string{"restore", path}, targetLookup, &output) == nil {
		t.Fatal("restore replaced existing state")
	}
	db := openState(t, target)
	if count(t, db, "principals") != 3 || count(t, db, "sessions") != 0 {
		t.Fatal("published incomplete state")
	}
	if _, err := service(t, db).Authenticate(context.Background(), "alice", password); err != nil {
		t.Fatal(err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestStateCLIRefusesPublicSymlinkMissingAndCancelledInputs(t *testing.T) {
	source, _, _ := fixture(t)
	destination := privateTemp(t)
	backup := filepath.Join(destination, "state.json")
	lookup := func(string) (string, bool) { return source, true }
	if err := Run(context.Background(), []string{"export", backup}, lookup, ioDiscard{}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"public archive", "archive symlink", "public root", "root symlink", "nonempty root", "cancelled", "missing source"} {
		t.Run(mode, func(t *testing.T) {
			root := privateTemp(t)
			input := backup
			ctx := context.Background()
			action := "restore"
			switch mode {
			case "public archive":
				input = filepath.Join(privateTemp(t), "public.json")
				data, _ := os.ReadFile(backup)
				os.WriteFile(input, data, 0644)
			case "archive symlink":
				input = filepath.Join(privateTemp(t), "link.json")
				os.Symlink(backup, input)
			case "public root":
				os.Chmod(root, 0770)
			case "root symlink":
				link := filepath.Join(privateTemp(t), "link")
				os.Symlink(root, link)
				root = link
			case "nonempty root":
				os.WriteFile(filepath.Join(root, "keep"), []byte("keep"), 0600)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "missing source":
				action = "export"
				input = filepath.Join(privateTemp(t), "absent.json")
			}
			if err := Run(ctx, []string{action, input}, func(string) (string, bool) { return root, true }, ioDiscard{}); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if mode == "missing source" {
				entries, _ := os.ReadDir(root)
				if len(entries) != 0 {
					t.Fatal("export initialized a missing database")
				}
			}
		})
	}
}

func privateTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestArchiveParserBudgetsAndRedaction(t *testing.T) {
	if uniqueKeys([]byte(strings.Repeat("[", 20)+"0"+strings.Repeat("]", 20))) == nil {
		t.Fatal("excessive nesting accepted")
	}
	if uniqueKeys([]byte("["+strings.Repeat("0,", 250001)+"0]")) == nil {
		t.Fatal("unbounded token count accepted")
	}
	a := archive{Credentials: []credential{{Hash: "VERIFIER_SECRET_CANARY"}}}
	if strings.Contains(fmt.Sprintf("%+v %#v", a, a), "VERIFIER_SECRET_CANARY") {
		t.Fatal("archive formatting leaked credentials")
	}
	a = archive{Format: "scanner-kiosk-state", Version: 1, StateSchema: 5, Principals: make([]principal, maxRecords+1)}
	if validate(a) == nil {
		t.Fatal("record count limit missing")
	}
}
