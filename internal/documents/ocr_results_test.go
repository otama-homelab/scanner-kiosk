package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func ocrCandidateFixture(t *testing.T) (*fixture, OCRResultBundle) {
	t.Helper()
	f := setup(t)
	// Replace the queued job so its immutable input is a real OCR snapshot.
	must(t, f.docs, `DELETE FROM jobs WHERE id=?`, jobA)
	settings := RenderSettings{Format: "pdf", PDFEncoding: "jpeg", OCR: RenderOCR{Enabled: true, ProviderID: "provider_local_001", ModelID: "model_local_00001", ModelVersion: "v1", Languages: []string{"ja"}}}
	b := OCRResultBundle{ID: "ocr_result_candidate_001", JobID: jobA, InputRevision: 1, Settings: settings, Output: ArtifactRecord(output())}
	snapshot := RenderSnapshot{Version: 1, Settings: settings}
	for i, suffix := range []string{"a", "b"} {
		original := "artifact_original_000" + suffix
		page := "page_original_000" + suffix
		hash := sha256.Sum256([]byte(original))
		must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',20,?,1,1000)`, original, docA, alice, "original/"+suffix, hash[:])
		must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES(?,?,?,?,?,'a4','color',300,100,200)`, page, docA, alice, i, original)
		text := ArtifactRecord{ID: "artifact_transcript_00" + suffix, RelativePath: "result/text_" + suffix, MediaType: "text/plain", Size: 4, SHA256: sha256.Sum256([]byte("text"))}
		b.Pages = append(b.Pages, OCRResultPage{PageID: page, OriginalArtifactID: original, EffectiveArtifactID: original, OriginalSHA256: hash, EffectiveSHA256: hash, PixelWidth: 100, PixelHeight: 200, PDFTextOrigin: "recognition", Text: text})
		snapshot.Pages = append(snapshot.Pages, CapturedPage{ID: page, OriginalArtifactID: original, Size: "a4", Color: "color", DPI: 300})
	}
	raw, _ := json.Marshal(snapshot)
	must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',1,?,1500,9000)`, jobA, docA, alice, string(raw))
	must(t, f.docs, `INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,100)`, jobA, alice)
	old := output()
	old.ID = "artifact_prior_output_001"
	old.RelativePath = "prior/output"
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'output','ready',?,'application/pdf',3,?,1,1000)`, old.ID, docA, alice, old.RelativePath, old.SHA256[:])
	must(t, f.docs, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,?)`, docA, alice, old.ID)
	claim(t, f)
	return f, b
}

func TestReOCRCandidateCompletion(t *testing.T) {
	f, b := ocrCandidateFixture(t)
	ctx := context.Background()
	if err := f.store.CompleteOCRResult(ctx, alice, jobA, runner, 1, b); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE state='candidate'`) != 1 || scalar(t, f.docs, `SELECT count(*) FROM reservations`) != 0 || scalar(t, f.docs, `SELECT count(*) FROM jobs WHERE state='succeeded'`) != 1 {
		t.Fatal("candidate not atomic")
	}
	ref, err := f.store.ReadCurrentOutput(ctx, alice, docA)
	if err != nil || ref.Artifact.ID != "artifact_prior_output_001" {
		t.Fatal("completion replaced prior output", err)
	}
	if ok, err := f.store.OCRResultRecorded(ctx, alice, jobA, b); err != nil || !ok {
		t.Fatal("complete bundle not reconciled", err)
	}
	if err := f.store.CompleteOCRResult(ctx, alice, jobA, runner, 1, b); !errors.Is(err, ErrConflict) {
		t.Fatal("reservation consumed twice", err)
	}
}

func TestReOCRCandidateBundleAtomicity(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change func(*fixture, *OCRResultBundle)
	}{
		{"missing page", func(_ *fixture, b *OCRResultBundle) { b.Pages = b.Pages[:1] }},
		{"order", func(_ *fixture, b *OCRResultBundle) { b.Pages[0], b.Pages[1] = b.Pages[1], b.Pages[0] }},
		{"source hash", func(_ *fixture, b *OCRResultBundle) { b.Pages[0].OriginalSHA256 = sha256.Sum256([]byte("foreign")) }},
		{"model", func(_ *fixture, b *OCRResultBundle) { b.Settings.OCR.ModelVersion = "other" }},
		{"reuse input", func(_ *fixture, b *OCRResultBundle) { b.Pages[0].Text.ID = b.Pages[0].OriginalArtifactID }},
		{"duplicate", func(_ *fixture, b *OCRResultBundle) { b.Pages[1].Text = b.Pages[0].Text }},
		{"budget", func(f *fixture, _ *OCRResultBundle) { must(t, f.docs, `UPDATE reservations SET byte_size=10`) }},
		{"cancel", func(f *fixture, _ *OCRResultBundle) {
			must(t, f.docs, `UPDATE jobs SET state='cancelling' WHERE id=?`, jobA)
		}},
		{"disabled", func(f *fixture, _ *OCRResultBundle) {
			must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, b := ocrCandidateFixture(t)
			tc.change(f, &b)
			if err := f.store.CompleteOCRResult(ctx, alice, jobA, runner, 1, b); err == nil {
				t.Fatal("accepted malformed bundle")
			}
			if scalar(t, f.docs, `SELECT count(*) FROM ocr_results`) != 0 || scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE id=?`, output().ID) != 0 || scalar(t, f.docs, `SELECT count(*) FROM reservations`) != 1 {
				t.Fatal("partial candidate stored")
			}
		})
	}
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit rollback", true: "lost commit response"}[committed], func(t *testing.T) {
			f, b := ocrCandidateFixture(t)
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost commit response")
			}
			if err := f.store.CompleteOCRResult(ctx, alice, jobA, runner, 1, b); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			ok, err := f.store.OCRResultRecorded(ctx, alice, jobA, b)
			if err != nil || ok != committed {
				t.Fatal("unsafe reconciliation", ok, err)
			}
			if committed {
				b.Pages[0].Text.Size++
				if _, err := f.store.OCRResultRecorded(ctx, alice, jobA, b); !errors.Is(err, ErrConflict) {
					t.Fatal("partial identity accepted", err)
				}
			}
		})
	}
	t.Run("near commit expiry", func(t *testing.T) {
		f, b := ocrCandidateFixture(t)
		calls := 0
		f.store.now = func() time.Time {
			calls++
			if calls > 1 {
				return time.UnixMilli(10000)
			}
			return time.UnixMilli(2000)
		}
		if err := f.store.CompleteOCRResult(ctx, alice, jobA, runner, 1, b); err == nil {
			t.Fatal("expired near commit accepted")
		}
		if scalar(t, f.docs, `SELECT count(*) FROM ocr_results`) != 0 {
			t.Fatal("expired bundle retained")
		}
	})
}
