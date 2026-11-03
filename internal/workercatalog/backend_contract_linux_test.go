//go:build linux

package workercatalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/testsupport/catalogcontract"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestSQLitePublisherContract(t *testing.T) {
	catalogcontract.Run(t, func(t *testing.T) catalogcontract.Fixture {
		directory := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		db, err := storage.Open(filepath.Join(directory, "documents.db"), storage.Documents)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		readDB, err := storage.Open(filepath.Join(directory, "documents.db"), storage.Documents)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { readDB.Close() })
		now := int64(1000)
		clock := func() time.Time { return time.UnixMilli(now) }
		store, err := workercatalog.New(db, clock, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := workercatalog.NewReader(readDB, clock)
		if err != nil {
			t.Fatal(err)
		}
		acquire := func() *workeridentity.Guard {
			t.Helper()
			g, err := workeridentity.Acquire(directory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { g.Close() })
			return g
		}
		guard := acquire()
		begin := func(g *workeridentity.Guard) workercatalog.Publisher {
			t.Helper()
			p, err := store.BeginPublisher(context.Background(), g)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		return catalogcontract.Fixture{
			Publisher: begin(guard), OCR: reader, Scanners: reader,
			SetTime: func(ms int64) { now = ms },
			Takeover: func() workercatalog.Publisher {
				if err := guard.Close(); err != nil {
					t.Fatal(err)
				}
				return begin(acquire())
			},
			OCRSnapshot:     renderadmission.CatalogSnapshot{Providers: []ocrplugin.Capabilities{{APIVersion: 1, ID: "plugin", Name: "OCR", Version: "v1", Models: []ocrplugin.Model{{ID: "model", Name: "Model", Version: "pinned-v1", Languages: []string{"jpn", "eng"}, QualityPresets: []string{"standard"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 10, MaxPagePixels: 10000}}}}, Defaults: []renderadmission.ModelDefault{{ProviderID: "plugin", ModelID: "model", QualityPreset: "standard"}}},
			ScannerSnapshot: scannermetadata.Snapshot{Devices: []scannermetadata.Device{{ID: "scanner_contract_001", Profiles: []scanner.Profile{{Capture: scanner.Capture{Size: "a4", Color: "gray", DPI: 300}, Width: 80, Height: 100, CheckedAtMS: 1000}}}}},
		}
	})
}
