//go:build linux

package workerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

func TestStartupWithdrawnUnstartedScanUnblocksCleanup(t *testing.T) {
	for _, started := range []bool{false, true} {
		name := "unstarted"
		if started {
			name = "started"
		}
		t.Run(name, func(t *testing.T) {
			c, store, db, root := fixture(t)
			ctx := context.Background()
			now := time.Now().UnixMilli()
			const oldBoot = "worker_previous_scan_01"
			const job = "job_previous_scan_01"
			snapshot, err := json.Marshal(documents.ScanSnapshot{Version: 1, DeviceID: c.Scanners[0].ID, WorkerBootID: oldBoot, LeaseEpoch: 2, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}})
			if err != nil {
				t.Fatal(err)
			}
			execute(t, db, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id) VALUES(?,?,?,'scan','queued',1,?,?,?,?)`, job, document, owner, string(snapshot), now, now+60000, oldBoot)
			execute(t, db, `INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1024)`, job, owner)
			execute(t, db, `INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES(?,2,'held',?)`, c.Scanners[0].ID, job)
			if started {
				execute(t, db, "UPDATE jobs SET state='running',started_at_ms=?,heartbeat_at_ms=? WHERE id=?", now, now, job)
			}
			cleanup, err := store.RequestDelete(ctx, owner, documents.DeleteRequest{DocumentID: document, Revision: 1, IdempotencyKey: "request_withdraw_scan_01"}, 72*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			err = Run(ctx, c, true)
			if started {
				if !errors.Is(err, ErrReconciliation) {
					t.Fatal("started native job cleared", err)
				}
				if _, err = os.Stat(filepath.Join(root, "original.png")); err != nil {
					t.Fatal("unresolved content removed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadJob(ctx, owner, cleanup.ID)
			if err != nil || status.State != "succeeded" {
				t.Fatal(status, err)
			}
			if _, err = os.Stat(filepath.Join(root, "original.png")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cleanup did not remove private original", err)
			}
			if err = Run(ctx, c, true); err != nil {
				t.Fatal("fresh boot repeated capture", err)
			}
		})
	}
}
