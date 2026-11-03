package documents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func multiScanFixture(t *testing.T) (*fixture, ScanLimits, []ScanRequest) {
	t.Helper()
	f := scanFixture(t)
	limits := scanLimits()
	limits.MaxConcurrentScans = 2
	requests := []ScanRequest{scanRequest(), scanRequest(), scanRequest()}
	for i := range requests {
		requests[i].IdempotencyKey = fmt.Sprintf("request_multi_%04d", i)
		if i > 0 {
			requests[i].DeviceID = fmt.Sprintf("scanner_multi_%04d", i)
			must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", requests[i].DeviceID)
		}
	}
	return f, limits, requests
}

func TestConcurrentScanSlotsAdmitDifferentDevicesWithinLimit(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f, limits, requests := multiScanFixture(t)
			limits.MaxConcurrentScans = limit
			stores := []*Store{f.store, secondStore(t, f), secondStore(t, f)}
			start := make(chan struct{})
			results := make(chan error, 3)
			var wait sync.WaitGroup
			for i, store := range stores {
				wait.Go(func() {
					<-start
					owner := alice
					if i == 1 {
						owner = bob
					}
					_, err := store.AdmitScan(context.Background(), owner, requests[i], limits)
					results <- err
				})
			}
			close(start)
			wait.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else if !errors.Is(err, ErrDeviceBusy) {
					t.Fatal(err)
				}
			}
			if success != limit || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != limit || scalar(t, f.docs, "SELECT count(*) FROM reservations") != limit || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held'") != limit {
				t.Fatal("scan capacity over/under admitted", success, limit)
			}
		})
	}
}

func TestConcurrentScanSlotsClaimBothDevicesExactlyOnce(t *testing.T) {
	f, limits, requests := multiScanFixture(t)
	for i, owner := range []string{alice, bob} {
		if _, err := f.store.AdmitScan(context.Background(), owner, requests[i], limits); err != nil {
			t.Fatal(err)
		}
	}
	stores := []*Store{f.store, secondStore(t, f), secondStore(t, f)}
	results := make(chan ClaimedScan, 3)
	errs := make(chan error, 3)
	var wait sync.WaitGroup
	for _, store := range stores {
		wait.Go(func() {
			value, err := store.ClaimNextScanWithLimit(context.Background(), runner, 2)
			results <- value
			errs <- err
		})
	}
	wait.Wait()
	close(results)
	close(errs)
	success, noWork := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrNoWork) {
			noWork++
		} else {
			t.Fatal(err)
		}
	}
	ids := map[string]bool{}
	for value := range results {
		if value.Job.ID != "" {
			if ids[value.Job.ID] {
				t.Fatal("duplicate claimed scan")
			}
			ids[value.Job.ID] = true
		}
	}
	if success != 2 || noWork != 1 || len(ids) != 2 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan' AND state='running'") != 2 {
		t.Fatal(success, noWork, ids)
	}
	// Both are still fenced by their own lease, owner and reservation after claim.
	for _, owner := range []string{alice, bob} {
		var job string
		if err := f.docs.QueryRow("SELECT id FROM jobs WHERE owner_id=? AND kind='scan'", owner).Scan(&job); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.LoadScanWork(context.Background(), owner, job, runner); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanSlotsSameDeviceAndReplayNeverAcquireAnotherSlot(t *testing.T) {
	f, limits, requests := multiScanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, requests[0], limits)
	if err != nil {
		t.Fatal(err)
	}
	same := requests[0]
	same.IdempotencyKey = "request_same_device_001"
	if _, err = f.store.AdmitScan(context.Background(), bob, same, limits); !errors.Is(err, ErrDeviceBusy) {
		t.Fatal("same device double writer", err)
	}
	if _, err = f.store.AdmitScan(context.Background(), bob, requests[1], limits); err != nil {
		t.Fatal(err)
	}
	requests[0].WorkerBootID = "worker_other_boot_001"
	replay, err := f.store.AdmitScan(context.Background(), alice, requests[0], scanLimits())
	if err != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("full slots blocked exact replay", err)
	}
	changed := requests[0]
	changed.DeviceID = requests[2].DeviceID
	if _, err = f.store.AdmitScan(context.Background(), alice, changed, limits); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("replay moved device", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 2 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE device_id=? AND state='idle' AND epoch=1", requests[2].DeviceID) != 1 {
		t.Fatal("replay changed capacity/lease")
	}
}

