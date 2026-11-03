package documents

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
)

func TestClaimNextScanIsExclusiveAndBootBound(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.store.ClaimNextScan(context.Background(), "worker_foreign_0001"); !errors.Is(err, ErrNoWork) || claimed.Job.ID != "" {
		t.Fatal("foreign boot claimed physical scan", err)
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Go(func() {
			claimed, err := f.store.ClaimNextScan(context.Background(), runner)
			if err == nil && (claimed.OwnerID != alice || claimed.Job.ID != a.JobID) {
				err = errors.New("wrong claimed work")
			}
			results <- err
		})
	}
	wait.Wait()
	close(results)
	success, idle := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrNoWork) {
			idle++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || idle != 1 {
		t.Fatal("scan claimed twice", success, idle)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='running' AND started_at_ms=2000", a.JobID) != 1 {
		t.Fatal("missing immutable claim")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held' AND epoch=2", a.JobID) != 1 {
		t.Fatal("claim released lease")
	}
}

func TestClaimNextScanRejectsLostAuthority(t *testing.T) {
	for _, mode := range []string{"disabled", "expired", "cancelled", "started", "revision", "lease-epoch", "reconciling", "reservation", "quota", "space-error"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "expired":
				f.now += 100000000
			case "cancelled":
				if err := f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "started":
				must(t, f.docs, "UPDATE jobs SET started_at_ms=2000 WHERE id=?", a.JobID)
			case "revision":
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", a.DocumentID)
			case "lease-epoch":
				must(t, f.docs, "UPDATE device_leases SET epoch=epoch+1 WHERE job_id=?", a.JobID)
			case "reconciling":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
			case "reservation":
				must(t, f.docs, "DELETE FROM reservations WHERE job_id=?", a.JobID)
			case "quota":
				f.store.space = func(context.Context) (int64, error) { return 0, nil }
			case "space-error":
				f.store.space = func(context.Context) (int64, error) { return 0, errors.New("injected space probe failure") }
			}
			claimed, err := f.store.ClaimNextScan(context.Background(), runner)
			if err == nil || claimed.Job.ID != "" {
				t.Fatal("unavailable scan dispatched", mode, err)
			}
			if mode == "quota" && !errors.Is(err, ErrQuota) {
				t.Fatal("wrong quota failure", err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='running'", a.JobID) != 0 {
				t.Fatal("failed claim was not rolled back")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("claim freed lease")
			}
			if mode != "reservation" && scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("claim freed reservation")
			}
		})
	}
}

func TestClaimNextScanCommitUncertaintyReturnsNoWork(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected uncertain commit")
	}
	claimed, err := f.store.ClaimNextScan(context.Background(), runner)
	if !errors.Is(err, ErrCommitUncertain) || claimed.Job.ID != "" || claimed.OwnerID != "" {
		t.Fatal("uncertain claim escaped", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='running'", a.JobID) != 1 {
		t.Fatal("fixture did not commit")
	}
	if claimed, err := f.store.ClaimNextScan(context.Background(), runner); !errors.Is(err, ErrNoWork) || claimed.Job.ID != "" {
		t.Fatal("uncertain claim automatically retried", err)
	}
}

func TestScanDispatchBlocksOtherBootHeavyWorkAndTerminalHeldLease(t *testing.T) {
	for _, mode := range []string{"running", "reconciling", "terminal-held"} {
		t.Run(mode, func(t *testing.T) {
			f := scanFixture(t)
			if _, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits()); err != nil {
				t.Fatal(err)
			}
			must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES('scanner_second_0001',1,'idle')")
			request := scanRequest()
			request.DeviceID, request.IdempotencyKey = "scanner_second_0001", "request_second_0001"
			limits := scanLimits()
			limits.MaxConcurrentScans = 2
			a, err := f.store.AdmitScan(context.Background(), bob, request, limits)
			if err != nil {
				t.Fatal(err)
			}
			state := mode
			if mode == "terminal-held" {
				state = "failed"
			}
			must(t, f.docs, "UPDATE jobs SET state=?,runner_id='worker_other_boot_001' WHERE id=?", state, a.JobID)
			if claimed, err := f.store.ClaimNextScan(context.Background(), runner); !errors.Is(err, ErrNoWork) || claimed.Job.ID != "" {
				t.Fatal("other boot/held lease ignored", err)
			}
		})
	}
}

func TestRenderDispatchBlocksTerminalScanLease(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	must(t, f.docs, "UPDATE jobs SET state='failed' WHERE id=?", a.JobID)
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docB)
	seedPage(t, f, docB, bob, "original_bob_00001", "page_bob_00000001")
	request := admissionRequest()
	request.DocumentID = docB
	if _, err := f.store.AdmitRender(context.Background(), bob, request, admissionLimits()); err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.store.ClaimNextRender(context.Background(), runner); !errors.Is(err, ErrNoWork) || claimed.Job.ID != "" {
		t.Fatal("terminal held lease allowed render dispatch", err)
	}
}

func TestClaimNextScanClockRollbackRetainsAdmission(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	admitted, err := f.store.AdmitScan(ctx, alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	admittedAt := f.now
	f.now--
	claimed, err := f.store.ClaimNextScan(ctx, runner)
	if !errors.Is(err, ErrStorage) || claimed.Job.ID != "" || claimed.OwnerID != "" {
		t.Fatal("clock rollback escaped the storage constraint", claimed, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='queued' AND started_at_ms IS NULL", admitted.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admitted.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", admitted.JobID) != 1 {
		t.Fatal("failed claim changed the admitted work")
	}
	f.now = admittedAt
	claimed, err = f.store.ClaimNextScan(ctx, runner)
	if err != nil || claimed.Job.ID != admitted.JobID || claimed.OwnerID != alice {
		t.Fatal("valid clock could not claim unchanged admission", claimed, err)
	}
}
