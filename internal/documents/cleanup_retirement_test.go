package documents

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func completedForRetirement(t *testing.T) (*fixture, string) {
	t.Helper()
	f, job, work := claimedCleanup(t)
	acknowledgeAll(t, f, job, work)
	if err := f.store.FinishCleanup(context.Background(), alice, job, runner); err != nil {
		t.Fatal(err)
	}
	return f, job
}

func TestRetirementKeepsReceiptsAsGoneKeys(t *testing.T) {
	f, job := completedForRetirement(t)
	ctx := context.Background()
	f.now += 72 * time.Hour.Milliseconds()
	before := scalar(t, f.docs, "SELECT count(*) FROM request_receipts WHERE owner_id=?", alice)
	count, err := f.store.PurgeCompletedCleanup(ctx, 100, 72*time.Hour)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM documents WHERE id=?", docA) != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=?", job) != 0 {
		t.Fatal("status retained")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM request_receipts WHERE owner_id=?", alice) != before || scalar(t, f.docs, "SELECT count(*) FROM request_receipts WHERE result_job_id=?", job) != 0 {
		t.Fatal("replay tombstones lost or stale FK")
	}
	if _, err = f.store.RequestDelete(ctx, alice, deleteRequest(), time.Hour); !errors.Is(err, ErrGone) {
		t.Fatal("expired exact command re-enqueued", err)
	}
	if _, err = f.store.ReadJob(ctx, alice, job); !errors.Is(err, ErrUnavailable) {
		t.Fatal("status still visible", err)
	}
	if again, err := f.store.PurgeCompletedCleanup(ctx, 100, time.Hour); err != nil || again != 0 {
		t.Fatal(again, err)
	}
}

