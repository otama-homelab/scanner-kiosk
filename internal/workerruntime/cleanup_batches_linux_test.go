//go:build linux

package workerruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

func TestStartupCleanupDrainsMoreThanOneMetadataBatch(t *testing.T) {
	c, _, db, _ := fixture(t)
	now := time.Now().UnixMilli()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 101; i++ {
		doc, job, device := fmt.Sprintf("document_batch_%04d", i), fmt.Sprintf("job_scan_batch_%04d", i), fmt.Sprintf("scanner_batch_%04d", i)
		snapshot, _ := json.Marshal(documents.ScanSnapshot{Version: 1, DeviceID: device, WorkerBootID: "worker_batch_previous_01", LeaseEpoch: 2, Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}})
		for _, query := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','scanning',?,?)`, []any{doc, owner, now - 10000, now - 1}},
			{`INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id) VALUES(?,?,?,'scan','queued',1,?,?,?,'worker_batch_previous_01')`, []any{job, doc, owner, string(snapshot), now - 10000, now - 500}},
			{`INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES(?,2,'held',?)`, []any{device, job}},
			{`INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1)`, []any{job, owner}},
		} {
			if _, err = tx.Exec(query.sql, query.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal("full batch treated as unknown native", err)
	}
	for query, wanted := range map[string]int{
		"SELECT count(*) FROM jobs WHERE kind='scan' AND state='cancelled' AND error_code='not_started' AND started_at_ms IS NULL": 100,
		"SELECT count(*) FROM jobs WHERE kind='cleanup'":                                                           101,
		"SELECT count(*) FROM jobs WHERE kind='cleanup' AND state='succeeded'":                                     1,
		"SELECT count(*) FROM jobs WHERE kind='scan' AND state IN ('queued','running','cancelling','reconciling')": 0,
		"SELECT count(*) FROM reservations":                                                                        0,
		"SELECT count(*) FROM device_leases WHERE job_id IS NOT NULL OR state!='idle'":                             0,
	} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != wanted {
			t.Fatal(query, count, err)
		}
	}
}
