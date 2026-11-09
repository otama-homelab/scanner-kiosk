package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func reOCRCorrectionWork(t *testing.T) (*fixture, Admission) {
	t.Helper()
	f, correction, _ := correctionFixture(t)
	if _, err := f.store.ApplyCorrection(context.Background(), alice, correction, time.Hour); err != nil {
		t.Fatal(err)
	}
	seedImportedPDFPage(t, f)
	seedPageAt(t, f, docA, alice, "original_alice_0003", "page_alice_000003", 2)
	r := reOCRRequest()
	r.Revision = 2
	a, err := f.store.AdmitRender(context.Background(), alice, r, admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, a.JobID, runner); err != nil {
		t.Fatal(err)
	}
	return f, a
}

func TestReOCRWorkUsesAppliedCorrection(t *testing.T) {
	f, a := reOCRCorrectionWork(t)
	w, err := f.store.LoadRenderWork(context.Background(), alice, a.JobID, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Inputs) != 3 || w.Inputs[0].ID != "artifact_correction_one" || w.Snapshot.Pages[0].OriginalArtifactID != "artifact_correction_original" || len(w.Originals) != 3 || w.Originals[0].ID != "artifact_correction_original" || w.Inputs[1].MediaType != "application/pdf" || w.Inputs[2].ID != w.Originals[2].ID {
		t.Fatal("effective input lost correction or original lineage")
	}
}

func TestReOCRWorkRejectsSourceDrift(t *testing.T) {
	for _, mode := range []string{"hash", "transform", "rotation", "original-hash", "correction-removed", "geometry"} {
		t.Run(mode, func(t *testing.T) {
			f, a := reOCRCorrectionWork(t)
			// Simulate stored metadata corruption beyond normal immutable-write guards.
			must(t, f.docs, "DROP TRIGGER artifact_identity_immutable")
			must(t, f.docs, "DROP TRIGGER correction_suggestion_immutable")
			must(t, f.docs, "DROP TRIGGER page_capture_immutable")
			switch mode {
			case "hash":
				must(t, f.docs, "UPDATE artifacts SET sha256=zeroblob(32) WHERE id='artifact_correction_one'")
			case "original-hash":
				must(t, f.docs, "UPDATE artifacts SET sha256=zeroblob(32) WHERE id='artifact_correction_original'")
			case "transform":
				must(t, f.docs, "UPDATE page_corrections SET transform_json='{}' WHERE state='applied'")
			case "rotation":
				must(t, f.docs, "UPDATE pages SET rotation=90 WHERE id=?", correctionPage)
			case "correction-removed":
				must(t, f.docs, "UPDATE page_corrections SET state='discarded',applied_at_ms=NULL WHERE state='applied'")
			case "geometry":
				must(t, f.docs, "UPDATE pages SET pixel_width=961 WHERE id=?", correctionPage)
			}
			if w, err := f.store.LoadRenderWork(context.Background(), alice, a.JobID, runner); err == nil || len(w.Inputs) != 0 {
				t.Fatal("changed effective source accepted")
			}
		})
	}
}

func TestRenderWorkMatchesClaimedSnapshot(t *testing.T) {
	f := renderFixture(t)
	seedPageWithCapture(t, f, docA, alice, "original_alice_0002", "page_alice_000002", 1, "a5", "gray", 600)
	must(t, f.docs, "UPDATE pages SET rotation=90 WHERE id=?", "page_alice_000002")
	admitted, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.LoadRenderWork(context.Background(), alice, admitted.JobID, runner); !errors.Is(err, ErrConflict) {
		t.Fatal("unclaimed job exposed")
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, admitted.JobID, runner); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.LoadRenderWork(context.Background(), alice, admitted.JobID, runner)
	if err != nil || len(work.Inputs) != 2 {
		t.Fatalf("resolve: %v", err)
	}
	if work.Inputs[1].ID != "original_alice_0002" || work.Snapshot.Pages[1].Rotation != 90 || work.Snapshot.Pages[1].DPI != 600 || work.Snapshot.Pages[1].Size != "a5" || work.ReserveBytes != 50 {
		t.Fatal("snapshot/metadata mismatch")
	}
}

func TestRenderWorkRejectsUnavailableOrChangedInputs(t *testing.T) {
	for _, mode := range []string{"foreign-owner", "foreign-runner", "cancelled", "expired", "disabled", "revision", "rotation", "missing-page", "not-ready", "deadline", "ttl"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			ctx := context.Background()
			admitted, err := f.store.AdmitRender(ctx, alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimExport(ctx, alice, admitted.JobID, runner); err != nil {
				t.Fatal(err)
			}
			owner, worker := alice, runner
			switch mode {
			case "foreign-owner":
				owner = bob
			case "foreign-runner":
				worker = "runner_foreign_0001"
			case "cancelled":
				if err = f.store.RequestCancel(ctx, alice, admitted.JobID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				must(t, f.docs, "UPDATE documents SET state='expired' WHERE id=?", docA)
			case "deadline":
				f.now += 60001
			case "ttl":
				f.now += 86400000
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=1001 WHERE id=?", alice)
			case "revision":
				must(t, f.docs, "UPDATE documents SET revision=revision+1 WHERE id=?", docA)
			case "rotation":
				must(t, f.docs, "UPDATE pages SET rotation=90 WHERE document_id=?", docA)
			case "missing-page":
				must(t, f.docs, "DELETE FROM pages WHERE document_id=?", docA)
			case "not-ready":
				must(t, f.docs, "UPDATE artifacts SET state='deleting' WHERE id=?", "original_alice_0001")

			}
			work, err := f.store.LoadRenderWork(ctx, owner, admitted.JobID, worker)
			if err == nil || len(work.Inputs) != 0 || work.Job.ID != "" {
				t.Fatal("invalid work returned content")
			}
		})
	}
}
