//go:build linux

package workercatalog

import (
	"context"
	"testing"
)

func TestPublisherScannerObservationKeepsFreshReadinessAndNeverReleasesHeldLease(t *testing.T) {
	f, records, lease, proof, request, limits := readyFixture(t)
	p := &sqlitePublisher{store: f.store, lease: lease}
	ctx := context.Background()
	expires := readyCount(t, f.db, "SELECT expires_at_ms FROM scanner_readiness")
	epoch, err := p.BeginScannerObservation(ctx, proof.DeviceID)
	if err != nil || epoch != 1 {
		t.Fatal(epoch, err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness") != 1 {
		t.Fatal("healthy renewal withdrew unexpired readiness")
	}
	if readyCount(t, f.db, "SELECT expires_at_ms FROM scanner_readiness") != expires {
		t.Fatal("beginning an observation extended evidence lifetime")
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

func TestPublisherWithdrawsOnlySelectedDeviceReadiness(t *testing.T) {
	f, _, lease, proof, _, _ := readyFixture(t)
	p := &sqlitePublisher{store: f.store, lease: lease}
	other := proof
	other.DeviceID, other.PhysicalID, other.LeaseEpoch = "scanner_other_0001", "physical_other_0001", 0
	if err := p.PublishReadiness(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if err := p.WithdrawReadiness(context.Background(), proof.DeviceID); err != nil {
		t.Fatal(err)
	}
	if readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness WHERE device_id=?", proof.DeviceID) != 0 || readyCount(t, f.db, "SELECT count(*) FROM scanner_readiness WHERE device_id=?", other.DeviceID) != 1 {
		t.Fatal("withdrawal changed another device")
	}
	if readyCount(t, f.db, "SELECT count(*) FROM device_leases WHERE state='idle' AND job_id IS NULL") != 2 {
		t.Fatal("withdrawal changed device leases")
	}
}
