//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/pkg/ocrplugin"
)

func (r *candidateRecords) CompleteOCRResult(ctx context.Context, o, j, runner string, v int64, b documents.OCRResultBundle) error {
	r.calls++
	if r.mode == "rollback" || r.mode == "unreadable-rollback" {
		return documents.ErrCommitUncertain
	}
	err := r.Store.CompleteOCRResult(ctx, o, j, runner, v, b)
	if err == nil && (r.mode == "committed-error" || r.mode == "unreadable-commit") {
		return documents.ErrCommitUncertain
	}
	return err
}
func (r *candidateRecords) OCRResultRecorded(ctx context.Context, o, j string, b documents.OCRResultBundle) (bool, error) {
	r.reads++
	if r.mode == "unreadable-commit" || r.mode == "unreadable-rollback" {
		return false, documents.ErrStorage
	}
	return r.Store.OCRResultRecorded(ctx, o, j, b)
}
func TestReOCRBundleCommitUncertain(t *testing.T) {
	for _, mode := range []string{"committed-error", "rollback", "unreadable-commit", "unreadable-rollback"} {
		t.Run(mode, func(t *testing.T) {
			r, root, job, options, old := candidateFixture(t, ocrplugin.PositionedText)
			r.mode = mode
			r.calls = 0
			r.reads = 0
			p, e := Prepare(context.Background(), r, root, owner, job, runner, options)
			if e != nil {
				t.Fatal(e)
			}
			_, e = p.Publish(context.Background())
			if strings.HasPrefix(mode, "unreadable") {
				if !errors.Is(e, ErrPending) || !errors.Is(p.Close(), ErrPending) || !errors.Is(p.Finish(context.Background(), "interrupted"), ErrPending) {
					t.Fatal("uncertain bundle deleted", e)
				}
				for _, a := range []documents.ArtifactRecord{p.candidate.Output, p.candidate.Pages[0].Text, *p.candidate.Pages[0].Positions, *p.candidate.Pages[0].Preview, *p.candidate.Pages[0].Layer} {
					if _, e := os.Stat(filepath.Join(root, a.RelativePath)); e != nil {
						t.Fatal("pending artifact removed", e)
					}
				}
				r.mode = "success"
				_, e = p.Publish(context.Background())
			}
			success := mode == "committed-error" || mode == "unreadable-commit"
			if (e == nil) != success {
				t.Fatal("reconciliation result", e)
			}
			if r.calls != 1 {
				t.Fatal("candidate completion repeated")
			}
			if success {
				b := readCandidateBundle(t, r, job)
				if ok, e := r.Store.OCRResultRecorded(context.Background(), owner, job, b); e != nil || !ok {
					t.Fatal("whole bundle not confirmed", e)
				}
			}
			if e = p.Close(); e != nil {
				t.Fatal(e)
			}
			current, e := r.GetExport(context.Background(), owner, document)
			if e != nil || current.ID != old.ID {
				t.Fatal("prior output changed", e)
			}
		})
	}
}
func TestReOCRBundleBudgetCleanup(t *testing.T) {
	t.Run("reservation overflow", func(t *testing.T) {
		r, root, job, options, old := candidateFixture(t, ocrplugin.PositionedText)
		sqlExec(t, r.db, "UPDATE reservations SET byte_size=1000 WHERE job_id=?", job)
		recovery, e := ExecuteClaimed(context.Background(), r, root, owner, job, runner, options, time.Millisecond)
		if e == nil || recovery != nil {
			t.Fatal("overflow not terminalized", e)
		}
		var count int
		r.db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&count)
		if count != 0 {
			t.Fatal("clean overflow retained reservation")
		}
		current, e := r.GetExport(context.Background(), owner, document)
		if e != nil || current.ID != old.ID {
			t.Fatal("overflow changed old output", e)
		}
	})
	for _, mode := range []string{"cleanup failure", "cancel before commit"} {
		t.Run(mode, func(t *testing.T) {
			r, root, job, options, old := candidateFixture(t, ocrplugin.Text)
			p, e := Prepare(context.Background(), r, root, owner, job, runner, options)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "cleanup failure" {
				if e = os.Chmod(root, 0500); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { os.Chmod(root, 0700) })
				if e = p.Finish(context.Background(), "interrupted"); !errors.Is(e, artifactscope.ErrStorage) {
					t.Fatal("unconfirmed cleanup accepted", e)
				}
				var count int
				r.db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", job).Scan(&count)
				if count != 1 {
					t.Fatal("failed cleanup released reservation")
				}
				if e = os.Chmod(root, 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				if e = r.RequestCancel(context.Background(), owner, job); e != nil {
					t.Fatal(e)
				}
				if _, e = p.Publish(context.Background()); e == nil {
					t.Fatal("cancelled commit accepted")
				}
			}
			if e = p.Finish(context.Background(), "interrupted"); e != nil {
				t.Fatal("confirmed cleanup recovery failed", e)
			}
			current, e := r.GetExport(context.Background(), owner, document)
			if e != nil || current.ID != old.ID {
				t.Fatal("cleanup changed prior output", e)
			}
			for _, a := range []documents.ArtifactRecord{p.candidate.Output, p.candidate.Pages[0].Text, *p.candidate.Pages[0].Preview} {
				if _, e = os.Stat(filepath.Join(root, a.RelativePath)); !os.IsNotExist(e) {
					t.Fatal("unpublished artifact retained", e)
				}
			}
		})
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__pdf-raster" {
		os.Exit(PDFRasterChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "__pdf-assemble" {
		os.Exit(pdfassemble.AssemblyChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

func TestMixedPDFRenderWithOCRRetainsImportedText(t *testing.T) {
	for _, ocr := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "ocr"}[ocr], func(t *testing.T) {
			settings := documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}
			if ocr {
				settings.OCR = documents.RenderOCR{Enabled: true, ProviderID: "testocr", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"ja", "en"}, QualityPreset: "auto"}
			}
			records, _, root, job, options := fixtureWithPDF(t, true, settings)
			provider := &layerProvider{}
			selected := &ocrRecords{faultRecords: records, provider: provider}
			if recovery, err := ExecuteClaimed(context.Background(), selected, root, owner, job, runner, options, time.Millisecond); err != nil || recovery != nil {
				t.Fatal("mixed render failed", err)
			}
			if ocr && (len(provider.seen) != 1 || provider.seen[0].ID != "page_render_00001") {
				t.Fatal("imported PDF was sent to OCR", provider.seen)
			}
			output, err := records.GetExport(context.Background(), owner, document)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, output.RelativePath))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(data), model.NewStatelessConfiguration(), nil)
			if err != nil || parsed.PageCount != 2 {
				t.Fatal("invalid mixed output", err)
			}
			dict, _, _, err := parsed.PageDict(context.Background(), 2, false)
			if err != nil {
				t.Fatal(err)
			}
			content, err := parsed.PageContent(dict, 2)
			if err != nil || !bytes.Contains(content, []byte("OCR RESULT")) {
				t.Fatal("imported text lost", err)
			}
		})
	}
}
