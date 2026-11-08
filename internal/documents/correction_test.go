package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"scanner-kiosk/internal/correction"
)

const correctionPage = "page_correction_0001"

func correctionFixture(t *testing.T) (*fixture, CorrectionRequest, string) {
	t.Helper()
	f := setup(t)
	must(t, f.docs, "DELETE FROM reservations")
	must(t, f.docs, "DELETE FROM jobs")
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docA)
	original := sha256.Sum256([]byte("original"))
	source := hex.EncodeToString(original[:])
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('artifact_correction_original','document_alice_001',?,'original','ready','correction-original.png','image/png',20,?,1,1000)`, alice, original[:])
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES(?,'document_alice_001',?,0,'artifact_correction_original','a4','color',300,960,640)`, correctionPage, alice)
	insertCorrectionSuggestion(t, f, "suggestion_correction_001", "artifact_correction_one", source)
	insertCorrectionSuggestion(t, f, "suggestion_correction_002", "artifact_correction_two", source)
	return f, CorrectionRequest{
		DocumentID: docA, PageID: correctionPage, SuggestionID: "suggestion_correction_001",
		SourceSHA256: source, Revision: 1, IdempotencyKey: "request_correction_apply_001",
	}, source
}

func insertCorrectionSuggestion(t *testing.T, f *fixture, suggestion, artifact, source string) {
	t.Helper()
	digest := sha256.Sum256([]byte(artifact))
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,'document_alice_001',?,'intermediate','ready',?,'image/png',20,?,1,1000)`, artifact, alice, artifact+".png", digest[:])
	transform := correction.Transform{
		CropQuad:    [4]correction.Point{{X: 0, Y: 0}, {X: 960, Y: 0}, {X: 960, Y: 640}, {X: 0, Y: 640}},
		OutputWidth: 960, OutputHeight: 640, ConfidenceKind: "unavailable",
	}
	body, err := json.Marshal(transform)
	if err != nil {
		t.Fatal(err)
	}
	must(t, f.docs, `INSERT INTO page_corrections(suggestion_id,document_id,owner_id,page_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json,confidence_kind,confidence_value,risk_flags,state,created_at_ms) VALUES(?,'document_alice_001',?,?,'artifact_correction_original',?,?,?,?,'unavailable',NULL,'[]','suggested',1500)`, suggestion, alice, correctionPage, artifact, mustHash(t, source), digest[:], string(body))
}

func mustHash(t *testing.T, value string) []byte {
	t.Helper()
	body, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCorrectionApplyUndo(t *testing.T) {
	f, request, _ := correctionFixture(t)
	ctx := context.Background()
	applied, err := f.store.ApplyCorrection(ctx, alice, request, time.Hour)
	if err != nil || applied.Revision != 2 || len(applied.Pages) != 1 || applied.Pages[0].Correction == nil ||
		applied.Pages[0].Correction.State != "applied" || !applied.Pages[0].Correction.CanUndo ||
		applied.Pages[0].PreviewURL == "" {
		t.Fatalf("apply failed: %+v %v", applied, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM page_corrections WHERE state='applied'") != 1 {
		t.Fatal("wrong applied count")
	}
	if got := scalar(t, f.docs, "SELECT count(*) FROM pages WHERE id=? AND original_artifact_id='artifact_correction_original'", correctionPage); got != 1 {
		t.Fatal("original page source changed")
	}
	replay, err := f.store.ApplyCorrection(ctx, alice, request, time.Hour)
	if err != nil || replay.Revision != applied.Revision || replay.Pages[0].Correction == nil || replay.Pages[0].Correction.CorrectionArtifactID != "artifact_correction_one" {
		t.Fatalf("apply replay failed: %+v %v", replay, err)
	}
	undo := CorrectionRequest{DocumentID: docA, PageID: correctionPage, Revision: 2, IdempotencyKey: "request_correction_undo_001"}
	undone, err := f.store.UndoCorrection(ctx, alice, undo, time.Hour)
	if err != nil || undone.Revision != 3 || undone.Pages[0].Correction == nil || undone.Pages[0].Correction.State != "suggested" {
		t.Fatalf("undo failed: %+v %v", undone, err)
	}
	reference, err := f.store.ReadCorrectionArtifact(ctx, alice, docA, correctionPage)
	if err != nil || reference.Artifact.MediaType != "image/png" || reference.Artifact.ID == "" || reference.ExpiresAtMS <= 0 {
		t.Fatalf("correction artifact read failed: %+v %v", reference, err)
	}
}

func TestCorrectionRejectsStaleOrForeign(t *testing.T) {
	f, request, _ := correctionFixture(t)
	ctx := context.Background()
	foreign := request
	if _, err := f.store.ApplyCorrection(ctx, bob, foreign, time.Hour); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("foreign owner accepted: %v", err)
	}
	stale := request
	stale.Revision = 2
	if _, err := f.store.ApplyCorrection(ctx, alice, stale, time.Hour); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	wrongSource := request
	wrongSource.SourceSHA256 = hex.EncodeToString(make([]byte, 32))
	if _, err := f.store.ApplyCorrection(ctx, alice, wrongSource, time.Hour); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong source accepted: %v", err)
	}
}

func TestCorrectionReprocessKeepsOriginal(t *testing.T) {
	f, request, source := correctionFixture(t)
	ctx := context.Background()
	if _, err := f.store.ApplyCorrection(ctx, alice, request, time.Hour); err != nil {
		t.Fatal(err)
	}
	second := CorrectionRequest{
		DocumentID: docA, PageID: correctionPage, SuggestionID: "suggestion_correction_002",
		SourceSHA256: source, Revision: 2, IdempotencyKey: "request_correction_apply_002",
	}
	result, err := f.store.ApplyCorrection(ctx, alice, second, time.Hour)
	if err != nil || result.Revision != 3 || result.Pages[0].Correction == nil ||
		result.Pages[0].Correction.CorrectionArtifactID != "artifact_correction_two" {
		t.Fatalf("reprocess failed: %+v %v", result, err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM page_corrections WHERE state='applied'") != 1 ||
		scalar(t, f.docs, "SELECT count(*) FROM page_corrections WHERE state='discarded'") != 1 {
		t.Fatal("reprocess did not preserve one applied suggestion")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM pages WHERE id=? AND original_artifact_id='artifact_correction_original'", correctionPage) != 1 {
		t.Fatal("reprocess changed original")
	}
}
