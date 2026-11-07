//go:build linux

package workerruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

func metadataPublisherFixture(t *testing.T) (*catalogPublisher, *sql.DB) {
	t.Helper()
	_, _, db, root := fixture(t)
	guard, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Close() })
	store, err := workercatalog.New(db, time.Now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := startCatalogPublisher(ctx, cancel, store, guard, catalogBuilderFunc(func(context.Context) (renderadmission.CatalogSnapshot, error) {
		return renderadmission.CatalogSnapshot{}, errors.New("optional OCR unavailable")
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.close() })
	return p, db
}
func TestSlowMetadataDeviceDoesNotDelayHealthyPublication(t *testing.T) {
	p, db := metadataPublisherFixture(t)
	reader, err := workercatalog.NewReader(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	slow, fast := "scanner_metadata_slow", "scanner_metadata_fast"
	entered := make(chan struct{}, 1)
	var fastCalls atomic.Int64
	p.startScannerMetadata(metadataInspectorFunc(func(ctx context.Context, id string, capture scanner.Capture) (scanner.Profile, error) {
		if id == slow {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return scanner.Profile{}, ctx.Err()
		}
		fastCalls.Add(1)
		if capture != (scanner.Capture{Size: "a4", Color: "color", DPI: 300}) {
			return scanner.Profile{}, scannermetadata.ErrUnavailable
		}
		return scanner.Profile{Capture: capture, Width: 2480, Height: 3508, CheckedAtMS: time.Now().UnixMilli()}, nil
	}), []string{slow, fast}, time.Second)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("slow device did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		value, err := reader.ReadScannerMetadata(context.Background())
		if err == nil && len(value.Devices) == 1 && value.Devices[0].ID == fast {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("healthy device waited for blocked inspection", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for fastCalls.Load() < 60 {
		if time.Now().After(deadline) {
			t.Fatal("remaining settings did not complete", fastCalls.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A heartbeat publication keeps the completed native result without probing
	// it again, while the slow device is still blocked in its first operation.
	time.Sleep(400 * time.Millisecond)
	value, err := reader.ReadScannerMetadata(context.Background())
	if err != nil || len(value.Devices) != 1 || value.Devices[0].ID != fast || fastCalls.Load() != 60 {
		t.Fatal("healthy metadata not retained or re-probed", value, err, fastCalls.Load())
	}
	var leases int
	if err = db.QueryRow("SELECT count(*) FROM device_leases").Scan(&leases); err != nil || leases != 0 {
		t.Fatal("metadata created readiness", err)
	}
	if err := p.close(); err != nil {
		t.Fatal("metadata shutdown failed", err)
	}
	if err = db.QueryRow("SELECT count(*) FROM scanner_metadata_snapshot").Scan(&leases); err != nil || leases != 0 {
		t.Fatal("shutdown did not withdraw metadata", err)
	}
}
func TestMetadataPoolBoundsNativeInspectionsAndJoinsLateFailure(t *testing.T) {
	p, _ := metadataPublisherFixture(t)
	entered := make(chan string, 8)
	var active, maximum atomic.Int64
	ids := []string{}
	for i := range 8 {
		ids = append(ids, fmt.Sprintf("scanner_metadata_%03d", i))
	}
	p.startScannerMetadata(metadataInspectorFunc(func(ctx context.Context, id string, _ scanner.Capture) (scanner.Profile, error) {
		value := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); value > old && !maximum.CompareAndSwap(old, value); old = maximum.Load() {
		}
		entered <- id
		<-ctx.Done()
		return scanner.Profile{}, scanner.ErrUnresolved
	}), ids, time.Second)
	for range 4 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("bounded workers did not enter")
		}
	}
	select {
	case <-entered:
		t.Fatal("metadata pool exceeded four operations")
	case <-time.After(30 * time.Millisecond):
	}
	err := p.close()
	if !errors.Is(err, ErrReconciliation) || !errors.Is(err, scanner.ErrUnresolved) || active.Load() != 0 || maximum.Load() != 4 {
		t.Fatal("late failure hidden or unjoined native metadata", err, active.Load(), maximum.Load())
	}
	select {
	case <-entered:
		t.Fatal("queued metadata operation started after shutdown")
	default:
	}
}
func TestMetadataSnapshotPrunesAgeingDeviceWithoutExpiringHealthyDevice(t *testing.T) {
	now := time.Now()
	horizon := 12 * time.Second
	stale := scannermetadata.Device{ID: "scanner_metadata_old", Profiles: []scanner.Profile{{CheckedAtMS: now.Add(-scannermetadata.MaxAge + horizon).UnixMilli()}}}
	healthy := scannermetadata.Device{ID: "scanner_metadata_new", Profiles: []scanner.Profile{{CheckedAtMS: now.UnixMilli()}}}
	cached := map[string]scannermetadata.Device{stale.ID: stale, healthy.ID: healthy}
	out := scannerMetadataSnapshot([]string{stale.ID, healthy.ID}, cached, now, horizon)
	if len(out.Devices) != 1 || out.Devices[0].ID != healthy.ID || len(cached) != 1 || out.Devices[0].Profiles[0].CheckedAtMS != healthy.Profiles[0].CheckedAtMS {
		t.Fatal("ageing device starved or refreshed healthy observations", out)
	}
}

func TestVerifiedSettingPublishesBeforeLaterSettingFinishes(t *testing.T) {
	p, db := metadataPublisherFixture(t)
	reader, err := workercatalog.NewReader(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	later := make(chan struct{})
	var calls atomic.Int64
	p.startScannerMetadata(metadataInspectorFunc(func(ctx context.Context, _ string, capture scanner.Capture) (scanner.Profile, error) {
		if calls.Add(1) == 1 {
			return scanner.Profile{Capture: capture, Width: 2480, Height: 3508, CheckedAtMS: time.Now().UnixMilli()}, nil
		}
		close(later)
		<-ctx.Done()
		return scanner.Profile{}, ctx.Err()
	}), []string{"scanner_partial_verified_01"}, time.Second)
	select {
	case <-later:
	case <-time.After(3 * time.Second):
		t.Fatal("later setting did not start")
	}
	value, err := reader.ReadScannerMetadata(context.Background())
	if err != nil || len(value.Devices) != 1 || len(value.Devices[0].Profiles) != 1 {
		t.Fatal("verified first setting waited for unrelated later inspection", value, err)
	}
	if value.Devices[0].Profiles[0].Capture != (scanner.Capture{Size: "a4", Color: "color", DPI: 300}) {
		t.Fatal("unverified setting advertised")
	}
	var readiness int
	if err = db.QueryRow("SELECT count(*) FROM scanner_readiness").Scan(&readiness); err != nil || readiness != 0 {
		t.Fatal("metadata fabricated idle authority", readiness, err)
	}
}
