package documents

import (
	"context"
	"database/sql"
	"errors"
	"scanner-kiosk/internal/workwatch"
	"testing"
	"time"
)

func TestScanWorkAndPulsePreserveClaim(t *testing.T) {
	f, a, stop := startedCapture(t)
	ctx := context.Background()
	work, err := f.store.LoadScanWork(ctx, alice, a.JobID, runner)
	if err != nil || work.Job.ID != a.JobID || work.Job.DocumentID != a.DocumentID || work.Snapshot.LeaseEpoch != stop.LeaseEpoch || work.Snapshot.Capture != scanRequest().Capture || work.ReserveBytes != 50 {
		t.Fatalf("wrong work: %+v %v", work, err)
	}
	if err = f.store.PulseScan(ctx, alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	var start, heartbeat int64
	if err = f.docs.QueryRow("SELECT started_at_ms,heartbeat_at_ms FROM jobs WHERE id=?", a.JobID).Scan(&start, &heartbeat); err != nil || start != 2000 || heartbeat != 2300 {
		t.Fatal("pulse changed start or missed heartbeat", err)
	}
}
func TestScanWorkRejectsLostAuthority(t *testing.T) {
	for _, mode := range []string{"owner", "boot", "cancel", "disabled", "deadline", "ttl", "revision", "deleting", "lease-epoch", "lease-state", "reservation", "snapshot", "unknown-field", "photo-format", "clock-rollback"} {
		t.Run(mode, func(t *testing.T) {
			f, a, _ := startedCapture(t)
			ctx := context.Background()
			owner, boot := alice, runner
			switch mode {
			case "owner":
				owner = bob
			case "boot":
				boot = "worker_foreign_0001"
			case "cancel":
				if err := f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2300 WHERE id=?", alice)
			case "deadline":
				f.now += time.Minute.Milliseconds()
			case "ttl":
				f.now += (24 * time.Hour).Milliseconds()
			case "clock-rollback":
				f.now = 1999 // The immutable claim started at 2000.
			case "revision":
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", a.DocumentID)
			case "deleting":
				must(t, f.docs, "UPDATE documents SET state='deleting' WHERE id=?", a.DocumentID)
			case "lease-epoch":
				must(t, f.docs, "UPDATE device_leases SET epoch=epoch+1 WHERE device_id=?", scanner)
			case "lease-state":
				must(t, f.docs, "UPDATE device_leases SET state='reconciling' WHERE device_id=?", scanner)
			case "reservation":
				must(t, f.docs, "DELETE FROM reservations WHERE job_id=?", a.JobID)
			case "snapshot":
				// Corrupt persisted input deliberately; normal writes are trigger-protected.
				must(t, f.docs, "DROP TRIGGER job_snapshot_immutable")
				must(t, f.docs, "UPDATE jobs SET configuration_json=json_set(configuration_json,'$.device_id','scanner_foreign_001') WHERE id=?", a.JobID)
			case "unknown-field":
				// Corrupt persisted input deliberately; normal writes are trigger-protected.
				must(t, f.docs, "DROP TRIGGER job_snapshot_immutable")
				must(t, f.docs, "UPDATE jobs SET configuration_json=json_set(configuration_json,'$.unexpected',true) WHERE id=?", a.JobID)
			case "photo-format":
				// Corrupt persisted input deliberately; normal writes are trigger-protected.
				must(t, f.docs, "DROP TRIGGER job_snapshot_immutable")
				must(t, f.docs, "UPDATE jobs SET configuration_json=json_set(configuration_json,'$.photo_format','png') WHERE id=?", a.JobID)
			}
			work, err := f.store.LoadScanWork(ctx, owner, a.JobID, boot)
			if err == nil || work.Job.ID != "" {
				t.Fatal("invalid work exposed")
			}
			if mode == "clock-rollback" && !errors.Is(err, ErrInvalid) {
				t.Fatal("clock rollback did not match the runtime failure", err)
			}
			if err = f.store.PulseScan(ctx, owner, a.JobID, boot); err == nil {
				t.Fatal("invalid work renewed")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("monitor released hardware lease")
			}
			if mode != "reservation" && scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
				t.Fatal("monitor released reservation")
			}
		})
	}
}
func TestUnclaimedScanCannotLoadOrPulse(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	a, err := f.store.AdmitScan(ctx, alice, scanRequest(), scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.LoadScanWork(ctx, alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("unclaimed scan exposed", err)
	}
	if err = f.store.PulseScan(ctx, alice, a.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("unclaimed scan renewed", err)
	}
}
func TestScanCancellationStopsWatchedWorkAndRetainsLease(t *testing.T) {
	f, a, _ := startedCapture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := false
	err := workwatch.Run(ctx, time.Millisecond, func(ctx context.Context) error { return f.store.PulseScan(ctx, alice, a.JobID, runner) }, func(ctx context.Context) error {
		if err := f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
			return err
		}
		<-ctx.Done()
		stopped = true
		return nil
	})
	if !errors.Is(err, ErrConflict) || !stopped {
		t.Fatal("persisted cancellation not observed", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
		t.Fatal("local cancellation fabricated physical idle/cleanup")
	}
}

func TestScanWorkCommitUncertaintyReturnsNoAuthority(t *testing.T) {
	f, a, _ := startedCapture(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected uncertain acknowledgment")
	}
	work, err := f.store.LoadScanWork(context.Background(), alice, a.JobID, runner)
	if !errors.Is(err, ErrCommitUncertain) || work.Job.ID != "" {
		t.Fatal("uncertain read granted execution", err)
	}
	if err = f.store.PulseScan(context.Background(), alice, a.JobID, runner); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("uncertain heartbeat allowed work", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE job_id=? AND state='held'", a.JobID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1 {
		t.Fatal("uncertainty released resources")
	}
}
func TestScanWorkRetainsPhotoEncoding(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			f := scanFixture(t)
			request := scanRequest()
			request.Purpose = "photo"
			request.PhotoFormat = format
			a, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimScan(context.Background(), alice, a.JobID, runner); err != nil {
				t.Fatal(err)
			}
			work, err := f.store.LoadScanWork(context.Background(), alice, a.JobID, runner)
			if err != nil || work.Snapshot.PhotoFormat != format {
				t.Fatal("photo encoding lost", err)
			}
		})
	}
}
