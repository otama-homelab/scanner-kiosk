//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

func TestCatalogInitialPublicationClockRegressionStopsAuthority(t *testing.T) {
	for _, backwards := range []bool{false, true} {
		name := "steady"
		if backwards {
			name = "backwards"
		}
		t.Run(name, func(t *testing.T) {
			_, _, db, root := fixture(t)
			guard, err := workeridentity.Acquire(root)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			var wall atomic.Int64
			wall.Store(1000)
			store, err := workercatalog.New(db, func() time.Time { return time.UnixMilli(wall.Load()) }, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			publisher, err := startCatalogPublisher(ctx, cancel, store, guard, catalogBuilderFunc(func(context.Context) (renderadmission.CatalogSnapshot, error) {
				if backwards {
					wall.Store(999)
				}
				return renderadmission.CatalogSnapshot{}, nil
			}), 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			defer func() {
				if !closed {
					_ = publisher.close()
				}
			}()
			if backwards {
				<-ctx.Done()
				if ctx.Err() != context.Canceled || !errors.Is(publisher.failure(), ErrCatalog) {
					t.Fatal("regressed publication did not cancel authority", ctx.Err(), publisher.failure())
				}
			} else {
				for {
					if _, err := store.ReadOCRCatalog(ctx); err == nil {
						break
					}
					if ctx.Err() != nil {
						t.Fatal("steady clock did not publish", ctx.Err(), publisher.failure())
					}
					time.Sleep(time.Millisecond)
				}
				if publisher.failure() != nil {
					t.Fatal("steady clock lost authority", publisher.failure())
				}
			}
			closeErr := publisher.close()
			closed = true
			if errors.Is(closeErr, ErrCatalog) != backwards {
				t.Fatal("publication failure lost during withdrawal", closeErr)
			}
			var state string
			var created, heartbeat, until int64
			var ocrRows, scannerRows int
			err = db.QueryRow(`SELECT state,created_at_ms,heartbeat_at_ms,valid_until_ms,
    (SELECT count(*) FROM ocr_catalog_snapshot), (SELECT count(*) FROM scanner_metadata_snapshot)
    FROM processing_coordinator WHERE singleton=1`).Scan(&state, &created, &heartbeat, &until, &ocrRows, &scannerRows)
			if err != nil {
				t.Fatal(err)
			}
			if state != "stopped" || created != 1000 || heartbeat != 1000 || until != 31000 || ocrRows != 0 || scannerRows != 0 {
				t.Fatalf("authority withdrawal changed immutable clock evidence: state=%s created=%d heartbeat=%d until=%d ocr=%d scanners=%d", state, created, heartbeat, until, ocrRows, scannerRows)
			}
			wall.Store(1001)
			if _, err := store.ReadOCRCatalog(context.Background()); !errors.Is(err, workercatalog.ErrUnavailable) {
				t.Fatal("clock recovery revived a stopped authority", err)
			}
		})
	}
}
