//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"testing"
	"time"
)

type catalogBuilderFunc func(context.Context) (renderadmission.CatalogSnapshot, error)

func (f catalogBuilderFunc) catalog(ctx context.Context) (renderadmission.CatalogSnapshot, error) {
	return f(ctx)
}
func TestCatalogHeartbeatContinuesDuringProviderInitialization(t *testing.T) {
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
	entered := make(chan struct{})
	publisher, err := startCatalogPublisher(ctx, cancel, store, guard, catalogBuilderFunc(func(ctx context.Context) (renderadmission.CatalogSnapshot, error) {
		close(entered)
		<-ctx.Done()
		return renderadmission.CatalogSnapshot{}, ctx.Err()
	}), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("catalog initialization did not start")
	}
	var initial int64
	if err := db.QueryRow("SELECT heartbeat_at_ms FROM processing_coordinator").Scan(&initial); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var heartbeat int64
		if err := db.QueryRow("SELECT heartbeat_at_ms FROM processing_coordinator").Scan(&heartbeat); err != nil {
			t.Fatal(err)
		}
		if heartbeat > initial {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initialization blocked heartbeat")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if publisher.failure() != nil || ctx.Err() != nil {
		t.Fatal("optional initialization cancelled worker")
	}
	if err := publisher.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadOCRCatalog(context.Background()); !errors.Is(err, workercatalog.ErrUnavailable) {
		t.Fatal("closed catalog readable", err)
	}
}
func TestCatalogPublicationStopsOnAuthorityLoss(t *testing.T) {
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
		return renderadmission.CatalogSnapshot{}, errors.New("optional provider unavailable")
	}), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE processing_coordinator SET state='stopped'"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("authority loss did not stop worker")
	}
	if publisher.failure() != ErrCatalog {
		t.Fatal("missing authority failure")
	}
	_ = publisher.close()
}

func TestRuntimePublishesAndWithdrawsCatalog(t *testing.T) {
	c, _, db, _ := fixture(t)
	t.Setenv("SCANNER_OCR_PLUGINS", "")
	t.Setenv("SCANNER_OCR_DEFAULT_PLUGIN", "")
	reader, err := workercatalog.NewReader(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, false) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Errorf("runtime shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("runtime did not stop")
		}
		if _, err := reader.ReadOCRCatalog(context.Background()); !errors.Is(err, workercatalog.ErrUnavailable) {
			t.Error("stopped runtime retained catalog", err)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := reader.ReadOCRCatalog(ctx)
		if err == nil {
			if len(snapshot.Providers) != 0 || len(snapshot.Defaults) != 0 {
				t.Fatal("disabled OCR advertised models")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime did not publish", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
