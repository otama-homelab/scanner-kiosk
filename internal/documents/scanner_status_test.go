package documents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
	scannerport "scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
)

// Count application document-query calls using the actual SQLite driver. This
// wrapper changes no SQL or transaction behavior and is confined to this test.
type statusCountDriver struct {
	base  driver.Driver
	calls *atomic.Int64
}

func (d statusCountDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return statusCountConn{Conn: conn, calls: d.calls}, nil
}

type statusCountConn struct {
	driver.Conn
	calls *atomic.Int64
}

func (c statusCountConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}
func (c statusCountConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.calls.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

var statusDriverSequence atomic.Int64

func TestScannerStatusQueryCountDoesNotGrowWithDeviceCount(t *testing.T) {
	for _, count := range []int{1, 32} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			must(t, f.docs, "DELETE FROM jobs")
			must(t, f.docs, "INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,?,1,'active',1000,1000,11000)", runner)
			metadata := scannermetadata.Snapshot{PublishedAtMS: 1500, ExpiresAtMS: 11000, Devices: []scannermetadata.Device{}}
			ids := []string{}
			for i := range count {
				id := fmt.Sprintf("scanner_status_%04d", i)
				ids = append(ids, id)
				metadata.Devices = append(metadata.Devices, scannermetadata.Device{ID: id, Profiles: []scannerport.Profile{{Capture: scannerport.Capture{Size: "a4", Color: "color", DPI: 300}, Width: 80, Height: 100, CheckedAtMS: 1500}}})
				must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", id)
				must(t, f.docs, `INSERT INTO scanner_readiness(device_id,boot_id,generation,lease_epoch,physical_id,handoff_id,scan_concurrency,ownership_confirmed_at_ms,local_stopped_at_ms,idle_observed_at_ms,published_at_ms,expires_at_ms) VALUES(?,?,1,1,?,'handoff_status_0001',4,1000,1000,1500,1500,11000)`, id, runner, fmt.Sprintf("physical_status_%04d", i))
			}
			data, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			must(t, f.docs, "INSERT INTO scanner_metadata_snapshot(singleton,boot_id,generation,published_at_ms,expires_at_ms,payload) VALUES(1,?,1,1500,11000,?)", runner, string(data))
			var calls atomic.Int64
			name := fmt.Sprintf("scanner_status_count_%d", statusDriverSequence.Add(1))
			sql.Register(name, statusCountDriver{base: &sqlite.Driver{}, calls: &calls})
			db, err := sql.Open(name, f.docsPath+"?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			records, err := New(f.state, db, f.store.now, fixtureSpace)
			if err != nil {
				t.Fatal(err)
			}
			out, err := records.ReadScannerReadiness(context.Background(), alice, ids, 40000000)
			if err != nil || len(out.Devices) != count {
				t.Fatal(out, err)
			}
			for _, device := range out.Devices {
				if device.State != scannerreadiness.Idle {
					t.Fatal("valid device not idle", device)
				}
			}
			if calls.Load() != 3 {
				t.Fatal("scanner list query count changed with device count", count, calls.Load())
			}
		})
	}
}
