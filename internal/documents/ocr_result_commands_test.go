package documents

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestReOCRAdoptWholeDocument(t *testing.T) {
	t.Run("binding", testReOCRAdoptionBinding)
	f, b := completedOCRCandidate(t)
	request := ocrResultRequest(b)
	result, err := f.store.AdoptOCRResult(context.Background(), alice, request, time.Hour)
	if err != nil || result.Revision != 2 || result.Output == nil || len(result.Pages) != 2 {
		t.Fatal("whole adoption failed", result, err)
	}
	ref, err := f.store.ReadCurrentOutput(context.Background(), alice, docA)
	if err != nil || ref.Artifact.ID != b.Output.ID {
		t.Fatal("adopted output not current", err)
	}
	if scalar(t, f.docs, `SELECT input_revision FROM artifacts WHERE id=?`, b.Output.ID) != 1 || scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE id='artifact_prior_output_001' AND state='ready'`) != 1 || scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE state='adopted' AND adopted_revision=2`) != 1 {
		t.Fatal("adoption rewrote immutable provenance or deleted prior")
	}
	if _, err = f.docs.Exec(`UPDATE published_outputs SET artifact_id='artifact_prior_output_001' WHERE document_id=?`, docA); err == nil {
		t.Fatal("arbitrary stale output accepted")
	}
}

func testReOCRAdoptionBinding(t *testing.T) {
	f, b := ocrCandidateFixture(t)
	if err := f.store.CompleteOCRResult(context.Background(), alice, jobA, runner, 1, b); err != nil {
		t.Fatal(err)
	}
	// A durable explicit whole-bundle disposition must authorize only this
	// output at the next revision, without mutating its input provenance.
	tx, err := f.docs.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM published_outputs WHERE document_id='document_alice_001'`,
		`UPDATE documents SET revision=2 WHERE id='document_alice_001'`,
		`UPDATE ocr_results SET state='adopted',adopted_revision=2 WHERE id='ocr_result_candidate_001'`,
		`INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES('document_alice_001','principal_alice_001',2,'artifact_export_0001')`,
	} {
		if _, err = tx.Exec(q); err != nil {
			t.Fatal("explicit whole-document adoption rejected", err)
		}
	}
}

func completedOCRCandidate(t *testing.T) (*fixture, OCRResultBundle) {
	t.Helper()
	f, b := ocrCandidateFixture(t)
	if err := f.store.CompleteOCRResult(context.Background(), alice, jobA, runner, 1, b); err != nil {
		t.Fatal(err)
	}
	return f, b
}
func ocrResultRequest(b OCRResultBundle) OCRResultRequest {
	return OCRResultRequest{DocumentID: docA, ResultID: b.ID, Revision: 1, IdempotencyKey: "request_adopt_ocr_001"}
}

func TestReOCRDiscardKeepsPrior(t *testing.T) {
	f, b := completedOCRCandidate(t)
	ctx := context.Background()
	request := ocrResultRequest(b)
	result, err := f.store.DiscardOCRResult(ctx, alice, request, time.Hour)
	if err != nil || result.Revision != 1 {
		t.Fatal(result, err)
	}
	ref, err := f.store.ReadCurrentOutput(ctx, alice, docA)
	if err != nil || ref.Artifact.ID != "artifact_prior_output_001" {
		t.Fatal("discard lost prior output", err)
	}
	if scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE state='discarded'`) != 1 || scalar(t, f.docs, `SELECT count(*) FROM artifacts WHERE id=?`, b.Output.ID) != 1 {
		t.Fatal("discard reclaimed files outside cleanup")
	}
	if _, err = f.store.DiscardOCRResult(ctx, alice, request, time.Hour); err != nil {
		t.Fatal("discard replay failed", err)
	}
	request.IdempotencyKey = "request_adopt_discarded_001"
	if _, err = f.store.AdoptOCRResult(ctx, alice, request, time.Hour); !errors.Is(err, ErrConflict) {
		t.Fatal("adopted discarded result", err)
	}
}

func TestReOCRAdoptReplayAndStale(t *testing.T) {
	ctx := context.Background()
	t.Run("receipt first", func(t *testing.T) {
		f, b := completedOCRCandidate(t)
		r := ocrResultRequest(b)
		first, err := f.store.AdoptOCRResult(ctx, alice, r, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		again, err := f.store.AdoptOCRResult(ctx, alice, r, time.Hour)
		if err != nil || again.Revision != first.Revision || scalar(t, f.docs, `SELECT revision FROM documents WHERE id=?`, docA) != 2 {
			t.Fatal("duplicate revision", err)
		}
		r.ResultID = "other_ocr_result_0001"
		if _, err = f.store.AdoptOCRResult(ctx, alice, r, time.Hour); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatal("wrong payload replay", err)
		}
	})
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback replay", true: "lost response replay"}[committed], func(t *testing.T) {
			f, b := completedOCRCandidate(t)
			r := ocrResultRequest(b)
			original := f.store.commit
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("lost response")
			}
			if _, err := f.store.AdoptOCRResult(ctx, alice, r, time.Hour); !errors.Is(err, ErrCommitUncertain) {
				t.Fatal(err)
			}
			f.store.commit = original
			result, err := f.store.AdoptOCRResult(ctx, alice, r, time.Hour)
			if err != nil || result.Revision != 2 || scalar(t, f.docs, `SELECT count(*) FROM request_receipts`) != 1 {
				t.Fatal("uncertain replay duplicated result", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*fixture, *OCRResultRequest)
		owner  string
	}{
		{"revision", func(_ *fixture, r *OCRResultRequest) { r.Revision = 2 }, alice},
		{"foreign owner", func(_ *fixture, _ *OCRResultRequest) {}, bob},
		{"disabled", func(f *fixture, _ *OCRResultRequest) {
			must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
		}, alice},
		{"expired", func(f *fixture, _ *OCRResultRequest) { f.now = 10000 }, alice},
		{"source rotation", func(f *fixture, _ *OCRResultRequest) {
			must(t, f.docs, `UPDATE pages SET rotation=90 WHERE ordinal=0 AND document_id=?`, docA)
		}, alice},
		{"source order", func(f *fixture, _ *OCRResultRequest) {
			must(t, f.docs, `UPDATE pages SET ordinal=ordinal+10 WHERE document_id=?`, docA)
			must(t, f.docs, `UPDATE pages SET ordinal=11-ordinal WHERE document_id=?`, docA)
		}, alice},
		{"active work", func(f *fixture, _ *OCRResultRequest) {
			must(t, f.docs, `INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('render_active_job_002',?,?,'render','queued',1,'{}',2000,9000)`, docA, alice)
		}, alice},
		{"missing candidate", func(_ *fixture, r *OCRResultRequest) { r.ResultID = "missing_result_0001" }, alice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, b := completedOCRCandidate(t)
			r := ocrResultRequest(b)
			tc.mutate(f, &r)
			if _, err := f.store.AdoptOCRResult(ctx, tc.owner, r, time.Hour); err == nil {
				t.Fatal("unsafe adoption succeeded")
			}
			if scalar(t, f.docs, `SELECT revision FROM documents WHERE id=?`, docA) != 1 || scalar(t, f.docs, `SELECT count(*) FROM ocr_results WHERE state='candidate'`) != 1 {
				t.Fatal("failed adoption mutated candidate")
			}
		})
	}
}