func TestRetirementRequiresAllRetentionBoundaries(t *testing.T) {
	for _, mode := range []string{"completion-window", "original-deadline", "live-receipt", "disabled", "late-completion"} {
		t.Run(mode, func(t *testing.T) {
			f, job := completedForRetirement(t)
			ctx := context.Background()
			retention := time.Hour
			var original int64
			if err := f.docs.QueryRow("SELECT deadline_at_ms FROM jobs WHERE id=?", job).Scan(&original); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "completion-window":
				retention = 72 * time.Hour
				f.now += retention.Milliseconds() - 1
			case "original-deadline":
				f.now = original - 1
				retention = time.Second
			case "live-receipt":
				f.now += 72 * time.Hour.Milliseconds()
				must(t, f.docs, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,zeroblob(32),'delete_document',zeroblob(32),?,?)`, alice, job, f.now+1)
			case "disabled":
				f.now += 72 * time.Hour.Milliseconds()
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "late-completion":
				f.now = original + 72*time.Hour.Milliseconds()
				must(t, f.docs, "UPDATE jobs SET heartbeat_at_ms=? WHERE id=?", f.now, job)
			}
			count, err := f.store.PurgeCompletedCleanup(ctx, 1, retention)
			expected := 0
			if mode == "disabled" {
				expected = 1
			}
			if err != nil || count != expected {
				t.Fatal(mode, count, err)
			}
			if expected == 0 {
				if mode == "late-completion" {
					f.now += retention.Milliseconds()
				} else {
					f.now++
				}
				count, err = f.store.PurgeCompletedCleanup(ctx, 1, retention)
				if err != nil || count != 1 {
					t.Fatal("exact boundary", mode, count, err)
				}
			}
		})
	}
}

func TestRetirementRejectsIncompleteOrInconsistentCleanup(t *testing.T) {
	for _, mode := range []string{"pending", "payload", "reservation", "edit-result"} {
		t.Run(mode, func(t *testing.T) {
			f, job, work := claimedCleanup(t)
			ctx := context.Background()
			if mode != "pending" {
				acknowledgeAll(t, f, job, work)
				if err := f.store.FinishCleanup(ctx, alice, job, runner); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "payload":
				must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('artifact_retained_01',?,?,'intermediate','ready','retained.bin','application/octet-stream',1,zeroblob(32),1,2000)`, docA, alice)
			case "reservation":
				must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1)", job, alice)
			case "edit-result":
				must(t, f.docs, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,zeroblob(32),'rotate',zeroblob(32),NULL,2001)`, alice)
				must(t, f.docs, `INSERT INTO edit_results(owner_id,key_hash,document_id,projection_json) VALUES(?,zeroblob(32),?,'{}')`, alice, docA)
			}
			f.now += 72 * time.Hour.Milliseconds()
			if count, err := f.store.PurgeCompletedCleanup(ctx, 100, time.Hour); err != nil || count != 0 {
				t.Fatal(count, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM documents WHERE id=?", docA) != 1 {
				t.Fatal("unverified document purged")
			}
		})
	}
}

func TestRetirementCannotReexecuteOldPhysicalScan(t *testing.T) {
	f := scanFixture(t)
	ctx := context.Background()
	request := scanRequest()
	a, err := f.store.AdmitScan(ctx, alice, request, scanLimits())
	if err != nil {
		t.Fatal(err)
	}
	deletion := DeleteRequest{DocumentID: a.DocumentID, Revision: a.Revision, IdempotencyKey: "retire_scan_delete_01"}
	cleanup, err := f.store.RequestDelete(ctx, alice, deletion, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.AbortUnstartedScan(ctx, alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimCleanup(ctx, alice, cleanup.ID, runner); err != nil {
		t.Fatal(err)
	}
	if err = f.store.FinishCleanup(ctx, alice, cleanup.ID, runner); err != nil {
		t.Fatal(err)
	}
	f.now += 72 * time.Hour.Milliseconds()
	if count, err := f.store.PurgeCompletedCleanup(ctx, 100, 72*time.Hour); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = f.store.AdmitScan(ctx, alice, request, scanLimits()); !errors.Is(err, ErrGone) {
		t.Fatal("old scan was admitted again", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='scan'") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("native work resurrected")
	}
}

func TestRetirementDoesNotHideCommitUncertainty(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "committed"}[committed], func(t *testing.T) {
			f, job := completedForRetirement(t)
			f.now += 72 * time.Hour.Milliseconds()
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit acknowledgement")
			}
			count, err := f.store.PurgeCompletedCleanup(context.Background(), 100, time.Hour)
			if count != 0 || !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(count, err)
			}
			expected := 1
			if committed {
				expected = 0
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=?", job) != expected {
				t.Fatal("partial or wrong commit")
			}
		})
	}
}

func TestRetirementBatchBoundAndIndexes(t *testing.T) {
	f, job := completedForRetirement(t)
	ctx := context.Background()
	must(t, f.docs, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('retired_extra_doc_01',?,'document','expired',1000,10000)`, alice)
	must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms,runner_id,started_at_ms,heartbeat_at_ms) VALUES('retired_extra_job_01','retired_extra_doc_01',?,'cleanup','succeeded',1,'{"version":1}',2000,3000,?,2000,2000)`, alice, runner)
	f.now += 72 * time.Hour.Milliseconds()
	rows, err := f.docs.QueryContext(ctx, "EXPLAIN QUERY PLAN "+cleanupRetirementQuery, f.now-time.Hour.Milliseconds(), f.now, f.now, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"jobs_cleanup_retirement", "request_receipts_result_expiry", "edit_results_document"} {
		if !strings.Contains(plan, index) {
			t.Fatal("bounded retirement lookup lost index", index, plan)
		}
	}
	for _, limit := range []int{0, 101} {
		if _, err = f.store.PurgeCompletedCleanup(ctx, limit, time.Hour); !errors.Is(err, ErrInvalid) {
			t.Fatal(limit, err)
		}
	}
	if count, err := f.store.PurgeCompletedCleanup(ctx, 1, time.Hour); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE kind='cleanup'") != 1 {
		t.Fatal("batch bound ignored")
	}
	if count, err := f.store.PurgeCompletedCleanup(ctx, 1, time.Hour); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=?", job) != 0 {
		t.Fatal("status not retired")
	}
}
