//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/config"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

type metadataInspector struct {
	calls      int
	unresolved bool
}

type metadataInspectorFunc func(context.Context, string, scanner.Capture) (scanner.Profile, error)

func (f metadataInspectorFunc) InspectCapture(ctx context.Context, id string, capture scanner.Capture) (scanner.Profile, error) {
	return f(ctx, id, capture)
}

func TestMetadataShutdownRetainsInspectionFailureDiscoveredWhileJoining(t *testing.T) {
	_, _, db, root := fixture(t)
	guard, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	store, err := workercatalog.New(db, time.Now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher, err := startCatalogPublisher(ctx, cancel, store, guard, catalogBuilderFunc(func(context.Context) (renderadmission.CatalogSnapshot, error) {
		return renderadmission.CatalogSnapshot{}, errors.New("optional OCR unavailable")
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	publisher.startScannerMetadata(metadataInspectorFunc(func(ctx context.Context, _ string, _ scanner.Capture) (scanner.Profile, error) {
		close(entered)
		<-ctx.Done()
		return scanner.Profile{}, scanner.ErrUnresolved
	}), []string{"scanner_metadata_001"}, time.Second)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("inspection did not start")
	}
	if err := publisher.close(); !errors.Is(err, ErrReconciliation) || !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("join hid unresolved native state", err)
	}
}

func (i *metadataInspector) InspectCapture(_ context.Context, _ string, capture scanner.Capture) (scanner.Profile, error) {
	i.calls++
	if i.unresolved {
		return scanner.Profile{}, scanner.ErrUnresolved
	}
	if capture != (scanner.Capture{Size: "a4", Color: "color", DPI: 300}) {
		return scanner.Profile{}, scannermetadata.ErrUnavailable
	}
	return scanner.Profile{Capture: capture, Width: 2480, Height: 3508, CheckedAtMS: time.Now().UnixMilli()}, nil
}

func TestScannerMetadataBuilderEnumeratesOnlyVerifiedTuplesAndCaches(t *testing.T) {
	inspector := &metadataInspector{}
	builder := scannerMetadataBuilder{inspector: inspector, ids: []string{"scanner_metadata_001"}}
	value, err := completeMetadataCycle(&builder)
	if err != nil || inspector.calls != 60 || len(value.Devices) != 1 || len(value.Devices[0].Profiles) != 1 {
		t.Fatal(value, inspector.calls, err)
	}
	if _, err := builder.catalog(context.Background()); err != nil || inspector.calls != 60 {
		t.Fatal("lease refresh repeated native probes", inspector.calls, err)
	}
	builder.checked = time.Now().Add(-2 * time.Minute)
	if _, err := completeMetadataCycle(&builder); err != nil || inspector.calls != 120 {
		t.Fatal("expired inspection cache was not refreshed", inspector.calls, err)
	}
}

func TestUnresolvedMetadataInspectionStopsPublicationAndKeepsNoPartialSnapshot(t *testing.T) {
	_, _, db, root := fixture(t)
	guard, err := workeridentity.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	store, err := workercatalog.New(db, time.Now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher, err := startCatalogPublisher(ctx, cancel, store, guard, catalogBuilderFunc(func(context.Context) (renderadmission.CatalogSnapshot, error) {
		return renderadmission.CatalogSnapshot{}, errors.New("optional OCR unavailable")
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.close()
	publisher.startScannerMetadata(&metadataInspector{unresolved: true}, []string{"scanner_metadata_001"}, time.Second)
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("unresolved native state did not stop worker")
	}
	if err := publisher.failure(); !errors.Is(err, ErrReconciliation) || !errors.Is(err, scanner.ErrUnresolved) {
		t.Fatal("unresolved state lost", err)
	}
	var snapshots int
	if err := db.QueryRow("SELECT count(*) FROM scanner_metadata_snapshot").Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatal("partial unresolved metadata was published", err)
	}
}

func TestScannerMetadataCompletesSlowVerifiedSettingsWithinFreshness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		builder := scannerMetadataBuilder{ids: []string{"scanner_slow_native_001"}, inspector: metadataInspectorFunc(func(ctx context.Context, _ string, capture scanner.Capture) (scanner.Profile, error) {
			calls++
			time.Sleep(1500 * time.Millisecond)
			return scanner.Profile{Capture: capture, Width: 2480, Height: 3508, CheckedAtMS: time.Now().UnixMilli()}, nil
		})}
		value, err := completeMetadataCycle(&builder)
		if err != nil || calls != 60 || len(value.Devices) != 1 || len(value.Devices[0].Profiles) != 60 {
			t.Fatal("verified settings discarded before bounded inspection completed", calls, err)
		}
		oldest := value.Devices[0].Profiles[0].CheckedAtMS
		if time.Now().UnixMilli()-oldest >= scannermetadata.MaxAge.Milliseconds() {
			t.Fatal("published stale settings")
		}
	})
}

func completeMetadataCycle(builder *scannerMetadataBuilder) (scannermetadata.Snapshot, error) {
	for {
		value, err := builder.catalog(context.Background())
		if err != nil || builder.cursor == 0 {
			return value, err
		}
	}
}

type dynamicMetadataInspector struct {
	mu      sync.Mutex
	devices []config.Scanner
}

func (d *dynamicMetadataInspector) IDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := []string{}
	for _, device := range d.devices {
		ids = append(ids, device.ID)
	}
	return ids
}
func (d *dynamicMetadataInspector) Devices() []config.Scanner {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]config.Scanner(nil), d.devices...)
}
func (*dynamicMetadataInspector) InspectCapture(ctx context.Context, id string, c scanner.Capture) (scanner.Profile, error) {
	return scanner.Profile{Capture: c, Width: 80, Height: 100, CheckedAtMS: time.Now().UnixMilli()}, nil
}
func TestMetadataPublishesDevicesAddedAfterEmptyStartup(t *testing.T) {
	p, db := metadataPublisherFixture(t)
	defer p.close()
	inspector := &dynamicMetadataInspector{}
	p.startScannerMetadata(inspector, nil, time.Second)
	inspector.mu.Lock()
	inspector.devices = []config.Scanner{{ID: "scanner_added_later_001", Name: "Later scanner"}}
	inspector.mu.Unlock()
	reader, err := workercatalog.NewReader(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := reader.ReadScannerMetadata(context.Background())
		if err == nil && len(snapshot.Devices) == 1 && snapshot.Devices[0].Name == "Later scanner" && len(snapshot.Devices[0].Profiles) == 60 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hotplug metadata missing", snapshot, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
