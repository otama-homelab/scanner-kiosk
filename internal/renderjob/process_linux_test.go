//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
)

func TestMain(m *testing.M) {
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
