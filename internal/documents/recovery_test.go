package documents

import (
	"context"
	"errors"
	"testing"
)

func TestRecoveryRequiredPreservesUncertainWorkAcrossOwners(t *testing.T) {
	for _, mode := range []string{"queued-unstarted", "running", "cancelling", "reconciling", "requeued-started", "queued-runner", "disabled-expired-running", "cleanup-running", "completed", "idle-device", "held-device", "reconciling-device"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			expected := true
			switch mode {
			case "queued-unstarted":
				expected = false
			case "running", "cancelling", "reconciling", "requeued-started", "disabled-expired-running", "completed":
				claim(t, f)
				switch mode {
				case "cancelling", "reconciling":
					must(t, f.docs, "UPDATE jobs SET state=? WHERE id=?", mode, jobA)
				case "requeued-started":
					must(t, f.docs, "UPDATE jobs SET state='queued',runner_id=NULL WHERE id=?", jobA)
				case "disabled-expired-running":
					must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
					f.now = 10000
				case "completed":
					if err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); err != nil {
						t.Fatal(err)
					}
					expected = false
				}
			case "queued-runner":
				must(t, f.docs, "UPDATE jobs SET runner_id=? WHERE id=?", runner, jobA)
			case "cleanup-running":
				must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms) VALUES('cleanup_recovery_001',?,?,'cleanup','running',1,'{}',1500,9000,?,1500)`, docB, bob, runner)
				expected = false
			case "idle-device":
				must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state) VALUES('scanner_recovery_01',1,'idle')")
				expected = false
			case "held-device", "reconciling-device":
				must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('scan_recovery_0001',?,?,'scan','queued',1,'{}',1500,9000)`, docB, bob)
				state := "held"
				if mode == "reconciling-device" {
					state = "reconciling"
				}
				must(t, f.docs, "INSERT INTO device_leases(device_id,epoch,state,job_id) VALUES('scanner_recovery_01',1,?,'scan_recovery_0001')", state)
			}
			beforeJobs := scalar(t, f.docs, "SELECT count(*) FROM jobs")
			beforeReserved := scalar(t, f.docs, "SELECT COALESCE(sum(byte_size),0) FROM reservations")
			required, err := f.store.RecoveryRequired(context.Background())
			if err != nil || required != expected {
				t.Fatal("incorrect recovery barrier", mode, required, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != beforeJobs || scalar(t, f.docs, "SELECT COALESCE(sum(byte_size),0) FROM reservations") != beforeReserved {
				t.Fatal("recovery check changed work")
			}
		})
	}
}
func TestRecoveryRequiredUnavailableIsNotClear(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if clear, err := f.store.RecoveryRequired(ctx); clear || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read reported clear", clear, err)
	}
	if err := f.docs.Close(); err != nil {
		t.Fatal(err)
	}
	if clear, err := f.store.RecoveryRequired(context.Background()); clear || !errors.Is(err, ErrStorage) {
		t.Fatal("failed read reported clear", clear, err)
	}
}
