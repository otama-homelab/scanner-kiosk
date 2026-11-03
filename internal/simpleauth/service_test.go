package simpleauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/storage"
)

const password = "Synthetic passphrase 2026!"
const replacement = "A different passphrase 2027!"

func fixture(t *testing.T) (*Service, *SQLiteRecords, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	records, err := NewSQLiteRecords(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(records)
	if err != nil {
		t.Fatal(err)
	}
	return service, records, db
}

func TestLocalCredentialsCreateVerifyResetAndDisable(t *testing.T) {
	s, records, db := fixture(t)
	ctx := context.Background()
	created, err := s.Create(ctx, "Alice", password)
	if err != nil || created.ID == "" || created.CanAdminister || created.CredentialVersion != 1 {
		t.Fatal(created, err)
	}
	verified, err := s.Authenticate(ctx, "ALICE", password)
	if err != nil || verified != created {
		t.Fatal("identity changed", verified, err)
	}
	for _, name := range []string{"alice", "unknown"} {
		denied, err := s.Authenticate(ctx, name, "incorrect password")
		if !errors.Is(err, authport.ErrDenied) || denied.ID != "" {
			t.Fatal("invalid credentials accepted", name, err)
		}
	}
	if _, err := s.Create(ctx, "alice", replacement); !errors.Is(err, authport.ErrConflict) {
		t.Fatal("duplicate account overwritten", err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM principals").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate created orphan principal", err, count)
	}
	credential, found, err := records.Lookup(ctx, "alice")
	if err != nil || !found || !strings.HasPrefix(credential.Hash.Encoded(), hashPrefix) {
		t.Fatal("hash missing", err)
	}
	if strings.Contains(credential.Hash.Encoded(), password) {
		t.Fatal("plaintext stored")
	}
	printed := fmt.Sprintf("%v %s %q %#v", credential.Hash, credential.Hash, credential.Hash, credential.Hash)
	encoded, _ := json.Marshal(credential)
	if strings.Contains(printed, credential.Hash.Encoded()) || strings.Contains(string(encoded), credential.Hash.Encoded()) {
		t.Fatal("credential hash leaked through formatting")
	}
	reset, err := s.SetPassword(ctx, "Alice", replacement)
	if err != nil || reset.ID != created.ID || reset.CredentialVersion != 2 {
		t.Fatal("reset changed principal", reset, err)
	}
	if current, err := records.Current(ctx, created.ID, 1); err != nil || current {
		t.Fatal("stale credential version remained current", err)
	}
	if _, err := s.Authenticate(ctx, "alice", password); !errors.Is(err, authport.ErrDenied) {
		t.Fatal("old password accepted", err)
	}
	if result, err := s.Authenticate(ctx, "alice", replacement); err != nil || result != reset {
		t.Fatal("new password denied", err)
	}
	if _, err := db.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), created.ID); err != nil {
		t.Fatal(err)
	}
	if result, err := s.Authenticate(ctx, "alice", replacement); !errors.Is(err, authport.ErrDenied) || result.ID != "" {
		t.Fatal("disabled principal authenticated", err)
	}
	if _, err := s.SetPassword(ctx, "alice", password); !errors.Is(err, authport.ErrDenied) {
		t.Fatal("password reset reenabled principal", err)
	}
}

