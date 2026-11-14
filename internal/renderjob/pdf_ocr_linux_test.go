//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/pkg/ocrplugin"
	"strings"
	"testing"
	"time"
)

func (*candidateRecords) PDFRasterCommand() PDFRasterCommand {
	root := os.Getenv("SCANNER_TEST_PDF_RUNTIME")
	if root == "" {
		return PDFRasterCommand{}
	}
	return PDFRasterCommand{Path: filepath.Join(root, "lib/ld-musl-x86_64.so.1"), Args: []string{"--library-path", filepath.Join(root, "lib") + ":" + filepath.Join(root, "usr/lib"), filepath.Join(root, "usr/bin/pdftoppm")}}
}

func pdfCandidateFixture(t *testing.T, supplied ...[]byte) (*candidateRecords, string, string, pdfassemble.Options, documents.Export) {
	t.Helper()
	runtimeRoot := os.Getenv("SCANNER_TEST_PDF_RUNTIME")
	binary, e := os.ReadFile(filepath.Join(runtimeRoot, "usr/bin/pdftoppm"))
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(binary)) != "e969e55d7e2229f1e438284f9e8cfd666313e45a9b7b6265ccbf8b4ade5f2070" {
		t.Fatal("verified official Poppler runtime required: set SCANNER_TEST_PDF_RUNTIME")
	}
	source := invisibleLayer()
	if len(supplied) > 0 {
		source = supplied[0]
	}
	r, db, root, job, o := fixtureWithPDFData(t, true, source)
	p, e := Prepare(context.Background(), r, root, owner, job, runner, o)
	if e != nil {
		t.Fatal(e)
	}
	old, e := p.Publish(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	a, e := r.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 2, IdempotencyKey: "request_pdfocr_0001", ReOCR: true, ResolvedOutput: ocrplugin.PositionedText, Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: "testocr", ModelID: "installed", ModelVersion: "test-version", Languages: []string{"en"}, QualityPreset: "auto"}}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 4 << 20, GlobalMaxBytes: 8 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.ClaimExport(context.Background(), owner, a.JobID, runner); e != nil {
		t.Fatal(e)
	}
	o.MaxPagePixels = 1000000
	return &candidateRecords{faultRecords: r, provider: &candidateProvider{kind: ocrplugin.PositionedText}, db: db}, root, a.JobID, o, old
}

func vectorPDF() []byte {
	content := "0 0 1 rg 10 10 40 60 re f"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 80 100] /CropBox [10 10 70 90] /Rotate 90 /Resources << >> /Contents 4 0 R >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, o := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestReOCRMixedDocument(t *testing.T) {
	r, root, job, o, old := pdfCandidateFixture(t, vectorPDF())
	p, e := Prepare(context.Background(), r, root, owner, job, runner, o)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if _, e = p.Publish(context.Background()); e != nil {
		t.Fatal(e)
	}
	b := readCandidateBundle(t, r, job)
	page := b.Pages[1]
	if page.PDFTextOrigin != "recognition" || page.PDFRaster == nil || page.PDFRaster.SourceRotation != 90 || page.PixelWidth != 250 || page.PixelHeight != 334 || page.PDFRaster.CropBox != ([4]float64{10, 10, 70, 90}) {
		t.Fatalf("intrinsic rotation/crop mapping: %+v", page)
	}
	output := filepath.Join(root, b.Output.RelativePath)
	text, e := exec.Command("pdftotext", output, "-").Output()
	if e != nil || strings.Count(string(text), "REAL OCR TEXT") != 2 {
		t.Fatalf("image-only PDF did not receive searchable text: %v %s", e, text)
	}
	var renders [][]byte
	for _, path := range []string{filepath.Join(root, old.RelativePath), output} {
		pixels, e := exec.Command("pdftoppm", "-f", "2", "-l", "2", "-singlefile", "-r", "72", "-png", path).Output()
		if e != nil {
			t.Fatal(e)
		}
		renders = append(renders, pixels)
	}
	if !bytes.Equal(renders[0], renders[1]) {
		t.Fatal("vector content/crop/rotation changed")
	}
}

