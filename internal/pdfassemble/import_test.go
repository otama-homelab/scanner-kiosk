package pdfassemble

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func importedTextPDF() []byte {
	content := "BT /F1 12 Tf 20 30 Td (PRESERVED_TEXT) Tj ET\n0 0 1 rg 10 10 30 40 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestBuildImportedPDFBetweenScans(t *testing.T) {
	raw := importedTextPDF()
	imported := Page{ID: "imported", MediaType: "application/pdf", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
	pages := []Page{source(t, "scan-before", "png"), imported, source(t, "scan-after", "jpeg")}
	for _, angle := range []int{0, 90} {
		pages[1].Rotation = angle
		output, err := Build(context.Background(), pages, "lossless", opts())
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil)
		if err != nil || parsed.PageCount != 3 {
			t.Fatal("mixed PDF", err)
		}
		dict, _, _, err := parsed.PageDict(context.Background(), 2, false)
		if err != nil {
			t.Fatal(err)
		}
		content, err := parsed.PageContent(dict, 2)
		if err != nil || !bytes.Contains(content, []byte("PRESERVED_TEXT")) || !bytes.Contains(content, []byte(" re f")) {
			t.Fatal("lost text/vector", err)
		}
		dims, err := api.PageDims(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration())
		w, h := 200.0, 300.0
		if angle == 90 {
			w, h = h, w
		}
		if err != nil || dims[1].Width != w || dims[1].Height != h {
			t.Fatal("lost geometry/rotation", dims, err)
		}
	}
}

func TestBuildRejectsAmbiguousImportedPage(t *testing.T) {
	raw := importedTextPDF()
	open := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	for _, p := range []Page{
		{ID: "bad", MediaType: "text/plain", Open: open},
		{ID: "bad", MediaType: "application/pdf", Size: "a4", Open: open},
		{ID: "bad", MediaType: "application/pdf", Width: 10, Height: 10, Open: open},
	} {
		if _, err := Build(context.Background(), []Page{p}, "jpeg", opts()); err == nil {
			t.Fatal("accepted ambiguous source")
		}
	}
}

func TestBuildImportedPDFLimitsAndOCRBoundary(t *testing.T) {
	ctx := context.Background()
	multiple, err := Build(ctx, []Page{source(t, "a", "png"), source(t, "b", "png")}, "jpeg", opts())
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"malformed", "multiple-pages", "input-limit", "output-limit", "cancelled", "ocr"} {
		t.Run(mode, func(t *testing.T) {
			raw := importedTextPDF()
			policy := opts()
			callCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			switch mode {
			case "malformed":
				raw = []byte("%PDF-broken")
			case "multiple-pages":
				raw = multiple
			case "input-limit":
				policy.MaxInputBytes = 10
			case "output-limit":
				policy.MaxOutputBytes = 10
			case "cancelled":
				cancel()
			}
			p := Page{ID: "import", MediaType: "application/pdf", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
			var output []byte
			if mode == "ocr" {
				p.Open = func(context.Context) (io.ReadCloser, error) {
					t.Fatal("PDF opened through image OCR boundary")
					return nil, nil
				}
				output, err = BuildWithLayers(callCtx, []Page{p}, []TextLayer{layer(p.ID, "EXTRA")}, "jpeg", policy)
			} else {
				output, err = Build(callCtx, []Page{p}, "jpeg", policy)
			}
			if err == nil || len(output) != 0 {
				t.Fatal("invalid import returned output", err)
			}
		})
	}
}

func TestBuildImportedPDFWithScannedOCRLayer(t *testing.T) {
	raw := importedTextPDF()
	imported := Page{ID: "import", MediaType: "application/pdf", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
	scan := source(t, "new-scan", "png")
	output, err := BuildWithLayers(context.Background(), []Page{imported, scan}, []TextLayer{layer(scan.ID, "NEW_SCAN_TEXT")}, "jpeg", opts())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(output), model.NewStatelessConfiguration(), nil)
	if err != nil || parsed.PageCount != 2 {
		t.Fatal("mixed searchable PDF", err)
	}
	dict, _, _, err := parsed.PageDict(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := parsed.PageContent(dict, 1)
	if err != nil || !bytes.Contains(content, []byte("PRESERVED_TEXT")) {
		t.Fatal("missing imported text", err)
	}
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("independent text extraction requires Poppler")
	}
	path := filepath.Join(t.TempDir(), "mixed.pdf")
	if err = os.WriteFile(path, output, 0600); err != nil {
		t.Fatal(err)
	}
	for i, word := range []string{"PRESERVED_TEXT", "NEW_SCAN_TEXT"} {
		page := fmt.Sprint(i + 1)
		text, err := exec.Command(tool, "-f", page, "-l", page, path, "-").CombinedOutput()
		if err != nil || !strings.Contains(string(text), word) {
			t.Fatal("missing selectable text", word, err, string(text))
		}
	}
}
