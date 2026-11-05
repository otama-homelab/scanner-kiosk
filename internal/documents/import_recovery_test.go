package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAbortUnstartedImportCommitUncertaintyAndBatching(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "lost-ack"}[committed], func(t *testing.T) {
			f := scanFixture(t)
			ctx := context.Background()
			// The first active intake must not starve a later withdrawn one.
			if _, err := f.store.AdmitImport(ctx, alice, importRequest(), importLimits()); err != nil {
				t.Fatal(err)
			}
			request := importRequest()
			request.IdempotencyKey = "request_withdraw_next1"
			a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit acknowledgement")
			}
			if count, err := f.store.AbortUnstartedImports(ctx, 1); count != 0 || !errors.Is(err, ErrCommitUncertain) {
				t.Fatal("uncertainty hidden", count, err)
			}
			want := 1
			if committed {
				want = 0
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != want {
				t.Fatal("partial transaction")
			}
			f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
			if count, err := f.store.AbortUnstartedImports(ctx, 1); err != nil || count != want {
				t.Fatal("retry", count, err)
			}
		})
	}
}

func TestAbortUnstartedImportsPreservesLiveProducerAndReadySource(t *testing.T) {
	for _, intake := range []string{"pending", "receiving", "ready", "running"} {
		for _, reason := range []string{"active", "cancel", "deadline", "expired", "disabled"} {
			t.Run(intake+"/"+reason, func(t *testing.T) {
				f := scanFixture(t)
				ctx := context.Background()
				request := importRequest()
				a, err := f.store.AdmitImport(ctx, alice, request, importLimits())
				if err != nil {
					t.Fatal(err)
				}
				if intake != "pending" {
					if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err != nil {
						t.Fatal(err)
					}
				}
				if intake == "ready" || intake == "running" {
					if err = f.store.SealStoppedImportUpload(ctx, alice, a.JobID, a.ProducerID, intakeArtifact(a, request.InputBytes, request.SHA256), f.now); err != nil {
						t.Fatal(err)
					}
				}
				if intake == "running" {
					if _, err = f.store.ClaimNextImport(ctx, runner); err != nil {
						t.Fatal(err)
					}
				}
				switch reason {
				case "cancel":
					if err = f.store.RequestCancel(ctx, alice, a.JobID); err != nil {
						t.Fatal(err)
					}
				case "deadline":
					f.now = a.DeadlineAtMS
				case "expired":
					f.now += importLimits().DocumentLifetime.Milliseconds()
				case "disabled":
					must(t, f.state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", f.now, alice)
				}
				count, err := f.store.AbortUnstartedImports(ctx, 100)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if reason != "active" && (intake == "pending" || intake == "ready") {
					want = 1
				}
				if count != want {
					t.Fatal("incorrect terminalization", count, want)
				}
				if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", a.JobID) != 1-want {
					t.Fatal("reservation authority changed")
				}
				if want == 1 {
					if err = f.store.StartImportUpload(ctx, alice, a.JobID, a.ProducerID); err == nil {
						t.Fatal("cancelled producer restarted")
					}
					if scalar(t, f.docs, "SELECT count(*) FROM job_artifacts WHERE job_id=? AND artifact_id=?", a.JobID, a.Input.ID) != 1 {
						t.Fatal("source not owned for cleanup")
					}
					if intake == "ready" && int64(scalar(t, f.docs, "SELECT byte_size FROM artifacts WHERE id=?", a.Input.ID)) != request.InputBytes {
						t.Fatal("ready source changed")
					}
				}
			})
		}
	}
}