func TestPasswordEncodingAndInputBounds(t *testing.T) {
	hash, err := makeHash(password)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{"", strings.Repeat("x", 257), strings.Replace(hash.Encoded(), "m=19456", "m=99999999", 1), strings.Replace(hash.Encoded(), "t=2", "t=99999999", 1), hash.Encoded() + "=", strings.Replace(hash.Encoded(), "v=19", "v=16", 1), hash.Encoded() + "$extra"} {
		if _, err := ParsePasswordHash(encoded); !errors.Is(err, authport.ErrUnavailable) {
			t.Fatal("unbounded/invalid profile accepted")
		}
	}
	second, err := makeHash(password)
	if err != nil || second.Encoded() == hash.Encoded() {
		t.Fatal("salt reused", err)
	}
	for _, input := range []string{"", strings.Repeat("a", 14), strings.Repeat("a", 1025), "valid passphrase\x00tail", "valid passphrase\nsecond"} {
		if validPassword(input, true) {
			t.Fatal("invalid creation password accepted")
		}
	}
	if !validPassword(strings.Repeat("書", 15), true) || validPassword(strings.Repeat("書", 14), true) {
		t.Fatal("password counted bytes instead of characters")
	}
	for input, wanted := range map[string]string{"Alice": "alice", "Straße": "strasse", "カ\u3099": "ガ", "anna_01": "anna_01"} {
		got, err := NormalizeUsername(input)
		if err != nil || got != wanted {
			t.Fatal("username normalization", got, err)
		}
	}
	for _, input := range []string{"", " alice", "alice ", "a b", "a/b", "_alice", strings.Repeat("a", 65), "\xff"} {
		if _, err := NormalizeUsername(input); err == nil {
			t.Fatal("ambiguous/invalid username accepted")
		}
	}
}

type currentHook struct {
	Records
	before func()
}

func (h currentHook) Current(ctx context.Context, principal string, version int64) (bool, error) {
	h.before()
	return h.Records.Current(ctx, principal, version)
}

func TestCredentialResetOrDisableDuringVerificationIsRejected(t *testing.T) {
	for _, mode := range []string{"reset", "disable"} {
		t.Run(mode, func(t *testing.T) {
			s, records, db := fixture(t)
			ctx := context.Background()
			created, err := s.Create(ctx, "alice", password)
			if err != nil {
				t.Fatal(err)
			}
			hash, err := makeHash(replacement)
			if err != nil {
				t.Fatal(err)
			}
			wrapped, err := New(currentHook{Records: records, before: func() {
				if mode == "reset" {
					if _, err := records.Replace(ctx, "alice", hash); err != nil {
						t.Error(err)
					}
				} else {
					if _, err := db.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), created.ID); err != nil {
						t.Error(err)
					}
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := wrapped.Authenticate(ctx, "alice", password)
			if !errors.Is(err, authport.ErrDenied) || result.ID != "" {
				t.Fatal("stale verification issued identity", err)
			}
		})
	}
}

func TestCredentialConcurrencyAndUncertainCommit(t *testing.T) {
	_, records, db := fixture(t)
	ctx := context.Background()
	hash, err := makeHash(password)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, principal := range []string{"principal_competing_01", "principal_competing_02"} {
		wait.Go(func() { _, err := records.Create(ctx, "alice", principal, hash); results <- err })
	}
	wait.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, authport.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("duplicate account race", success, conflict)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM principals").Scan(&count); err != nil || count != 1 {
		t.Fatal("orphan identity", err, count)
	}
	records.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected commit uncertainty")
	}
	result, err := records.Create(ctx, "bob", "principal_uncertain_01", hash)
	if !errors.Is(err, authport.ErrUnavailable) || result.PrincipalID != "" {
		t.Fatal("uncertain identity escaped", err)
	}
	if credential, found, err := records.Lookup(ctx, "bob"); err != nil || !found || credential.Version != 1 {
		t.Fatal("fixture did not commit", err)
	}
	if _, err := records.Create(ctx, "bob", "principal_retry_00001", hash); !errors.Is(err, authport.ErrConflict) {
		t.Fatal("uncertain creation retried by overwrite", err)
	}
}

func TestPasswordWorkBoundCancellationAndStorageFailure(t *testing.T) {
	s, _, db := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Authenticate(ctx, "alice", password); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.gate <- struct{}{}
	if _, err := s.Authenticate(context.Background(), "alice", password); !errors.Is(err, authport.ErrLimit) {
		t.Fatal("hash work was not bounded", err)
	}
	<-s.gate
	db.Close()
	if result, err := s.Authenticate(context.Background(), "alice", password); !errors.Is(err, authport.ErrUnavailable) || result.ID != "" {
		t.Fatal("unavailable store authenticated", err)
	}
}

func BenchmarkPasswordVerification(b *testing.B) {
	hash, err := makeHash(password)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if matched, err := verifyHash(password, hash); err != nil || !matched {
			b.Fatal(err)
		}
	}
}
