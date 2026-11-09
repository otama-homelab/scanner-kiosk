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
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func fixtureLayer(text string) []byte {
	content := "BT /F1 8 Tf 3 Tr 10 50 Td (" + text + ") Tj ET"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 80 100] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func importedOCRFixture(width, height float64, content, extra string) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.8f %.8f] %s /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", width, height, extra), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func pdfPage(id string, raw []byte) Page {
	return Page{ID: id, MediaType: "application/pdf", Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
}

func pdfLayer(id string, width, height float64, text string) TextLayer {
	raw := importedOCRFixture(width, height, "BT /F1 12 Tf 3 Tr 20 30 Td ("+text+") Tj ET", "")
	return TextLayer{PageID: id, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }}
}

func extractedOCRText(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output.pdf")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-").Output()
	if err != nil {
		t.Fatal("required independent extraction", err)
	}
	return string(data)
}

func TestOCRImportedPDFLayer(t *testing.T) {
	ctx := context.Background()
	raw := importedOCRFixture(200, 300, "0 0 1 rg 10 10 30 40 re f", "")
	page := pdfPage("pdf", raw)
	output, err := BuildWithLayers(ctx, []Page{page}, []TextLayer{pdfLayer(page.ID, 200, 300, "NEW_OCR")}, "lossless", opts())
	if err != nil {
		t.Fatalf("imported PDF layer rejected: %v", err)
	}
	if got := extractedOCRText(t, output); !strings.Contains(got, "NEW_OCR") {
		t.Fatal("layer text lost", got)
	}
	parsed, err := api.ReadValidateAndOptimize(ctx, bytes.NewReader(output), model.NewStatelessConfiguration(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dict, _, _, err := parsed.PageDict(ctx, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := parsed.PageContent(dict, 1)
	if err != nil || !bytes.Contains(content, []byte(" re f")) {
		t.Fatal("source vector lost", err)
	}
	for _, bad := range []TextLayer{pdfLayer("foreign", 200, 300, "BAD"), pdfLayer(page.ID, 300, 200, "BAD")} {
		if data, err := BuildWithLayers(ctx, []Page{page}, []TextLayer{bad}, "lossless", opts()); err == nil || len(data) > 0 {
			t.Fatal("foreign/incorrect geometry layer accepted")
		}
	}
}

func TestOCRImportedPDFRotation(t *testing.T) {
	ctx := context.Background()
	for _, intrinsic := range []int{0, 90, 180, 270} {
		for _, display := range []int{0, 90, 180, 270} {
			t.Run(fmt.Sprintf("%d-%d", intrinsic, display), func(t *testing.T) {
				raw := importedOCRFixture(200, 300, "0 0 1 rg 10 10 30 40 re f", fmt.Sprintf("/Rotate %d", intrinsic))
				page := pdfPage("pdf", raw)
				page.Rotation = display
				scan := source(t, "scan", "png")
				output, err := BuildWithLayers(ctx, []Page{scan, page}, []TextLayer{pdfLayer(page.ID, 200, 300, "PDF_OCR"), layer(scan.ID, "SCAN_OCR")}, "lossless", opts())
				if err != nil {
					t.Fatal(err)
				}
				boxes, err := api.Boxes(ctx, bytes.NewReader(output), nil, model.NewStatelessConfiguration())
				if err != nil || len(boxes) != 2 {
					t.Fatal("mixed page count", err)
				}
				if ((boxes[1].Rot%360)+360)%360 != (intrinsic+display)%360 || boxes[1].MediaBox().Width() != 200 || boxes[1].MediaBox().Height() != 300 {
					t.Fatal("source rotation/geometry changed", boxes[1])
				}
				if got := extractedOCRText(t, output); !strings.Contains(got, "PDF_OCR") || !strings.Contains(got, "SCAN_OCR") {
					t.Fatal("mixed OCR missing", got)
				}
			})
		}
	}
}
func layer(id, text string) TextLayer {
	return TextLayer{PageID: id, Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(fixtureLayer(text))), nil
	}}
}
func TestOCRLayerBindingsAndRotations(t *testing.T) {
	a, b := source(t, "alpha", "jpeg"), source(t, "beta", "png")
	a.Rotation = 90
	b.Rotation = 180
	b.Size = "a5"
	for _, encoding := range []string{"jpeg", "lossless"} {
		if output, err := BuildWithLayers(context.Background(), []Page{b, a}, []TextLayer{layer(a.ID, "ALPHA ONE"), layer(b.ID, "BETA TWO")}, encoding, opts()); err != nil || len(output) == 0 {
			t.Fatalf("layered build: %v", err)
		}
	}
}
func TestOCRRejectsInvalidResultSetBeforeInput(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate-layer", "foreign-layer", "duplicate-page"} {
		t.Run(mode, func(t *testing.T) {
			a, b := source(t, "alpha", "png"), source(t, "beta", "png")
			a.Open = func(context.Context) (io.ReadCloser, error) { t.Fatal("invalid set opened original"); return nil, nil }
			b.Open = a.Open
			pages := []Page{a, b}
			layers := []TextLayer{layer(a.ID, "ALPHA"), layer(b.ID, "BETA")}
			switch mode {
			case "missing":
				layers = layers[:1]
			case "duplicate-layer":
				layers[1] = layers[0]
			case "foreign-layer":
				layers[1] = layer("foreign", "FOREIGN")
			case "duplicate-page":
				pages[1] = pages[0]
			}
			if output, err := BuildWithLayers(context.Background(), pages, layers, "lossless", opts()); err == nil || len(output) > 0 {
				t.Fatal("invalid result set accepted")
			}
		})
	}
}

func TestOCRBudgetNeverReturnsPartialPDF(t *testing.T) {
	a, b := source(t, "alpha", "png"), source(t, "beta", "png")
	policy := opts()
	single, err := BuildWithLayers(context.Background(), []Page{a}, []TextLayer{layer(a.ID, "ALPHA ONE")}, "lossless", policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.MaxOutputBytes = int64(len(single) + 128)
	if _, err = BuildWithLayers(context.Background(), []Page{a}, []TextLayer{layer(a.ID, "ALPHA ONE")}, "lossless", policy); err != nil {
		t.Fatal("one page must fit the fixture budget")
	}
	if output, err := BuildWithLayers(context.Background(), []Page{a, b}, []TextLayer{layer(a.ID, "ALPHA ONE"), layer(b.ID, "BETA TWO")}, "lossless", policy); err == nil || len(output) > 0 {
		t.Fatal("incomplete PDF returned on aggregate capacity failure")
	}
}
