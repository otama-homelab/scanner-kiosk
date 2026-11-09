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
