package oidcattempt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
)

func fixture(t *testing.T) (*Store, *sql.DB, string, string, *time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.sqlite3")
	db, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler, err := websession.New(db, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "oidc", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || len(response.Result().Cookies()) != 1 {
		t.Fatal("no real SCS browser session")
	}
	now := time.Now()
	store, err := New(db, Options{Lifetime: 5 * time.Minute, MaxPending: 2, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return store, db, path, response.Result().Cookies()[0].Value, &now
}
func TestAttemptBoundToActiveSessionAndConsumedOnce(t *testing.T) {
	store, db, _, browser, _ := fixture(t)
	ctx := context.Background()
	material, err := store.Begin(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	if !token(material.State()) || !token(material.Nonce()) || !token(material.Verifier()) || material.Challenge() == material.Verifier() {
		t.Fatal("invalid random or PKCE material")
	}
	otherHandler, err := websession.New(db, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "oidc", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	otherResponse := httptest.NewRecorder()
	otherHandler.ServeHTTP(otherResponse, httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
	if otherResponse.Code != 200 || len(otherResponse.Result().Cookies()) != 1 {
		t.Fatal("missing second active browser")
	}
	wrong := otherResponse.Result().Cookies()[0].Value
	if value, err := store.Consume(ctx, material.State(), wrong); !errors.Is(err, ErrDenied) || value.Nonce() != "" {
		t.Fatal("foreign browser consumed state", err)
	}
	foreignState, err := random()
	if err != nil {
		t.Fatal(err)
	}
	if value, err := store.Consume(ctx, foreignState, browser); !errors.Is(err, ErrDenied) || value.Nonce() != "" {
		t.Fatal("foreign state accepted", err)
	}
	value, err := store.Consume(ctx, material.State(), browser)
	if err != nil || value.Nonce() != material.Nonce() || value.Verifier() != material.Verifier() {
		t.Fatal("bound consume failed", err)
	}
	if value, err = store.Consume(ctx, material.State(), browser); !errors.Is(err, ErrDenied) || value.Verifier() != "" {
		t.Fatal("state replay allowed", err)
	}
	var stateHash, binding []byte
	if err = db.QueryRow("SELECT state_hash,session_hash FROM auth_attempts").Scan(&stateHash, &binding); err != nil {
		t.Fatal(err)
	}
	expectedState := sha256.Sum256([]byte(material.State()))
	expectedBinding := sha256.Sum256([]byte(browser))
	if string(stateHash) != string(expectedState[:]) || string(binding) != string(expectedBinding[:]) {
		t.Fatal("raw state or cookie stored")
	}
	for _, formatted := range []string{fmt.Sprint(material), fmt.Sprintf("%#v", material)} {
		if strings.Contains(formatted, material.Nonce()) || strings.Contains(formatted, material.Verifier()) {
			t.Fatal("implicit formatting leaked secrets")
		}
	}
	encoded, err := json.Marshal(material)
	if err != nil || string(encoded) != `"[redacted]"` {
		t.Fatal("implicit JSON exposed auth material")
	}
}
func TestAttemptExpiryQuotaAndClockRollback(t *testing.T) {
	store, _, _, browser, now := fixture(t)
	ctx := context.Background()
	first, err := store.Begin(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Begin(ctx, browser); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Begin(ctx, browser); !errors.Is(err, ErrLimit) || value.State() != "" {
		t.Fatal("pending limit bypassed", err)
	}
	*now = now.Add(-time.Minute)
	if value, err := store.Consume(ctx, first.State(), browser); !errors.Is(err, ErrDenied) || value.Verifier() != "" {
		t.Fatal("future-created attempt consumed", err)
	}
	*now = now.Add(6 * time.Minute)
	if value, err := store.Consume(ctx, first.State(), browser); !errors.Is(err, ErrDenied) || value.Verifier() != "" {
		t.Fatal("expired attempt consumed", err)
	}
	if _, err = store.Begin(ctx, browser); err != nil {
		t.Fatal("expired attempts still counted pending", err)
	}
}
func TestRevokedSessionCannotStartOrConsumeAttempt(t *testing.T) {
	store, db, _, browser, _ := fixture(t)
	ctx := context.Background()
	material, err := store.Begin(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(browser))
	stored := base64.RawURLEncoding.EncodeToString(hash[:])
	now := time.Now()
	if _, err = db.Exec("INSERT INTO session_revocations(token,revoked_at_ms,retain_until_ms) VALUES(?,?,?)", stored, now.UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Begin(ctx, browser); !errors.Is(err, ErrDenied) || value.State() != "" {
		t.Fatal("revoked browser started login", err)
	}
	if value, err := store.Consume(ctx, material.State(), browser); !errors.Is(err, ErrDenied) || value.Nonce() != "" {
		t.Fatal("revoked browser completed login", err)
	}
}
func TestTwoConnectionsConsumeOnlyOnce(t *testing.T) {
	store, _, path, browser, _ := fixture(t)
	material, err := store.Begin(context.Background(), browser)
	if err != nil {
		t.Fatal(err)
	}
	otherDB, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer otherDB.Close()
	other, err := New(otherDB, Options{Lifetime: 5 * time.Minute, MaxPending: 2, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*Store{store, other} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			_, err := s.Consume(context.Background(), material.State(), browser)
			results <- err
		}(candidate)
	}
	wg.Wait()
	close(results)
	accepted, denied := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrDenied) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatal("concurrent consume was not one-time")
	}
}
func TestUncertainCommitNeverReturnsProtocolMaterial(t *testing.T) {
	for _, operation := range []string{"begin", "consume"} {
		t.Run(operation, func(t *testing.T) {
			store, _, _, browser, _ := fixture(t)
			ctx := context.Background()
			material, err := store.Begin(ctx, browser)
			if err != nil {
				t.Fatal(err)
			}
			store.records.(*SQLiteRecords).commit = func(tx *sql.Tx) error {
				if err := tx.Commit(); err != nil {
					return err
				}
				return errors.New("injected lost acknowledgment")
			}
			var value Material
			if operation == "begin" {
				value, err = store.Begin(ctx, browser)
			} else {
				value, err = store.Consume(ctx, material.State(), browser)
			}
			if !errors.Is(err, ErrUncertain) || value.State() != "" || value.Nonce() != "" || value.Verifier() != "" {
				t.Fatal("uncertain transaction granted authority", err)
			}
			if operation == "consume" {
				if _, err = store.Consume(ctx, material.State(), browser); !errors.Is(err, ErrDenied) {
					t.Fatal("committed uncertain consume replayed", err)
				}
			}
		})
	}
}
func TestExpiredMissingAndMalformedBrowserDenied(t *testing.T) {
	for _, mode := range []string{"missing", "expired", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			store, db, _, browser, _ := fixture(t)
			material, err := store.Begin(context.Background(), browser)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				if _, err := db.Exec("DELETE FROM sessions"); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := db.Exec("DROP TRIGGER session_update_not_revoked; UPDATE sessions SET expiry=0"); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				browser = "not-a-session"
			}
			if value, err := store.Begin(context.Background(), browser); !errors.Is(err, ErrDenied) || value.State() != "" {
				t.Fatal("unavailable browser started attempt", err)
			}
			if value, err := store.Consume(context.Background(), material.State(), browser); !errors.Is(err, ErrDenied) || value.Verifier() != "" {
				t.Fatal("unavailable browser consumed state", err)
			}
		})
	}
}

func TestIdempotentRequestResumesOnlyOriginalPendingMaterial(t *testing.T) {
	store, db, _, browser, now := fixture(t)
	ctx := context.Background()
	configuration := sha256.Sum256([]byte("fixed provider and callback"))
	key := "request_oidc_start_001"
	first, err := store.BeginRequest(ctx, browser, key, configuration)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(db, store.options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.BeginRequest(ctx, browser, key, configuration)
	if err != nil || again.State() != first.State() || again.Nonce() != first.Nonce() || again.Verifier() != first.Verifier() {
		t.Fatal("retry/restart changed authorization request", err)
	}
	changed := sha256.Sum256([]byte("different provider or callback"))
	if material, err := store.BeginRequest(ctx, browser, key, changed); !errors.Is(err, ErrConflict) || material.State() != "" {
		t.Fatal("changed provider reused original intent", err)
	}
	if material, err := store.ConsumeRequest(ctx, first.State(), browser, changed); !errors.Is(err, ErrDenied) || material.Verifier() != "" {
		t.Fatal("changed provider consumed original request", err)
	}
	if _, err = store.ConsumeRequest(ctx, first.State(), browser, configuration); err != nil {
		t.Fatal(err)
	}
	if material, err := store.BeginRequest(ctx, browser, key, configuration); !errors.Is(err, ErrGone) || material.State() != "" {
		t.Fatal("used start was recreated", err)
	}
	second, err := store.BeginRequest(ctx, browser, "request_oidc_start_002", configuration)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(6 * time.Minute)
	if material, err := store.BeginRequest(ctx, browser, "request_oidc_start_002", configuration); !errors.Is(err, ErrGone) || material.State() != "" {
		t.Fatal("expired start was recreated", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM auth_attempts").Scan(&count); err != nil || count != 2 {
		t.Fatal("retry created extra attempts", err)
	}
	if second.State() == first.State() {
		t.Fatal("distinct intents shared state")
	}
}
func TestRequestTombstoneSurvivesProtocolMaterialRemoval(t *testing.T) {
	store, db, _, browser, _ := fixture(t)
	ctx := context.Background()
	configuration := sha256.Sum256([]byte("fixed provider"))
	if _, err := store.BeginRequest(ctx, browser, "request_oidc_start_001", configuration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM auth_attempts"); err != nil {
		t.Fatal(err)
	}
	if material, err := store.BeginRequest(ctx, browser, "request_oidc_start_001", configuration); !errors.Is(err, ErrGone) || material.Verifier() != "" {
		t.Fatal("deleted material was regenerated", err)
	}
}
