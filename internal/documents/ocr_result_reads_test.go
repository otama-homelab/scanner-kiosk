package documents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReOCRResultReads(t *testing.T) {
	f, b := completedOCRCandidate(t)
	p, err := f.store.ReadDocument(context.Background(), alice, docA)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["ocr_results"] == nil {
		t.Fatal("candidate result identity unavailable in document projection", b.ID)
	}
	result, err := f.store.ReadOCRResult(context.Background(), alice, docA, b.ID)
	if err != nil || len(result.Pages) != 2 || result.Settings.OCR.ModelID != b.Settings.OCR.ModelID {
		t.Fatal(result, err)
	}
	raw, _ = json.Marshal(result)
	if strings.Contains(string(raw), "result/text_") || strings.Contains(string(raw), "relative_path") {
		t.Fatal("storage path exposed", string(raw))
	}
	ref, err := f.store.ReadOCRResultArtifact(context.Background(), alice, docA, b.ID, b.Pages[0].PageID, "text")
	if err != nil || ref.Artifact.ID != b.Pages[0].Text.ID {
		t.Fatal(ref, err)
	}
	must(t, f.docs, `UPDATE pages SET rotation=90 WHERE document_id=?`, docA)
	result, err = f.store.ReadOCRResult(context.Background(), alice, docA, b.ID)
	if err != nil || result.Pages[0].Rotation != 0 {
		t.Fatal("history relabelled after edit", result, err)
	}
}

func TestReOCRResultCrossOwner(t *testing.T) {
	for _, mode := range []string{"foreign", "disabled", "expired", "expires during read", "artifact"} {
		t.Run(mode, func(t *testing.T) {
			f, b := completedOCRCandidate(t)
			owner := alice
			kind := "text"
			switch mode {
			case "foreign":
				owner = bob
			case "disabled":
				must(t, f.state, `UPDATE principals SET disabled_at_ms=2000 WHERE id=?`, alice)
			case "expired":
				f.now = 10000
			case "expires during read":
				calls := 0
				f.store.now = func() time.Time {
					calls++
					if calls > 1 {
						return time.UnixMilli(10000)
					}
					return time.UnixMilli(2000)
				}
			case "artifact":
				kind = "unknown"
			}
			if mode != "artifact" {
				if _, err := f.store.ReadOCRResult(context.Background(), owner, docA, b.ID); err == nil {
					t.Fatal("unauthorized metadata")
				}
			}
			if _, err := f.store.ReadOCRResultArtifact(context.Background(), owner, docA, b.ID, b.Pages[0].PageID, kind); err == nil {
				t.Fatal("unauthorized artifact")
			}
		})
	}
}

func TestReOCRDownloadDuringCandidate(t *testing.T) {
	f, b := completedOCRCandidate(t)
	ctx := context.Background()
	p, err := f.store.ReadDocument(ctx, alice, docA)
	if err != nil || p.Output == nil || p.ReadyRevision == nil {
		t.Fatal("candidate hid prior output", p, err)
	}
	if _, err := f.store.AdoptOCRResult(ctx, alice, ocrResultRequest(b), time.Hour); err != nil {
		t.Fatal(err)
	}
	ref, err := f.store.ReadCurrentOutput(ctx, alice, docA)
	if err != nil || ref.Artifact.ID != b.Output.ID {
		t.Fatal("adopted binding unavailable", ref, err)
	}
	if _, err = f.docs.Exec(`UPDATE published_outputs SET artifact_id='artifact_prior_output_001' WHERE document_id=?`, docA); err == nil {
		t.Fatal("ordinary stale output accepted")
	}
}
