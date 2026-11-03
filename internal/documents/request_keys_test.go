package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"scanner-kiosk/internal/requestkey"
)

func agedKey(now int64, nonce int) string { return fmt.Sprintf("rq1_%013d_%032x", now, nonce) }
func TestAgedScanReceiptPurgeNeverReexecutesAfterClockRollback(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	request := scanRequest()
	request.IdempotencyKey = agedKey(f.now, 1)
	limits := scanLimits()
	limits.DocumentLifetime = 72 * time.Hour
	first, err := f.store.AdmitScan(ctx, alice, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.AbortUnstartedScan(ctx, alice, first.JobID, runner); err != nil {
		t.Fatal(err)
	}
	// No receipt removal while either its replay window or key window is open.
	f.now += (25 * time.Hour).Milliseconds()
	replay, err := f.store.AdmitScan(ctx, alice, request, limits)
	if err != nil || !replay.Replay || replay.JobID != first.JobID || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("receipt replay after key age changed work", err)
	}
	if n, err := f.store.PurgeExpiredRequestReceipts(ctx, 100); err != nil || n != 0 {
		t.Fatal("live receipt purged", n, err)
	}
	f.now = 2000 + (72 * time.Hour).Milliseconds()
	if n, err := f.store.PurgeExpiredRequestReceipts(ctx, 100); err != nil || n != 1 {
		t.Fatal("aged receipt retained", n, err)
	}
	if _, err = f.store.AdmitScan(ctx, alice, request, scanLimits()); !errors.Is(err, ErrGone) {
		t.Fatal("purged scan key admitted", err)
	}
	f.now = 2000
	if _, err = f.store.AdmitScan(ctx, alice, request, scanLimits()); !errors.Is(err, ErrStorage) {
		t.Fatal("clock rollback bypassed retirement fence", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("old native request recreated work")
	}
	f.now = 2000 + (72 * time.Hour).Milliseconds()
	request.IdempotencyKey = agedKey(f.now, 2)
	if next, err := f.store.AdmitScan(ctx, alice, request, scanLimits()); err != nil || next.JobID == first.JobID {
		t.Fatal("fresh intent unavailable after retirement", err)
	}
}
func TestAgedKeysRecordedAcrossAllDocumentCommands(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"render", "edit", "delete", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			f := editable(t)
			key := agedKey(f.now, 1)
			var err error
			switch operation {
			case "render":
				r := admissionRequest()
				r.IdempotencyKey = key
				_, err = f.store.AdmitRender(ctx, alice, r, admissionLimits())
			case "edit":
				r := rotation()
				r.IdempotencyKey = key
				_, err = f.store.Edit(ctx, alice, r, time.Hour)
			case "delete":
				_, err = f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: 1, IdempotencyKey: key}, time.Hour)
			case "cancel":
				must(t, f.docs, "UPDATE jobs SET state='queued' WHERE id=?", jobA)
				_, err = f.store.CancelJob(ctx, alice, jobA, key, time.Hour)
			}
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(key))
			var expiry int64
			if err = f.docs.QueryRow("SELECT key_expires_at_ms FROM request_receipts WHERE key_hash=?", hash[:]).Scan(&expiry); err != nil || expiry != f.now+requestkey.Lifetime.Milliseconds() {
				t.Fatal("missing immutable key age", expiry, err)
			}
		})
	}
}
func TestAgedReceiptPurgeBoundsLegacyRowsEditCascadeAndUncertainCommit(t *testing.T) {
	for _, mode := range []string{"normal", "rollback", "committed"} {
		t.Run(mode, func(t *testing.T) {
			f := editable(t)
			ctx := context.Background()
			r := rotation()
			r.IdempotencyKey = agedKey(f.now, 1)
			if _, err := f.store.Edit(ctx, alice, r, time.Hour); err != nil {
				t.Fatal(err)
			}
			// Receipt expired after one hour, but key remains fresh for 24 hours.
			f.now += time.Hour.Milliseconds()
			if n, err := f.store.PurgeExpiredRequestReceipts(ctx, 100); err != nil || n != 0 {
				t.Fatal("fresh key receipt purged", n, err)
			}
			for i := 0; i < 102; i++ {
				hash := sha256.Sum256([]byte(fmt.Sprint(i)))
				must(t, f.docs, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms,key_expires_at_ms) VALUES(?,?,'render',?,1,86400000)`, bob, hash[:], hash[:])
			}
			legacy := sha256.Sum256([]byte("legacy_keep"))
			must(t, f.docs, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms) VALUES(?,?,'render',?,1)`, bob, legacy[:], legacy[:])
			f.now = 2000 + requestkey.Lifetime.Milliseconds()
			if mode != "normal" {
				f.store.commit = func(tx *sql.Tx) error {
					if mode == "committed" {
						if err := tx.Commit(); err != nil {
							return err
						}
					}
					return errors.New("lost acknowledgement")
				}
			}
			n, err := f.store.PurgeExpiredRequestReceipts(ctx, 100)
			if mode == "normal" {
				if err != nil || n != 100 {
					t.Fatal(n, err)
				}
			} else if !errors.Is(err, ErrCommitUncertain) || n != 0 {
				t.Fatal("unconfirmed purge acknowledged", n, err)
			}
			if mode == "rollback" {
				if scalar(t, f.docs, "SELECT count(*) FROM request_receipts WHERE key_expires_at_ms IS NOT NULL") != 103 || scalar(t, f.docs, "SELECT retired_at_ms FROM request_retirement_fence") != 0 {
					t.Fatal("partial purge/fence survived rollback")
				}
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			for i := 0; i < 3; i++ {
				if _, err = f.store.PurgeExpiredRequestReceipts(ctx, 100); err != nil {
					t.Fatal(err)
				}
			}
			if scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 1 || scalar(t, f.docs, "SELECT count(*) FROM edit_results") != 0 || int64(scalar(t, f.docs, "SELECT retired_at_ms FROM request_retirement_fence")) != f.now {
				t.Fatal("legacy retention/cascade/fence incorrect")
			}
		})
	}
}
func TestExpiredVersionedKeysFailBeforeDocumentOrCapabilityWork(t *testing.T) {
	f := renderFixture(t)
	ctx := context.Background()
	f.now = requestkey.Lifetime.Milliseconds() + 2000
	r := admissionRequest()
	r.IdempotencyKey = agedKey(2000, 1)
	if _, _, err := f.store.FindRender(ctx, alice, r); !errors.Is(err, ErrGone) {
		t.Fatal("capability preflight accepted expired key", err)
	}
	if _, err := f.store.AdmitRender(ctx, alice, r, admissionLimits()); !errors.Is(err, ErrGone) {
		t.Fatal("render admitted expired key", err)
	}
	e := rotation()
	e.IdempotencyKey = r.IdempotencyKey
	if _, err := f.store.Edit(ctx, alice, e, time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("edit admitted expired key", err)
	}
	if _, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: 1, IdempotencyKey: r.IdempotencyKey}, time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("delete admitted expired key", err)
	}
	if _, err := f.store.CancelJob(ctx, alice, jobA, r.IdempotencyKey, time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("cancel admitted expired key", err)
	}
}
