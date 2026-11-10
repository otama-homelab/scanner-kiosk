package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPhotoUsesSharedSuggestionApplyAndUndo(t *testing.T) {
	for _, mode := range []string{"apply", "normalized-drift", "original-drift", "correction-drift", "invalid-transform", "foreign", "stale", "expired"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f, r := manualPhotoFixture(t)
			a, err := f.store.AdmitRender(ctx, alice, r, admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.ClaimExport(ctx, alice, a.JobID, runner); err != nil {
				t.Fatal(err)
			}
			if err = f.store.CompletePhotoCrop(ctx, alice, a.JobID, runner, 1, manualArtifact()); err != nil {
				t.Fatal(err)
			}
			must(t, f.docs, `INSERT INTO page_corrections SELECT 'suggestion_photo_shared001',document_id,owner_id,page_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json,confidence_kind,confidence_value,risk_flags,'suggested',created_at_ms,NULL FROM page_corrections WHERE suggestion_id=?`, manualArtifact().ID)
			p, err := f.store.ReadDocument(ctx, alice, r.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			original, err := f.store.ReadPageOriginal(ctx, alice, p.ID, r.ManualCrop.PageID)
			if err != nil {
				t.Fatal(err)
			}
			request := CorrectionRequest{DocumentID: p.ID, PageID: r.ManualCrop.PageID, SuggestionID: "suggestion_photo_shared001", SourceSHA256: p.Pages[0].Correction.SourceSHA256, Revision: p.Revision, IdempotencyKey: "request_photo_shared001"}
			owner, want := alice, error(nil)
			switch mode {
			case "normalized-drift":
				must(t, f.docs, "DROP TRIGGER artifact_identity_immutable")
				must(t, f.docs, "UPDATE artifacts SET sha256=zeroblob(32) WHERE id=?", photoImported().Artifact.ID)
				want = ErrUnavailable
			case "original-drift", "correction-drift":
				must(t, f.docs, "DROP TRIGGER artifact_identity_immutable")
				aid := original.Artifact.ID
				if mode == "correction-drift" {
					aid = manualArtifact().ID
				}
				must(t, f.docs, "UPDATE artifacts SET sha256=zeroblob(32) WHERE id=?", aid)
				want = ErrConflict
			case "invalid-transform":
				must(t, f.docs, "DROP TRIGGER correction_suggestion_immutable")
				must(t, f.docs, "UPDATE page_corrections SET transform_json='{}' WHERE suggestion_id=?", request.SuggestionID)
				want = ErrInvalid
			case "foreign":
				owner, want = bob, ErrUnavailable
			case "stale":
				request.Revision++
				want = ErrRevision
			case "expired":
				f.now += int64(25 * time.Hour / time.Millisecond)
				want = ErrUnavailable
			}
			applied, err := f.store.ApplyCorrection(ctx, owner, request, 24*time.Hour)
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatalf("got %v want %v", err, want)
				}
				if scalar(t, f.docs, "SELECT revision FROM documents WHERE id=?", p.ID) != int(p.Revision) {
					t.Fatal("rejected correction changed revision")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if applied.Revision != p.Revision+1 || applied.Pages[0].Correction.SuggestionID != request.SuggestionID {
				t.Fatal("suggestion not applied", applied)
			}
			replayed, err := f.store.ApplyCorrection(ctx, alice, request, 24*time.Hour)
			if err != nil || replayed.Revision != applied.Revision {
				t.Fatal("non-idempotent apply", err)
			}
			effective, err := f.store.ReadPageEffective(ctx, alice, p.ID, request.PageID)
			if err != nil || effective.Artifact.ID != manualArtifact().ID {
				t.Fatal("wrong effective pixels", err)
			}
			undo := CorrectionRequest{DocumentID: p.ID, PageID: request.PageID, Revision: applied.Revision, IdempotencyKey: "request_photo_shared_undo001"}
			if _, err = f.store.UndoCorrection(ctx, alice, undo, 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			effective, err = f.store.ReadPageEffective(ctx, alice, p.ID, request.PageID)
			if err != nil || effective.Artifact.ID != photoImported().Artifact.ID {
				t.Fatal("undo lost normalized baseline", err)
			}
			after, err := f.store.ReadPageOriginal(ctx, alice, p.ID, request.PageID)
			if err != nil || after.Artifact != original.Artifact {
				t.Fatal("original changed", err)
			}
		})
	}
}