func TestScanSlotsUnknownWorkRemainsRecoveryBarrier(t *testing.T) {
	for _, mode := range []string{"foreign-boot", "missing-runner", "missing-snapshot-boot", "cancel", "reconciling", "terminal-held", "reservation", "started-queued", "running-not-started", "running-future-start", "expired", "lease-epoch"} {
		t.Run(mode, func(t *testing.T) {
			f, limits, requests := multiScanFixture(t)
			a, err := f.store.AdmitScan(context.Background(), alice, requests[0], limits)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "foreign-boot":
				must(t, f.docs, "UPDATE jobs SET runner_id='worker_other_boot_001' WHERE id=?", a.JobID)
			case "missing-runner":
				must(t, f.docs, "UPDATE jobs SET runner_id=NULL WHERE id=?", a.JobID)
			case "missing-snapshot-boot":
				must(t, f.docs, "DROP TRIGGER job_snapshot_immutable")
				must(t, f.docs, "UPDATE jobs SET configuration_json=json_remove(configuration_json,'$.worker_boot_id') WHERE id=?", a.JobID)
			case "cancel":
				if err = f.store.RequestCancel(context.Background(), alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "reconciling":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
			case "terminal-held":
				must(t, f.docs, "UPDATE jobs SET state='failed' WHERE id=?", a.JobID)
			case "reservation":
				must(t, f.docs, "DELETE FROM reservations WHERE job_id=?", a.JobID)
			case "started-queued":
				must(t, f.docs, "UPDATE jobs SET started_at_ms=2000 WHERE id=?", a.JobID)
			case "running-not-started":
				must(t, f.docs, "UPDATE jobs SET state='running' WHERE id=?", a.JobID)
			case "running-future-start":
				must(t, f.docs, "UPDATE jobs SET state='running',started_at_ms=2001 WHERE id=?", a.JobID)
			case "expired":
				f.now += 100000
			case "lease-epoch":
				must(t, f.docs, "UPDATE device_leases SET epoch=epoch+1 WHERE job_id=?", a.JobID)
			}
			if _, err = f.store.AdmitScan(context.Background(), bob, requests[1], limits); !errors.Is(err, ErrDeviceBusy) {
				t.Fatal("unknown work admitted another capture", mode, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("recovery barrier freed work")
			}
		})
	}
}

func TestScanSlotsDirectClaimsAndUncertainCommitReturnNoAuthority(t *testing.T) {
	f, limits, requests := multiScanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, requests[0], limits)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.store.AdmitScan(context.Background(), bob, requests[1], limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("one-slot caller ignored existing reservation", err)
	}
	if _, err = f.store.ClaimScanWithLimit(context.Background(), alice, a.JobID, runner, 2); err != nil {
		t.Fatal(err)
	}
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("uncertain scan claim")
	}
	value, err := f.store.ClaimNextScanWithLimit(context.Background(), runner, 2)
	if !errors.Is(err, ErrCommitUncertain) || value.Job.ID != "" || value.OwnerID != "" {
		t.Fatal("uncertain claim leaked authority", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='running'", b.JobID) != 1 {
		t.Fatal("fixture did not commit second claim")
	}
}

func TestScanSlotsShareGlobalDiskAndLogicalBudgets(t *testing.T) {
	for _, mode := range []string{"logical", "disk"} {
		t.Run(mode, func(t *testing.T) {
			f, limits, requests := multiScanFixture(t)
			if _, err := f.store.AdmitScan(context.Background(), alice, requests[0], limits); err != nil {
				t.Fatal(err)
			}
			if mode == "logical" {
				limits.GlobalMaxBytes = 99
				limits.JobMaxBytes = 99
			} else {
				f.store.space = func(context.Context) (int64, error) { return 99, nil }
			}
			if _, err := f.store.AdmitScan(context.Background(), bob, requests[1], limits); !errors.Is(err, ErrQuota) {
				t.Fatal("parallel scan overcommitted shared budget", err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE device_id=? AND state='idle'", requests[1].DeviceID) != 1 {
				t.Fatal("rejected admission changed state")
			}
		})
	}
}

func TestScanSlotsRecheckOtherDeadlinesAfterCapacityProbe(t *testing.T) {
	for _, mode := range []string{"admit", "claim"} {
		t.Run(mode, func(t *testing.T) {
			f, limits, requests := multiScanFixture(t)
			short := limits
			short.JobTimeout = time.Millisecond
			if _, err := f.store.AdmitScan(context.Background(), alice, requests[0], short); err != nil {
				t.Fatal(err)
			}
			var candidate ScanAdmission
			if mode == "claim" {
				var err error
				candidate, err = f.store.AdmitScan(context.Background(), bob, requests[1], limits)
				if err != nil {
					t.Fatal(err)
				}
			}
			f.store.space = func(context.Context) (int64, error) { f.now = 2002; return 1 << 30, nil }
			if mode == "admit" {
				if _, err := f.store.AdmitScan(context.Background(), bob, requests[1], limits); !errors.Is(err, ErrDeviceBusy) {
					t.Fatal("peer expired during admission capacity probe", err)
				}
				if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 1 {
					t.Fatal("expired peer admission partially committed")
				}
			} else {
				if _, err := f.store.ClaimScanWithLimit(context.Background(), bob, candidate.JobID, runner, 2); !errors.Is(err, ErrConflict) {
					t.Fatal("peer expired during claim capacity probe", err)
				}
				if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='queued' AND started_at_ms IS NULL", candidate.JobID) != 1 {
					t.Fatal("expired peer claim partially committed")
				}
			}
		})
	}
}

func TestInvalidScanSlotLimitsProduceNoWrites(t *testing.T) {
	for _, limit := range []int{-1, 33} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f, limits, requests := multiScanFixture(t)
			limits.MaxConcurrentScans = limit
			if _, err := f.store.AdmitScan(context.Background(), alice, requests[0], limits); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
			if _, err := f.store.ClaimNextScanWithLimit(context.Background(), runner, limit); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
			if _, err := f.store.ClaimScanWithLimit(context.Background(), alice, "job_unknown_00001", runner, limit); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
				t.Fatal("invalid scan policy wrote state")
			}
		})
	}
}
