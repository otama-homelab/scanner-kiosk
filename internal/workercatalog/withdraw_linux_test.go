//go:build linux

package workercatalog

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workeridentity"
)

func TestWithdrawPriorRevokesAdmissionWithoutReleasingWork(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "admitted"}[accepted], func(t *testing.T) {
			f, records, lease, _, request, limits := readyFixture(t)
			ctx := context.Background()
			if err := f.store.Publish(ctx, lease, snapshot()); err != nil {
				t.Fatal(err)
			}
			var original documents.ScanAdmission
			var err error
			if accepted {
				original, err = records.AdmitReadyScan(ctx, readyOwner, request, limits)
				if err != nil {
					t.Fatal(err)
				}
			}
			jobs := readyCount(t, f.db, "SELECT count(*) FROM jobs")
			reservation := readyCount(t, f.db, "SELECT COALESCE(sum(byte_size),0) FROM reservations")
			if err = f.guard.Close(); err != nil {
				t.Fatal(err)
			}
			successor, err := workeridentity.Acquire(f.root)
			if err != nil {
				t.Fatal(err)
			}
			defer successor.Close()
			if _, err = f.store.ReadOCRCatalog(ctx); err != nil {
				t.Fatal("fixture did not retain fresh predecessor metadata", err)
			}
			if err = f.store.WithdrawPrior(ctx, successor); err != nil {
				t.Fatal(err)
			}
			if err = f.store.WithdrawPrior(ctx, successor); err != nil {
				t.Fatal("withdrawal retry before Begin not idempotent", err)
			}
			if _, err = f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) {
				t.Fatal("OCR metadata survived", err)
			}
			if _, err = f.store.ReadScannerMetadata(ctx); !errors.Is(err, ErrUnavailable) {
				t.Fatal("native metadata survived", err)
			}
			if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 0 || readyCount(t, f.db, "SELECT count(*) FROM processing_coordinator WHERE state='stopped' AND generation=1") != 1 {
				t.Fatal("predecessor still authoritative")
			}
			if readyCount(t, f.db, "SELECT count(*) FROM jobs") != jobs || readyCount(t, f.db, "SELECT COALESCE(sum(byte_size),0) FROM reservations") != reservation {
				t.Fatal("withdrawal altered work/quota")
			}
			request.IdempotencyKey = "request_after_withdraw_01"
			if _, err = records.AdmitReadyScan(ctx, readyOwner, request, limits); !errors.Is(err, documents.ErrDeviceBusy) {
				t.Fatal("fresh admission after withdrawal", err)
			}
			if accepted {
				request.IdempotencyKey = "request_ready_0001"
				replay, err := records.AdmitReadyScan(ctx, readyOwner, request, limits)
				if err != nil || replay.JobID != original.JobID {
					t.Fatal("withdrawal broke exact receipt replay", err)
				}
				if required, err := records.RecoveryRequired(ctx); err != nil || !required {
					t.Fatal("held prior work disappeared", err)
				}
			} else {
				next, err := f.store.Begin(ctx, successor)
				if err != nil || next.generation != 2 {
					t.Fatal("successor generation", err)
				}
				if err = f.store.WithdrawPrior(ctx, successor); !errors.Is(err, ErrUnavailable) {
					t.Fatal("same guard withdrew its own active coordinator", err)
				}
				if readyCount(t, f.db, "SELECT count(*) FROM processing_coordinator WHERE state='active' AND generation=2") != 1 {
					t.Fatal("own authority changed")
				}
			}
		})
	}
}
func TestWithdrawPriorSerializesWithReadyAdmission(t *testing.T) {
	f, records, _, _, request, limits := readyFixture(t)
	if err := f.guard.Close(); err != nil {
		t.Fatal(err)
	}
	successor, err := workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	other, err := storage.Open(filepath.Join(f.root, "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	writer, err := New(other, func() time.Time { return time.UnixMilli(f.now) }, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var admitted documents.ScanAdmission
	var admissionErr, withdrawErr error
	go func() {
		defer wg.Done()
		<-start
		admitted, admissionErr = records.AdmitReadyScan(context.Background(), readyOwner, request, limits)
	}()
	go func() { defer wg.Done(); <-start; withdrawErr = writer.WithdrawPrior(context.Background(), successor) }()
	close(start)
	wg.Wait()
	if withdrawErr != nil {
		t.Fatal(withdrawErr)
	}
	if admissionErr == nil {
		if admitted.JobID == "" || readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 {
			t.Fatal("prior admission resources lost")
		}
		if required, err := records.RecoveryRequired(context.Background()); err != nil || !required {
			t.Fatal("prior admission not gated", err)
		}
	} else if !errors.Is(admissionErr, documents.ErrDeviceBusy) {
		t.Fatal(admissionErr)
	}
	request.IdempotencyKey = "request_after_withdraw_02"
	if _, err = records.AdmitReadyScan(context.Background(), readyOwner, request, limits); !errors.Is(err, documents.ErrDeviceBusy) {
		t.Fatal("post-withdraw admission succeeded", err)
	}
}
func TestWithdrawPriorRollbackAndGuardValidation(t *testing.T) {
	f, _, _, _, _, _ := readyFixture(t)
	if err := f.store.WithdrawPrior(context.Background(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil guard", err)
	}
	if err := f.store.WithdrawPrior(context.Background(), f.guard); !errors.Is(err, ErrUnavailable) {
		t.Fatal("own guard", err)
	}
	if err := f.guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.store.WithdrawPrior(context.Background(), f.guard); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed guard", err)
	}
	successor, err := workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	readyExec(t, f.db, `CREATE TRIGGER fail_withdraw BEFORE DELETE ON scanner_readiness BEGIN SELECT RAISE(ABORT,'fixture withdrawal failure'); END`)
	if err = f.store.WithdrawPrior(context.Background(), successor); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed withdrawal accepted", err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM processing_coordinator WHERE state='active'") != 1 || readyCount(t, f.db, "SELECT count(*) FROM scanner_metadata_snapshot") != 1 || readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 1 {
		t.Fatal("partial withdrawal committed")
	}
}