func TestReOCRPDFOnlyDocument(t *testing.T) {
	for _, mode := range []string{"cancel", "source-hash"} {
		t.Run(mode, func(t *testing.T) {
			r, root, job, o, old := pdfCandidateFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			} else {
				if e := os.WriteFile(filepath.Join(root, "imported.pdf"), []byte("corrupt"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			p, e := Prepare(ctx, r, root, owner, job, runner, o)
			if e == nil {
				p.Close()
				t.Fatal("failed source/cancel accepted")
			}
			current, e := r.GetExport(context.Background(), owner, document)
			if e != nil || current.ID != old.ID {
				t.Fatal("failed/cancelled recognition replaced output", e)
			}
		})
	}
}

type wrongPDFGeometryRecords struct{ *candidateRecords }

func (r wrongPDFGeometryRecords) LoadRenderWork(ctx context.Context, owner, job, runner string) (documents.RenderWork, error) {
	w, e := r.candidateRecords.LoadRenderWork(ctx, owner, job, runner)
	if e == nil {
		w.Snapshot.Pages[1].PDFWidthPoints++
	}
	return w, e
}

func TestReOCRPDFExistingTextPolicy(t *testing.T) {
	r, root, job, o, old := pdfCandidateFixture(t)
	p, e := Prepare(context.Background(), wrongPDFGeometryRecords{r}, root, owner, job, runner, o)
	if e == nil {
		p.Close()
		t.Fatal("actual PDF geometry must bind the admitted source geometry")
	}
	current, e := r.GetExport(context.Background(), owner, document)
	if e != nil || current.ID != old.ID {
		t.Fatal("geometry failure replaced output", e)
	}
}

func TestReOCRPDFRaster(t *testing.T) {
	r, root, job, o, _ := pdfCandidateFixture(t)
	p, e := Prepare(context.Background(), r, root, owner, job, runner, o)
	if e != nil {
		t.Fatalf("imported PDF recognition raster must prepare: %v", e)
	}
	defer p.Close()
	if _, e = p.Publish(context.Background()); e != nil {
		t.Fatal(e)
	}
	b := readCandidateBundle(t, r, job)
	if len(b.Pages) != 2 || b.Pages[1].SourceType != "pdf" || b.Pages[1].PixelWidth != 334 || b.Pages[1].PixelHeight != 417 {
		t.Fatalf("300dpi source mapping: %+v", b.Pages)
	}
}

func TestReOCRPDFRasterLimits(t *testing.T) {
	r, root, job, o, old := pdfCandidateFixture(t)
	// A successful real raster establishes that rejection below is a pixel policy,
	// rather than an unavailable executable or an unsupported PDF route.
	p, e := Prepare(context.Background(), r, root, owner, job, runner, o)
	if e != nil {
		t.Fatalf("real raster baseline: %v", e)
	}
	if e = p.Close(); e != nil {
		t.Fatal(e)
	}
	o.MaxPagePixels = 100000
	p, e = Prepare(context.Background(), r, root, owner, job, runner, o)
	if e == nil {
		p.Close()
		t.Fatal("over-policy 300dpi page accepted")
	}
	current, e := r.GetExport(context.Background(), owner, document)
	if e != nil || current.ID != old.ID {
		t.Fatal("failed raster replaced old output", e)
	}
}

func TestReOCRImportedPDFSupplementRetainsSourceText(t *testing.T) {
	records, root, job, options, _ := pdfCandidateFixture(t)
	records.provider.supplement = "Page layout interpretation."
	prepared, err := Prepare(context.Background(), records, root, owner, job, runner, options)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err = prepared.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	bundle := readCandidateBundle(t, records, job)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "pdftotext", "-raw", filepath.Join(root, bundle.Output.RelativePath), "-")
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	if strings.Count(text, "OCR RESULT") != 1 || strings.Count(text, "REAL OCR TEXT") != 1 || strings.Count(text, "Page layout interpretation.") != 2 {
		t.Fatalf("original retained PDF text or per-page supplement lost/duplicated: %q", text)
	}
}
