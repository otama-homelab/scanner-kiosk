//go:build linux

package documents

import (
	"context"
	"errors"
	"os"
	"testing"

	"scanner-kiosk/internal/workeridentity"
)

func TestFreshWorkerGuardCannotResumeOldQueuedPhysicalScan(t *testing.T) {
	f := scanFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	request := scanRequest()
	request.WorkerBootID = old.ID()
	admitted, err := f.store.AdmitScan(context.Background(), alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = f.store.ClaimScan(context.Background(), alice, admitted.JobID, next.ID()); !errors.Is(err, ErrConflict) {
		t.Fatal("new coordinator executed old queued scan")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM device_leases WHERE state='held' AND epoch=2") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("lock release was mistaken for physical termination")
	}
}
