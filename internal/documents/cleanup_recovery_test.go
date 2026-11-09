package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

const successor = "worker_cleanup_next_01"
const thirdRunner = "worker_cleanup_third_01"

func TestDeliveryCleanupAndRetirement(t *testing.T) {
	ctx := context.Background()
	f, d := readyDelivery(t)
	job, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "delete_delivery_0001"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, "", runner, time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatal("cleanup must not claim a delivery-pinned output", err)
	}
	if scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE id=? AND state='ready'`, d.Artifact.ID) != 1 {
		t.Fatal("active artifact removed")
	}
	if _, err = f.store.NextCleanup(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatal("delivery pin stalled cleanup dispatcher", err)
	}
	f.now = d.ExpiresAtMS
	if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, "", runner, time.Minute); err != nil {
		t.Fatal("TTL did not release pin", err)
	}
	work, err := f.store.LoadCleanupWork(ctx, alice, job.ID, runner, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range work.Files {
		if err = f.store.RecordCleanupRemoval(ctx, alice, job.ID, runner, removal(file, f.now)); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.store.FinishCleanup(ctx, alice, job.ID, runner); err != nil {
		t.Fatal(err)
	}
	reopenDelivery(t, f)
	if scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE document_id=?`, docA) != 0 {
		t.Fatal("expired private artifact metadata retained")
	}
	if scalar(t, f.docs, `SELECT count(*) FROM deliveries WHERE id=? AND state='retired' AND document_id='retired' AND relative_path='retired' AND task_id IS NULL AND remote_document_id IS NULL AND expires_at_ms=10000`, d.ID) != 1 {
		t.Fatal("retirement lost fence or kept private locators")
	}
	if _, err = f.store.AdmitDelivery(ctx, alice, deliveryRequest()); !errors.Is(err, ErrGone) {
		t.Fatal("old key revived send", err)
	}
	if _, err = f.docs.Exec(`DELETE FROM deliveries WHERE id=?`, d.ID); err == nil {
		t.Fatal("semantic evidence deletable")
	}
}

func TestDeliveryCancelClaimRace(t *testing.T) {
	ctx := context.Background()
	for _, order := range []string{"cancel-first", "claim-first", "race"} {
		t.Run(order, func(t *testing.T) {
			f, d := readyDelivery(t)
			if order == "cancel-first" {
				if got, err := f.store.CancelDelivery(ctx, alice, d.ID); err != nil || got.State != "cancelled" {
					t.Fatal(got, err)
				}
			}
			if order == "claim-first" {
				if _, err := f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, runner); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			done := make(chan error, 2)
			go func() { <-start; _, err := f.store.CancelDelivery(ctx, alice, d.ID); done <- err }()
			go func() { <-start; _, err := f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, runner); done <- err }()
			close(start)
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil && !errors.Is(err, ErrConflict) {
					t.Fatal(err)
				}
			}
			reopenDelivery(t, f)
			got, err := f.store.ReadDelivery(ctx, alice, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State == "cancelled" {
				if got.AttemptedAtMS != 0 {
					t.Fatal("attempted delivery called cancelled")
				}
			} else if got.State != "attempted" || got.AttemptedAtMS == 0 {
				t.Fatal("race rewound attempt", got)
			}
			if order == "cancel-first" && got.State != "cancelled" {
				t.Fatal("cancel did not win")
			}
			if order == "claim-first" && got.State != "attempted" {
				t.Fatal("attempt incorrectly rolled back")
			}
		})
	}
}

