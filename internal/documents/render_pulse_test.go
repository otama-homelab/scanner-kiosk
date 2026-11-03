package documents

import (
	"context"
	"errors"
	"scanner-kiosk/internal/workwatch"
	"testing"
	"time"
)

func TestRenderPulsePreservesImmutableStart(t *testing.T) {
	f := renderFixture(t)
	ctx := context.Background()
	admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
		t.Fatal(err)
	}
	f.now = 2001
	if err = f.store.PulseRender(ctx, alice, admission.JobID, runner); err != nil {
		t.Fatal(err)
	}
	var start, heartbeat int64
	if err = f.docs.QueryRow("SELECT started_at_ms,heartbeat_at_ms FROM jobs WHERE id=?", admission.JobID).Scan(&start, &heartbeat); err != nil || start != 2000 || heartbeat != 2001 {
		t.Fatal("pulse changed start or missed heartbeat")
	}
}
func TestRenderPulseRejectsLostAuthority(t *testing.T) {
	for _, mode := range []string{"unstarted", "cancelled", "disabled", "expired", "revision", "foreign-owner", "foreign-runner", "deleting"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			ctx := context.Background()
			admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "unstarted" {
				if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
					t.Fatal(err)
				}
			}
			owner, worker := alice, runner
			switch mode {
			case "cancelled":
				if err = f.store.RequestCancel(ctx, alice, admission.JobID); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "expired":
				f.now = 10000
			case "revision":
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", docA)
			case "foreign-owner":
				owner = bob
			case "foreign-runner":
				worker = "worker_foreign_001"
			case "deleting":
				must(t, f.docs, "UPDATE documents SET state='deleting' WHERE id=?", docA)
			}
			if err = f.store.PulseRender(ctx, owner, admission.JobID, worker); err == nil {
				t.Fatal("lost authority renewed")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admission.JobID) != 1 {
				t.Fatal("heartbeat released resources without stop")
			}
		})
	}
}

func TestPersistedCancellationStopsWatchedWork(t *testing.T) {
	f := renderFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
		t.Fatal(err)
	}
	stopped := false
	err = workwatch.Run(ctx, time.Millisecond, func(ctx context.Context) error { return f.store.PulseRender(ctx, alice, admission.JobID, runner) }, func(ctx context.Context) error {
		if err := f.store.RequestCancel(ctx, alice, admission.JobID); err != nil {
			return err
		}
		<-ctx.Done()
		stopped = true
		return nil
	})
	if !errors.Is(err, ErrConflict) || !stopped {
		t.Fatalf("persisted cancellation not observed: %v", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admission.JobID) != 1 {
		t.Fatal("monitor fabricated stop/cleanup evidence")
	}
}
