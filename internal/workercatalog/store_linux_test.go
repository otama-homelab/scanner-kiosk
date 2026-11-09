//go:build linux

package workercatalog

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/workeridentity"
	"scanner-kiosk/pkg/ocrplugin"
	"testing"
	"time"
)

type fixture struct {
	store     *Store
	db        *sql.DB
	guard     *workeridentity.Guard
	root      string
	statePath string
	now       int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{root: filepath.Join(t.TempDir(), "private"), now: 1000}
	if err := os.Mkdir(f.root, 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	f.db, err = storage.Open(filepath.Join(f.root, "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.Close() })
	f.guard, err = workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.guard.Close() })
	f.store, err = New(f.db, func() time.Time { return time.UnixMilli(f.now) }, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func snapshot() renderadmission.CatalogSnapshot {
	return renderadmission.CatalogSnapshot{Providers: []ocrplugin.Capabilities{{APIVersion: 1, ID: "plugin", Name: "OCR", Version: "v1", Models: []ocrplugin.Model{{ID: "model", Name: "Model", Version: "pinned-v1", Languages: []string{"jpn", "eng"}, QualityPresets: []string{"standard"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 10, MaxPagePixels: 10000}}}}, Defaults: []renderadmission.ModelDefault{{ProviderID: "plugin", ModelID: "model", QualityPreset: "standard"}}}
}
func TestCatalogPublicationSourceExpiryAndStop(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Publish(ctx, lease, snapshot()); err != nil {
		t.Fatal(err)
	}
	source, err := f.store.ReadOCRCatalog(ctx)
	if err != nil || source.PublishedAtMS != 1000 || source.ExpiresAtMS != 11000 {
		t.Fatal(source, err)
	}
	resolver := renderadmission.CatalogResolver{Source: f.store, Now: func() time.Time { return time.UnixMilli(f.now) }}
	resolved, err := resolver.ResolveOCR(ctx, documents.RenderOCR{Enabled: true, ProviderID: "plugin", ModelID: "model", Languages: []string{"jpn", "eng"}}, false)
	if err != nil || resolved.ModelVersion != "pinned-v1" {
		t.Fatal("persistent source did not resolve", resolved, err)
	}
	source.Providers[0].Models[0].Languages[0] = "mutated"
	again, err := f.store.ReadOCRCatalog(ctx)
	if err != nil || again.Providers[0].Models[0].Languages[0] != "jpn" {
		t.Fatal("stored alias", err)
	}
	f.now = 5000
	if err = f.store.Pulse(ctx, lease); err != nil {
		t.Fatal(err)
	}
	f.now = 11000
	if _, err = f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("heartbeat revived expired snapshot", err)
	}
	if err = f.store.Publish(ctx, lease, snapshot()); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Stop(ctx, lease); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stopped metadata readable", err)
	}
	if err = f.store.Pulse(ctx, lease); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stopped authority revived", err)
	}
}
func TestNewGuardGenerationInvalidatesOldCatalog(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Publish(ctx, old, snapshot()); err != nil {
		t.Fatal(err)
	}
	if err = f.guard.Close(); err != nil {
		t.Fatal(err)
	}
	successor, err := workeridentity.Acquire(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	next, err := f.store.Begin(ctx, successor)
	if err != nil {
		t.Fatal(err)
	}
	if next.generation != old.generation+1 || next.boot == old.boot {
		t.Fatal("generation not advanced")
	}
	if _, err = f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old catalog survived takeover", err)
	}
	if err = f.store.Publish(ctx, old, snapshot()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed guard published", err)
	}
	if err = f.store.Publish(ctx, next, snapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Begin(ctx, successor); !errors.Is(err, ErrUnavailable) {
		t.Fatal("same boot re-registered", err)
	}
}
func TestExpiredAuthorityCannotBeRevived(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	f.now = 11000
	if err = f.store.Pulse(ctx, lease); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired heartbeat revived", err)
	}
	if err = f.store.Publish(ctx, lease, snapshot()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired authority published", err)
	}
}
func TestGenerationCASRejectsStillOpenOldGuard(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Publish(ctx, lease, snapshot()); err != nil {
		t.Fatal(err)
	}
	// Simulate the authoritative takeover record independently of flock closure.
	if _, err = f.db.Exec("UPDATE processing_coordinator SET boot_id='worker_successor_001',generation=generation+1"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{func() error { return f.store.Pulse(ctx, lease) }, func() error { return f.store.Publish(ctx, lease, snapshot()) }, func() error { return f.store.Stop(ctx, lease) }} {
		if err := operation(); !errors.Is(err, ErrUnavailable) {
			t.Fatal("stale generation accepted", err)
		}
	}
	if _, err = f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("generation-mismatched snapshot read", err)
	}
}
func TestInvalidPayloadAndCallerTimestampsAreRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lease, err := f.store.Begin(ctx, f.guard)
	if err != nil {
		t.Fatal(err)
	}
	data := snapshot()
	data.PublishedAtMS = 1000
	if err = f.store.Publish(ctx, lease, data); !errors.Is(err, ErrUnavailable) {
		t.Fatal("caller time accepted", err)
	}
	data = snapshot()
	data.Providers[0].Models[0].Languages = []string{"eng", "eng"}
	if err = f.store.Publish(ctx, lease, data); !errors.Is(err, ErrUnavailable) {
		t.Fatal("malformed metadata published", err)
	}
	if err = f.store.Publish(ctx, lease, snapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE ocr_catalog_snapshot SET payload='{}'`); err != nil {
		t.Fatal(err)
	}
	if out, err := f.store.ReadOCRCatalog(ctx); !errors.Is(err, ErrUnavailable) || out.Providers != nil {
		t.Fatal("malformed data escaped", out, err)
	}
}
