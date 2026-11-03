//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartupWithdrawsPredecessorBeforeFailedRecovery(t *testing.T) {
	c, store, db, _ := fixture(t)
	job := admit(t, store)
	if _, err := store.ClaimExport(context.Background(), owner, job, "worker_previous_001"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	execute(t, db, `INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,'worker_previous_001',7,'active',?,?,?)`, now-1000, now, now+30000)
	if err := Run(context.Background(), c, true); !errors.Is(err, ErrReconciliation) {
		t.Fatal("old work resumed", err)
	}
	var state, boot, runner string
	var generation int64
	if err := db.QueryRow("SELECT state,boot_id,generation FROM processing_coordinator WHERE singleton=1").Scan(&state, &boot, &generation); err != nil {
		t.Fatal(err)
	}
	if state != "stopped" || boot != "worker_previous_001" || generation != 7 {
		t.Fatal("failed startup published authority or left predecessor active", state, boot, generation)
	}
	if err := db.QueryRow("SELECT runner_id FROM jobs WHERE id=?", job).Scan(&runner); err != nil || runner != "worker_previous_001" {
		t.Fatal("prior runner changed", runner, err)
	}
	var reserved int
	if err := db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&reserved); err != nil || reserved != 1 {
		t.Fatal("prior reservation released", reserved, err)
	}
}
func TestStartupWithdrawalFailurePreventsMaintenanceAndDispatch(t *testing.T) {
	c, store, db, _ := fixture(t)
	job := admit(t, store)
	now := time.Now().UnixMilli()
	execute(t, db, `INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,'worker_previous_001',7,'active',?,?,?)`, now-1000, now, now+30000)
	execute(t, db, `CREATE TRIGGER fail_startup_withdraw BEFORE UPDATE ON processing_coordinator BEGIN SELECT RAISE(ABORT,'fixture withdrawal failure'); END`)
	if err := Run(context.Background(), c, true); !errors.Is(err, ErrCatalog) {
		t.Fatal("failed withdrawal allowed startup", err)
	}
	var state string
	var started int
	if err := db.QueryRow("SELECT state,started_at_ms IS NOT NULL FROM jobs WHERE id=?", job).Scan(&state, &started); err != nil || state != "queued" || started != 0 {
		t.Fatal("work started after failed withdrawal", state, started, err)
	}
}
