//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"testing"
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
	value, err := builder.catalog(context.Background())
	if err != nil || inspector.calls != 60 || len(value.Devices) != 1 || len(value.Devices[0].Profiles) != 1 {
		t.Fatal(value, inspector.calls, err)
	}
	if _, err := builder.catalog(context.Background()); err != nil || inspector.calls != 60 {
		t.Fatal("lease refresh repeated native probes", inspector.calls, err)
	}
	builder.checked = time.Now().Add(-2 * time.Minute)
	if _, err := builder.catalog(context.Background()); err != nil || inspector.calls != 120 {
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
