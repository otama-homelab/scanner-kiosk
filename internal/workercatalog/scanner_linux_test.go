//go:build linux

package workercatalog

import (
	"context"
	"errors"
	"testing"

	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workeridentity"
)

func scannerSnapshot(checked int64) scannermetadata.Snapshot {
	return scannermetadata.Snapshot{Devices: []scannermetadata.Device{{ID: "scanner_metadata_001", Profiles: []scanner.Profile{{Capture: scanner.Capture{Size: "a4", Color: "color", DPI: 300}, Width: 2480, Height: 3508, CheckedAtMS: checked}}}}}
}

func TestScannerMetadataAuthorityExpiryAndNoReadiness(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishScanners(ctx, lease, scannerSnapshot(1000)); err != nil {
		t.Fatal(err)
	}
	value, err := f.store.ReadScannerMetadata(ctx)
	if err != nil || value.ExpiresAtMS != 11000 || value.Devices[0].Profiles[0].Width != 2480 {
		t.Fatal(value, err)
	}
	var leases int
	if err := f.db.QueryRow("SELECT count(*) FROM device_leases").Scan(&leases); err != nil || leases != 0 {
		t.Fatal("metadata initialized readiness", err)
	}
	f.now = 5000
	if err := f.store.Pulse(ctx, lease); err != nil {
		t.Fatal(err)
	}
	f.now = 11000
	if _, err := f.store.ReadScannerMetadata(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("heartbeat renewed stale snapshot", err)
	}
	if err := f.store.PublishScanners(ctx, lease, scannerSnapshot(11000)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Stop(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ReadScannerMetadata(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stopped metadata survived", err)
	}
}

func TestScannerMetadataRejectsInvalidProfilesAndForeignGeneration(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"before-boot", "future", "duplicate", "unsupported", "zero-dimension", "supplied-times"} {
		t.Run(mode, func(t *testing.T) {
			value := scannerSnapshot(1000)
			switch mode {
			case "before-boot":
				value.Devices[0].Profiles[0].CheckedAtMS = 999
			case "future":
				value.Devices[0].Profiles[0].CheckedAtMS = 1001
			case "duplicate":
				value.Devices[0].Profiles = append(value.Devices[0].Profiles, value.Devices[0].Profiles[0])
			case "unsupported":
				value.Devices[0].Profiles[0].DPI = 1200
			case "zero-dimension":
				value.Devices[0].Profiles[0].Width = 0
			case "supplied-times":
				value.PublishedAtMS = 1000
			}
			if err := f.store.PublishScanners(ctx, old, value); !errors.Is(err, ErrUnavailable) {
				t.Fatal("invalid metadata published", mode, err)
			}
		})
	}
	if err := f.store.PublishScanners(ctx, old, scannerSnapshot(1000)); err != nil {
		t.Fatal(err)
	}
	f.guard.Close()
	guard, err := workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	f.now = 2000
	next, err := f.store.Begin(ctx, guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ReadScannerMetadata(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old generation metadata survived", err)
	}
	if err := f.store.PublishScanners(ctx, old, scannerSnapshot(2000)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old writer published", err)
	}
	if err := f.store.PublishScanners(ctx, next, scannerSnapshot(1000)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("new boot reused old inspection", err)
	}
	if err := f.store.PublishScanners(ctx, next, scannerSnapshot(2000)); err != nil {
		t.Fatal(err)
	}
}

func TestScannerMetadataObservationAgeIsBounded(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	for f.now < 1000+scannermetadata.MaxAge.Milliseconds() {
		f.now += 9000
		if err := f.store.Pulse(ctx, lease); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.PublishScanners(ctx, lease, scannerSnapshot(1000)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old inspection received new lease TTL", err)
	}
}
