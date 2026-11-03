//go:build linux

// Package catalogcontract checks backend-owned metadata publication authority.
package catalogcontract

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/workercatalog"
)

// Fixture starts at 1000ms with a 10-second authority and no publications.
// It controls the actual backend clock and guard. Takeover closes the old
// guard and registers a fresh authority against the same storage.
type Fixture struct {
	Publisher       workercatalog.Publisher
	OCR             renderadmission.CatalogSource
	Scanners        scannermetadata.Source
	SetTime         func(int64)
	Takeover        func() workercatalog.Publisher
	OCRSnapshot     renderadmission.CatalogSnapshot
	ScannerSnapshot scannermetadata.Snapshot
}

// Run checks metadata authority only, not native stop, device readiness, job
// claims, uncertain commits, durability or safe physical takeover.
func Run(t *testing.T, newFixture func(*testing.T) Fixture) {
	t.Helper()
	ctx := context.Background()
	publish := func(t *testing.T, f Fixture, p workercatalog.Publisher) {
		t.Helper()
		if err := p.Publish(ctx, f.OCRSnapshot); err != nil {
			t.Fatal(err)
		}
		if err := p.PublishScanners(ctx, f.ScannerSnapshot); err != nil {
			t.Fatal(err)
		}
	}
	unavailable := func(t *testing.T, f Fixture) {
		t.Helper()
		if _, err := f.OCR.ReadOCRCatalog(ctx); !errors.Is(err, workercatalog.ErrUnavailable) {
			t.Fatal("OCR readable", err)
		}
		if _, err := f.Scanners.ReadScannerMetadata(ctx); !errors.Is(err, workercatalog.ErrUnavailable) {
			t.Fatal("scanner metadata readable", err)
		}
	}
	rejected := func(t *testing.T, f Fixture, p workercatalog.Publisher) {
		t.Helper()
		for name, err := range map[string]error{"pulse": p.Pulse(ctx), "OCR": p.Publish(ctx, f.OCRSnapshot), "scanner": p.PublishScanners(ctx, f.ScannerSnapshot)} {
			if !errors.Is(err, workercatalog.ErrUnavailable) {
				t.Fatal("revoked authority", name, err)
			}
		}
	}
	t.Run("publication detached reads and independent expiry", func(t *testing.T) {
		f := newFixture(t)
		unavailable(t, f)
		publish(t, f, f.Publisher)
		o, err := f.OCR.ReadOCRCatalog(ctx)
		if err != nil {
			t.Fatal(err)
		}
		s, err := f.Scanners.ReadScannerMetadata(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if o.PublishedAtMS != 1000 || o.ExpiresAtMS != 11000 || s.PublishedAtMS != 1000 || s.ExpiresAtMS != 11000 {
			t.Fatal("authority timestamps", o, s)
		}
		o.Providers[0].Name = "mutated"
		s.Devices[0].Profiles[0].Width = 1
		again, err := f.OCR.ReadOCRCatalog(ctx)
		if err != nil || again.Providers[0].Name == "mutated" {
			t.Fatal("aliased OCR", err)
		}
		scanners, err := f.Scanners.ReadScannerMetadata(ctx)
		if err != nil || scanners.Devices[0].Profiles[0].Width == 1 {
			t.Fatal("aliased scanners", err)
		}
		f.SetTime(5000)
		if err := f.Publisher.Pulse(ctx); err != nil {
			t.Fatal(err)
		}
		f.SetTime(11000)
		unavailable(t, f)
		f.ScannerSnapshot.Devices[0].Profiles[0].CheckedAtMS = 11000
		publish(t, f, f.Publisher)
		if _, err := f.OCR.ReadOCRCatalog(ctx); err != nil {
			t.Fatal("fresh OCR", err)
		}
		if _, err := f.Scanners.ReadScannerMetadata(ctx); err != nil {
			t.Fatal("fresh scanner metadata", err)
		}
	})
	for _, mode := range []string{"expired", "stopped", "successor"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			publish(t, f, f.Publisher)
			var successor workercatalog.Publisher
			switch mode {
			case "expired":
				f.SetTime(11000)
			case "stopped":
				if err := f.Publisher.Stop(ctx); err != nil {
					t.Fatal(err)
				}
			case "successor":
				successor = f.Takeover()
			}
			unavailable(t, f)
			rejected(t, f, f.Publisher)
			if successor != nil {
				publish(t, f, successor)
				before, err := f.OCR.ReadOCRCatalog(ctx)
				if err != nil {
					t.Fatal(err)
				}
				scannerBefore, err := f.Scanners.ReadScannerMetadata(ctx)
				if err != nil {
					t.Fatal(err)
				}
				rejected(t, f, f.Publisher)
				if err := f.Publisher.Stop(ctx); !errors.Is(err, workercatalog.ErrUnavailable) {
					t.Fatal("old stop accepted", err)
				}
				after, err := f.OCR.ReadOCRCatalog(ctx)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("old stop changed successor OCR", err)
				}
				scannerAfter, err := f.Scanners.ReadScannerMetadata(ctx)
				if err != nil || !reflect.DeepEqual(scannerBefore, scannerAfter) {
					t.Fatal("old stop changed successor scanners", err)
				}
			}
		})
	}
}
