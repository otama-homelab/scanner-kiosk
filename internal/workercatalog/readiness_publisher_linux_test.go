//go:build linux

package workercatalog

import (
	"context"
	"testing"
)

func TestPublisherScannerObservationWithdrawsAndNeverReleasesHeldLease(t *testing.T) {
	f, records, lease, proof, request, limits := readyFixture(t)
	p := &sqlitePublisher{store: f.store, lease: lease}
	ctx := context.Background()
	epoch, err := p.BeginScannerObservation(ctx, proof.DeviceID)
	if err != nil || epoch != 1 {
		t.Fatal(epoch, err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 0 {
		t.Fatal("old readiness retained")
	}
	proof.LeaseEpoch = epoch
	if err = p.PublishReadiness(ctx, proof); err != nil {
		t.Fatal(err)
	}
	admitted, err := records.AdmitReadyScan(ctx, readyOwner, request, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.BeginScannerObservation(ctx, proof.DeviceID); err == nil {
		t.Fatal("held lease observed as idle")
	}
	if readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='held' AND job_id=?", admitted.JobID) != 1 {
		t.Fatal("held lease changed")
	}
	if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 0 {
		t.Fatal("held readiness retained")
	}
	if err = p.PublishReadiness(ctx, proof); err == nil {
		t.Fatal("stale proof overwrote held lease")
	}
	if err = p.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = p.BeginScannerObservation(ctx, "scanner_unseen_0001"); err == nil {
		t.Fatal("stopped publisher accepted")
	}
}