func TestDeliveryUnknownTTLRelease(t *testing.T) {
	ctx := context.Background()
	for _, state := range []string{"queued", "attempted", "unknown", "cancelled", "confirmed", "failed"} {
		t.Run(state, func(t *testing.T) {
			f, d := readyDelivery(t)
			const task = "12345678-1234-4234-8234-123456789abc"
			if state != "queued" && state != "cancelled" {
				if _, err := f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, runner); err != nil {
					t.Fatal(err)
				}
			}
			if state == "unknown" {
				if err := f.store.MarkDeliveryUnknown(ctx, alice, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			if state == "cancelled" {
				if _, err := f.store.CancelDelivery(ctx, alice, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			if state == "confirmed" || state == "failed" {
				if err := f.store.AcceptDelivery(ctx, alice, d.ID, task); err != nil {
					t.Fatal(err)
				}
				var err error
				if state == "confirmed" {
					err = f.store.ConfirmDelivery(ctx, alice, d.ID, task, 42, d.Artifact.SHA256)
				} else {
					err = f.store.FailDelivery(ctx, alice, d.ID, task)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			job, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "delete_delivery_ttl"}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
			active := state == "queued" || state == "attempted" || state == "unknown"
			if active {
				if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, "", runner, time.Minute); !errors.Is(err, ErrConflict) {
					t.Fatal("disabled owner released live bytes", err)
				}
				f.now = d.ExpiresAtMS
			}
			if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, "", runner, time.Minute); err != nil {
				t.Fatal("pin not released on terminal/TTL", err)
			}
			reopenDelivery(t, f)
			if scalar(t, f.docs, `SELECT expires_at_ms FROM deliveries WHERE id=?`, d.ID) != int(d.ExpiresAtMS) {
				t.Fatal("TTL extended")
			}
			if scalar(t, f.docs, `SELECT count(*) FROM deliveries WHERE id=? AND state='retired' AND task_id IS NULL AND remote_document_id IS NULL`, d.ID) != 1 {
				t.Fatal("remote details survived expiry")
			}
		})
	}
}

func TestDeliveryCleanupCommitUncertain(t *testing.T) {
	ctx := context.Background()
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "ack-loss"}[committed], func(t *testing.T) {
			f, d := readyDelivery(t)
			if _, err := f.store.CancelDelivery(ctx, alice, d.ID); err != nil {
				t.Fatal(err)
			}
			job, err := f.store.RequestDelete(ctx, alice, DeleteRequest{DocumentID: docA, Revision: 2, IdempotencyKey: "delete_delivery_fault"}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost cleanup ack")
			}
			if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, "", runner, time.Minute); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			reopenDelivery(t, f)
			_, err = f.store.LoadCleanupWork(ctx, alice, job.ID, runner, 100)
			if !committed {
				if !errors.Is(err, ErrConflict) {
					t.Fatal("rolled-back cleanup acquired IO", err)
				}
			} else {
				if err != nil {
					t.Fatal("known cleanup readback lost", err)
				}
				if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job.ID, runner, successor, time.Minute); err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.LoadCleanupWork(ctx, alice, job.ID, runner, 100); !errors.Is(err, ErrConflict) {
					t.Fatal("old cleanup runner retained authority", err)
				}
			}
			if scalar(t, f.docs, `SELECT count(*) FROM deliveries`) != 1 {
				t.Fatal("uncertain cleanup erased fence")
			}
			if _, err = f.store.ClaimDeliveryAttempt(ctx, alice, d.ID, successor); err == nil {
				t.Fatal("uncertain cleanup authorized send")
			}
		})
	}
}

func TestCleanupTakeoverRetainsProgressAndFencesOldRunner(t *testing.T) {
	f, job, work := claimedCleanup(t)
	ctx := context.Background()
	first := work.Files[0]
	if err := f.store.RecordCleanupRemoval(ctx, alice, job, runner, removal(first, f.now)); err != nil {
		t.Fatal(err)
	}
	originalDeadline := work.Job.DeadlineAtMS
	f.now = originalDeadline + 1 // Content/receipt expiry never re-authorizes content.
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	candidate, err := f.store.NextCleanup(ctx)
	if err != nil || candidate.JobID != job || candidate.PreviousRunner != runner {
		t.Fatal(candidate, err)
	}
	claimed, err := f.store.ClaimCleanupAttempt(ctx, alice, job, runner, successor, time.Minute)
	if err != nil || claimed.DeadlineAtMS != f.now+time.Minute.Milliseconds() {
		t.Fatal(claimed, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND started_at_ms=2000 AND deadline_at_ms=? AND runner_id=?", job, originalDeadline, successor) != 1 {
		t.Fatal("immutable job history changed")
	}
	current, err := f.store.LoadCleanupWork(ctx, alice, job, successor, 100)
	if err != nil || current.Remaining != work.Remaining-1 || len(current.Files) != len(work.Files)-1 {
		t.Fatal(current, err)
	}
	if _, err = f.store.LoadCleanupWork(ctx, alice, job, runner, 1); !errors.Is(err, ErrConflict) {
		t.Fatal("old load", err)
	}
	if err = f.store.RecordCleanupRemoval(ctx, alice, job, runner, removal(current.Files[0], f.now)); !errors.Is(err, ErrConflict) {
		t.Fatal("old report", err)
	}
	if err = f.store.FinishCleanup(ctx, alice, job, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("old finish", err)
	}
	if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job, runner, thirdRunner, time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatal("stale candidate stole new attempt", err)
	}
	if err = f.store.RecordCleanupRemoval(ctx, alice, job, successor, removal(current.Files[0], 2000)); !errors.Is(err, ErrConflict) {
		t.Fatal("old proof accepted for new attempt", err)
	}
	for _, file := range current.Files {
		if err = f.store.RecordCleanupRemoval(ctx, alice, job, successor, removal(file, f.now)); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.store.FinishCleanup(ctx, alice, job, successor); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM cleanup_attempts") != 0 {
		t.Fatal("attempt retained after finish")
	}
	if recorded, err := f.store.CleanupFinishedRecorded(ctx, alice, job, successor); err != nil || !recorded {
		t.Fatal(recorded, err)
	}
}

