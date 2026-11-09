package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestReOCRCancelKeepsPrior(t *testing.T) {
	f, _ := ocrCandidateFixture(t)
	ctx := context.Background()
	if err := f.store.RequestCancel(ctx, alice, jobA); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, `SELECT count(*) FROM reservations WHERE job_id=?`, jobA) != 1 {
		t.Fatal("cancel prematurely freed reservation")
	}
	if err := f.store.FinishStoppedRender(ctx, alice, jobA, RenderStop{RunnerID: runner, ProcessStoppedAtMS: 2000, WorkspaceCleanedAtMS: 2000}, "failed"); err != nil {
		t.Fatal(err)
	}
	p, err := f.store.ReadDocument(ctx, alice, docA)
	if err != nil || p.State != "ready" || p.Output == nil {
		t.Fatal("stopped optional OCR did not restore prior ready result", p.State, err)
	}
	if ref, err := f.store.ReadCurrentOutput(ctx, alice, docA); err != nil || ref.Artifact.ID != "artifact_prior_output_001" {
		t.Fatal(ref, err)
	}
}

func TestStoppedRenderReleasesOnlyAfterEvidence(t *testing.T) {
	for _, mode := range []string{"failed", "interrupted", "cancelled", "disabled", "expired", "deleting", "new-revision"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			ctx := context.Background()
			admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
				t.Fatal(err)
			}
			outcome := "failed"
			state := "review"
			terminal := "failed"
			switch mode {
			case "interrupted":
				outcome = "interrupted"
				terminal = outcome
			case "cancelled":
				if err = f.store.RequestCancel(ctx, alice, admission.JobID); err != nil {
					t.Fatal(err)
				}
				terminal = "cancelled"
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "expired":
				f.now = 10000
				state = "expired"
			case "deleting":
				must(t, f.docs, "UPDATE documents SET state='deleting' WHERE id=?", docA)
				state = "deleting"
			case "new-revision":
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", docA)
				state = "processing"
			}
			stop := RenderStop{RunnerID: runner, ProcessStoppedAtMS: 2000, WorkspaceCleanedAtMS: 2000}
			if err = f.store.FinishStoppedRender(ctx, alice, admission.JobID, stop, outcome); err != nil {
				t.Fatal(err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admission.JobID) != 0 {
				t.Fatal("reservation retained")
			}
			var actual string
			if err = f.docs.QueryRow("SELECT state FROM jobs WHERE id=?", admission.JobID).Scan(&actual); err != nil || actual != terminal {
				t.Fatal("wrong terminal state")
			}
			if err = f.docs.QueryRow("SELECT state FROM documents WHERE id=?", docA).Scan(&actual); err != nil || actual != state {
				t.Fatal("document revived or overwritten")
			}
			recorded, err := f.store.RenderStoppedRecorded(ctx, alice, admission.JobID, runner)
			if err != nil || !recorded {
				t.Fatal("cleanup not recorded")
			}
			if err = f.store.FinishStoppedRender(ctx, alice, admission.JobID, stop, outcome); !errors.Is(err, ErrConflict) {
				t.Fatal("terminal job changed twice")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE document_id=?", docA) != 1 {
				t.Fatal("original pages removed")
			}
		})
	}
}
func TestStoppedRenderRejectsUnsafeEvidence(t *testing.T) {
	for _, mode := range []string{"unstarted", "foreign-runner", "foreign-owner", "before-start", "clean-before-stop", "future", "success-outcome", "published"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			ctx := context.Background()
			admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "unstarted" {
				if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
					t.Fatal(err)
				}
			}
			stop := RenderStop{RunnerID: runner, ProcessStoppedAtMS: 2000, WorkspaceCleanedAtMS: 2000}
			owner, outcome := alice, "failed"
			switch mode {
			case "foreign-runner":
				stop.RunnerID = "worker_foreign_001"
			case "foreign-owner":
				owner = bob
			case "before-start":
				stop.ProcessStoppedAtMS = 1999
			case "clean-before-stop":
				stop.WorkspaceCleanedAtMS = 1999
			case "future":
				stop.WorkspaceCleanedAtMS = 2001
			case "success-outcome":
				outcome = "succeeded"
			case "published":
				if err = f.store.PublishExport(ctx, alice, admission.JobID, runner, admission.Revision, output()); err != nil {
					t.Fatal(err)
				}
			}
			if err = f.store.FinishStoppedRender(ctx, owner, admission.JobID, stop, outcome); err == nil {
				t.Fatal("unsafe cleanup accepted")
			}
			expected := 1
			if mode == "published" {
				expected = 0
			}
			if scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", admission.JobID) != expected {
				t.Fatal("reservation modified on rejection")
			}
			if mode == "published" {
				if _, err = f.store.GetExport(ctx, alice, docA); err != nil {
					t.Fatal("published output invalidated")
				}
			}
		})
	}
}
func TestRenderStopCommitReadBack(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "committed-error"}[committed], func(t *testing.T) {
			f := renderFixture(t)
			ctx := context.Background()
			admission, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimExport(ctx, alice, admission.JobID, runner); err != nil {
				t.Fatal(err)
			}
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("injected commit outcome")
			}
			err = f.store.FinishStoppedRender(ctx, alice, admission.JobID, RenderStop{RunnerID: runner, ProcessStoppedAtMS: 2000, WorkspaceCleanedAtMS: 2000}, "failed")
			if !errors.Is(err, ErrCommitUncertain) {
				t.Fatal("uncertainty lost")
			}
			recorded, err := f.store.RenderStoppedRecorded(ctx, alice, admission.JobID, runner)
			if err != nil || recorded != committed {
				t.Fatal("incorrect read-back")
			}
		})
	}
}
