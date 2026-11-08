package evaluation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/pdfassemble"
)

func TestIndependentPDFValidation(t *testing.T) {
	pdfPath, sourcePath := buildValidationPDF(t, "ALPHA ONE")
	receipt, err := ValidatePDF(context.Background(), pdfPath, []string{"page_validation_001"},
		[]string{"ALPHA ONE"}, PDFValidationOptions{OriginalImagePath: sourcePath, ExpectLossless: true})
	if err != nil {
		t.Fatalf("validation failed: %v receipt=%+v", err, receipt)
	}
	if receipt.Status != "passed" || len(receipt.Tools) != 4 || len(receipt.Pages) != 1 ||
		receipt.Pages[0].PageID != "page_validation_001" || receipt.Pages[0].EmbeddedImages != 1 ||
		receipt.Pages[0].RenderSHA256 == "" || receipt.Pages[0].ExpectedTextMatches != 1 {
		t.Fatalf("unexpected validation receipt: %+v", receipt)
	}
	for _, check := range receipt.Checks {
		if check.Status != "pass" {
			t.Fatalf("failed check in passed receipt: %+v", receipt)
		}
	}
}

func TestValidatorFailsClosed(t *testing.T) {
	corrupt := filepath.Join(t.TempDir(), "corrupt.pdf")
	if err := os.WriteFile(corrupt, []byte("not a pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := ValidatePDF(context.Background(), corrupt, []string{"page_validation_001"}, nil, PDFValidationOptions{})
	if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" {
		t.Fatalf("corrupt PDF passed: %+v %v", receipt, err)
	}

	pdfPath, _ := buildValidationPDF(t, "ALPHA ONE")
	receipt, err = validatePDF(context.Background(), pdfPath,
		[]string{"page_validation_001", "page_validation_002"}, []string{"ALPHA ONE"},
		PDFValidationOptions{}, []string{"/does-not-exist"})
	if !errors.Is(err, ErrUnavailable) || receipt.Status != "unavailable" {
		t.Fatalf("missing tools were not unavailable: %+v %v", receipt, err)
	}

	receipt, err = ValidatePDF(context.Background(), pdfPath,
		[]string{"page_validation_001", "page_validation_002"}, []string{"ALPHA ONE"}, PDFValidationOptions{})
	if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" {
		t.Fatalf("wrong page count passed: %+v %v", receipt, err)
	}
}

func TestValidatePDFRejectsPartialOriginalOptions(t *testing.T) {
	pdfPath, sourcePath := buildValidationPDF(t, "ALPHA ONE")
	for _, options := range []PDFValidationOptions{
		{ExpectLossless: true},
		{OriginalImagePath: sourcePath},
	} {
		receipt, err := ValidatePDF(context.Background(), pdfPath, []string{"page_validation_001"},
			[]string{"ALPHA ONE"}, options)
		if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" ||
			receipt.FailureClass != "invalid-input" {
			t.Fatalf("partial original options accepted: options=%+v receipt=%+v err=%v", options, receipt, err)
		}
	}
}

func buildValidationPDF(t *testing.T, expectedText string) (string, string) {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 80, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 80; x++ {
			bitmap.SetRGBA(x, y, color.RGBA{uint8(x * 3), uint8(y * 2), 100, 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, bitmap); err != nil {
		t.Fatal(err)
	}
	sourceBytes := append([]byte(nil), source.Bytes()...)
	page := pdfassemble.Page{
		ID: "page_validation_001", Size: "a5", Color: "color", Width: 80, Height: 100,
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(sourceBytes)), nil
		},
	}
	layer := pdfassemble.TextLayer{
		PageID: page.ID,
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(textLayerPDF(expectedText))), nil
		},
	}
	pdf, err := pdfassemble.BuildWithLayers(context.Background(), []pdfassemble.Page{page},
		[]pdfassemble.TextLayer{layer}, "lossless", pdfassemble.Options{
			MaxInputBytes: 1 << 20, MaxOutputBytes: 4 << 20, MaxPagePixels: 1000000,
			MaxPages: 8, JPEGQuality: 92,
		})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "searchable.pdf")
	sourcePath := filepath.Join(dir, "source.png")
	if err := os.WriteFile(pdfPath, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return pdfPath, sourcePath
}

func textLayerPDF(text string) []byte {
	content := "BT /F1 8 Tf 3 Tr 10 50 Td (" + text + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 80 100] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, output.Len())
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return output.Bytes()
}
