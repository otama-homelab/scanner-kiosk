package oidcattempt_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/oidcattempt"
)

// This fixture deliberately has no SQL dependency. Atomicity belongs to the
// backend; protocol material and post-commit validation belong to the service.
type memoryRecords struct {
	mu           sync.Mutex
	record       oidcattempt.StoredMaterial
	command      oidcattempt.BeginRecord
	consumed     bool
	failure      error
	corrupt      bool
	afterConsume func()
}

func (r *memoryRecords) Begin(_ context.Context, c oidcattempt.BeginRecord) (oidcattempt.StoredMaterial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record.ExpiresAtMS == 0 {
		r.record = c.Proposed
		r.command = c
	}
	if r.failure != nil {
		return r.record, r.failure
	}
	if c.BrowserHash != r.command.BrowserHash || c.KeyHash != r.command.KeyHash || c.ConfigurationHash != r.command.ConfigurationHash {
		return oidcattempt.StoredMaterial{}, oidcattempt.ErrConflict
	}
	if r.consumed {
		return oidcattempt.StoredMaterial{}, oidcattempt.ErrGone
	}
	record := r.record
	if r.corrupt {
		record.StateHash = [32]byte{}
	}
	return record, nil
}
func (r *memoryRecords) Consume(_ context.Context, c oidcattempt.ConsumeRecord) (oidcattempt.StoredMaterial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.consumed || c.StateHash != r.record.StateHash || c.BrowserHash != r.command.BrowserHash || c.ConfigurationHash != r.command.ConfigurationHash || c.NowMS >= r.record.ExpiresAtMS {
		return oidcattempt.StoredMaterial{}, oidcattempt.ErrDenied
	}
	r.consumed = true
	if r.afterConsume != nil {
		r.afterConsume()
	}
	return r.record, r.failure
}
func TestRecordsBackendReplayConcurrentConsumeAndRedaction(t *testing.T) {
	now := time.Unix(1700000000, 0)
	records := &memoryRecords{}
	service, err := oidcattempt.NewWithRecords(records, oidcattempt.Options{Lifetime: time.Minute, MaxPending: 2, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	browser := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	configuration := sha256.Sum256([]byte("configuration"))
	initial, err := service.BeginRequest(context.Background(), browser, "request_key_123456", configuration)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.BeginRequest(context.Background(), browser, "request_key_123456", configuration)
	if err != nil || initial.State() != replay.State() || initial.Verifier() != replay.Verifier() || replay.Consumed() {
		t.Fatal("unstable replay", err)
	}
	if records.command.BrowserHash != sha256.Sum256([]byte(browser)) || !records.command.RecordRequest {
		t.Fatal("missing hashed browser binding")
	}
	for _, value := range []any{records.command, records.record, oidcattempt.ConsumeRecord{BrowserHash: records.command.BrowserHash}, initial} {
		encoded, err := json.Marshal(value)
		if err != nil || string(encoded) != `"[redacted]"` {
			t.Fatal("unredacted JSON")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			out := fmt.Sprintf(format, value)
			if out != "[redacted]" || strings.Contains(out, initial.Nonce()) {
				t.Fatal("unredacted formatting")
			}
		}
	}
	var wg sync.WaitGroup
	successes := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, e := service.ConsumeRequest(context.Background(), initial.State(), browser, configuration)
			successes <- e == nil && m.Consumed()
		}()
	}
	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("successful consumes=%d", count)
	}
	if _, err = service.BeginRequest(context.Background(), browser, "request_key_123456", configuration); !errors.Is(err, oidcattempt.ErrGone) {
		t.Fatal("consumed request resurrected", err)
	}
}
func TestRecordsBackendUncertainOrCorruptNeverReleasesMaterial(t *testing.T) {
	for _, mode := range []string{"begin-uncertain", "begin-corrupt", "consume-uncertain", "expired-after-commit", "canceled-after-commit"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			records := &memoryRecords{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service, err := oidcattempt.NewWithRecords(records, oidcattempt.Options{Lifetime: time.Minute, MaxPending: 2, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			browser := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			if mode == "begin-uncertain" {
				records.failure = oidcattempt.ErrUncertain
			}
			if mode == "begin-corrupt" {
				records.corrupt = true
			}
			m, err := service.BeginRequest(ctx, browser, "request_key_123456", [32]byte{})
			if strings.HasPrefix(mode, "begin-") {
				if err == nil || m.State() != "" || m.Nonce() != "" || m.Verifier() != "" {
					t.Fatal("unconfirmed begin exposed material")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state := m.State()
			if mode == "consume-uncertain" {
				records.failure = oidcattempt.ErrUncertain
			}
			if mode == "expired-after-commit" {
				records.afterConsume = func() { now = now.Add(time.Minute) }
			}
			if mode == "canceled-after-commit" {
				records.afterConsume = cancel
			}
			m, err = service.ConsumeRequest(ctx, state, browser, [32]byte{})
			if err == nil || m.Consumed() || m.State() != "" || m.Verifier() != "" {
				t.Fatal("unconfirmed consume exposed material")
			}
		})
	}
}