func TestCleanupAttemptDeadlineAndRenewal(t *testing.T) {
	f, job := cleanupFixture(t)
	ctx := context.Background()
	f.now += 2 * time.Hour.Milliseconds() // queued job's immutable deadline has passed
	first, err := f.store.ClaimCleanupAttempt(ctx, alice, job, "", runner, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f.now = first.DeadlineAtMS
	if _, err = f.store.LoadCleanupWork(ctx, alice, job, runner, 1); !errors.Is(err, ErrConflict) {
		t.Fatal("expired IO continued", err)
	}
	if _, err = f.store.ClaimCleanupAttempt(ctx, alice, job, runner, runner, time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatal("same runner renewed itself", err)
	}
	second, err := f.store.ClaimCleanupAttempt(ctx, alice, job, runner, successor, time.Minute)
	if err != nil || second.DeadlineAtMS != f.now+time.Minute.Milliseconds() {
		t.Fatal(second, err)
	}
	if scalar(t, f.docs, "SELECT generation FROM cleanup_attempts WHERE job_id=?", job) != 2 {
		t.Fatal("generation not advanced")
	}
	if _, err = f.store.LoadCleanupWork(ctx, alice, job, successor, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = f.docs.Exec("UPDATE cleanup_attempts SET deadline_at_ms=deadline_at_ms+1 WHERE job_id=?", job); err == nil {
		t.Fatal("attempt extended without takeover")
	}
}

func TestCleanupAttemptRechecksBarrierAndManifest(t *testing.T) {
	for _, mode := range []string{"manifest", "reservation", "live-document", "wrong-owner", "wrong-runner"} {
		t.Run(mode, func(t *testing.T) {
			f, job, work := claimedCleanup(t)
			ctx := context.Background()
			who, old := alice, runner
			switch mode {
			case "manifest":
				must(t, f.docs, "DELETE FROM cleanup_files WHERE job_id=? AND artifact_id=?", job, work.Files[0].ArtifactID)
			case "reservation":
				must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,1)", job, alice)
			case "live-document":
				must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", work.Job.DocumentID)
			case "wrong-owner":
				who = bob
			case "wrong-runner":
				old = thirdRunner
			}
			if _, err := f.store.ClaimCleanupAttempt(ctx, who, job, old, successor, time.Minute); !errors.Is(err, ErrConflict) {
				t.Fatal(mode, err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND runner_id=?", job, runner) != 1 || scalar(t, f.docs, "SELECT count(*) FROM cleanup_attempts") != 0 {
				t.Fatal("failed takeover changed ownership")
			}
		})
	}
}

func TestCleanupAttemptCommitUncertaintyReadback(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "committed"}[committed], func(t *testing.T) {
			f, job, _ := claimedCleanup(t)
			ctx := context.Background()
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit result")
			}
			if _, err := f.store.ClaimCleanupAttempt(ctx, alice, job, runner, successor, time.Minute); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			_, err := f.store.LoadCleanupWork(ctx, alice, job, successor, 1)
			if committed && err != nil || !committed && !errors.Is(err, ErrConflict) {
				t.Fatal(committed, err)
			}
		})
	}
}

func TestCleanupTakeoverRaceOnlyOneSuccessorWins(t *testing.T) {
	f, job, _ := claimedCleanup(t)
	other := secondStore(t, f)
	ctx := context.Background()
	results := make(chan error, 2)
	go func() {
		_, err := f.store.ClaimCleanupAttempt(ctx, alice, job, runner, successor, time.Minute)
		results <- err
	}()
	go func() {
		_, err := other.ClaimCleanupAttempt(ctx, alice, job, runner, thirdRunner, time.Minute)
		results <- err
	}()
	accepted, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 || scalar(t, f.docs, "SELECT count(*) FROM cleanup_attempts") != 1 {
		t.Fatal(accepted, conflicts)
	}
}
