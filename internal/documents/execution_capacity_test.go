package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecutionCapacityRecheckRollsBackTentativeClaim(t *testing.T) {
	for _, kind := range []string{"scan", "render"} {
		for _, mode := range []string{"full", "probe-error", "clock-expired"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var f *fixture
				var jobID string
				if kind == "scan" {
					f = scanFixture(t)
					a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
					if err != nil {
						t.Fatal(err)
					}
					jobID = a.JobID
				} else {
					f = renderFixture(t)
					a, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
					if err != nil {
						t.Fatal(err)
					}
					jobID = a.JobID
				}
				f.store.space = func(context.Context) (int64, error) {
					switch mode {
					case "full":
						return 49, nil
					case "probe-error":
						return 0, errors.New("probe failure")
					case "clock-expired":
						f.now += time.Hour.Milliseconds()
						return 1 << 50, nil
					}
					return 0, nil
				}
				var err error
				if kind == "scan" {
					_, err = f.store.ClaimScan(context.Background(), alice, jobID, runner)
				} else {
					_, err = f.store.ClaimExport(context.Background(), alice, jobID, runner)
				}
				if err == nil {
					t.Fatal("execution began without current capacity/deadline")
				}
				if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='queued' AND started_at_ms IS NULL AND heartbeat_at_ms IS NULL", jobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobID) != 1 {
					t.Fatal("failed preflight marked work started or freed reservation")
				}
				if kind == "scan" && scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", jobID) != 1 {
					t.Fatal("capacity failure inferred physical termination")
				}
			})
		}
	}
}
func TestExecutionPreflightDoesNotReserveItsOwnBudgetTwice(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.store.space = func(context.Context) (int64, error) { return 50, nil }
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); err != nil {
		t.Fatalf("existing budget charged as a new reservation: %v", err)
	}
	if scalar(t, f.docs, "SELECT COALESCE(SUM(byte_size),0) FROM reservations") != 50 {
		t.Fatal("claim duplicated reservation")
	}
}

func TestPreflightFailureCanAbortWithoutInventingPhysicalStop(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	f.store.space = func(context.Context) (int64, error) { return 0, nil }
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrQuota) {
		t.Fatal("expected pre-launch capacity refusal")
	}
	if err = f.store.AbortUnstartedScan(context.Background(), alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='idle' AND job_id IS NULL") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='interrupted' AND started_at_ms IS NULL", a.JobID) != 1 {
		t.Fatal("unstarted job not terminated atomically")
	}
	f.store.space = fixtureSpace
	if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("old request ran after storage recovered")
	}
}
func TestAbortUnstartedCannotReleaseStartedOrForeignWork(t *testing.T) {
	f, a, _ := startedCapture(t)
	if err := f.store.AbortUnstartedScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("started work treated as unstarted")
	}
	if err := f.store.AbortUnstartedScan(context.Background(), bob, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign work released")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held'") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("active resources released without stop evidence")
	}
}
func TestUnstartedAbortAndClaimRaceOnlyOneWins(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	other := secondStore(t, f)
	results := make(chan error, 2)
	go func() { _, err := f.store.ClaimScan(context.Background(), alice, a.JobID, runner); results <- err }()
	go func() { results <- other.AbortUnstartedScan(context.Background(), alice, a.JobID, runner) }()
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("claim and abort both won")
	}
}

func TestUnstartedAbortDoesNotClearReconcilingDevice(t *testing.T) {
	f := scanFixture(t)
	a, err := f.store.AdmitScan(context.Background(), alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE job_id=?", a.JobID)
	if err = f.store.AbortUnstartedScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("unknown device activity treated as idle")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='reconciling'") != 1 {
		t.Fatal("reconciliation fence removed")
	}
}
