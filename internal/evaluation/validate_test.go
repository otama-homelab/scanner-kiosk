package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestOCRPDFOriginalRetention(t *testing.T) {
	path, err := filepath.Abs("../../scripts/fixtures/ocr/fixture_ocr_two_page_001.pdf")
	if err != nil { t.Fatal(err) }
	receipt, err := ValidatePDF(context.Background(), path, []string{"page_ocr_two_page_001", "page_ocr_two_page_002"}, nil, PDFValidationOptions{OriginalImagePath:path, ExpectLossless:true})
	if err != nil || receipt.Status != "passed" || len(receipt.Pages) != 2 {
		t.Fatalf("unchanged PDF source must retain per-page pixels: %+v %v",receipt,err)
	}
}

func TestOCRPDFPageOracle(t *testing.T) {
	pdf, ids, options := ocrPDFOracleFixture(t, "correct")
	receipt, err := ValidatePDF(context.Background(), pdf, ids, []string{"ALPHA ONE", "BRAVO TWO"}, options)
	if err != nil || receipt.Status != "passed" || len(receipt.Pages) != 2 {
		t.Fatalf("page oracle rejected: %+v %v", receipt, err)
	}
}

func TestOCRPDFRejectsSwappedPages(t *testing.T) {
	pdf, ids, options := ocrPDFOracleFixture(t, "swapped-pages")
	receipt, err := ValidatePDF(context.Background(), pdf, ids, []string{"ALPHA ONE", "BRAVO TWO"}, options)
	if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" {
		t.Fatalf("swapped source pages passed: %+v %v", receipt, err)
	}
}

func TestOCRPDFRejectsWrongPageText(t *testing.T) {
	for _, mode := range []string{"wrong-page-text", "wrong-geometry", "wrong-source-hash", "wrong-text-order"} {
		t.Run(mode, func(t *testing.T) {
			pdf, ids, options := ocrPDFOracleFixture(t, mode)
			receipt, err := ValidatePDF(context.Background(), pdf, ids, []string{"ALPHA ONE", "BRAVO TWO"}, options)
			if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" {
				t.Fatalf("wrong per-page oracle passed: %+v %v", receipt, err)
			}
		})
	}
}

func TestOCRPDFRetainsPixels(t *testing.T) {
	pdf, ids, options := ocrPDFOracleFixture(t, "lost-pixels")
	receipt, err := ValidatePDF(context.Background(), pdf, ids, []string{"ALPHA ONE", "BRAVO TWO"}, options)
	if !errors.Is(err, ErrInvalid) || receipt.Status == "passed" {
		t.Fatalf("changed source pixels passed: %+v %v", receipt, err)
	}
}

func ocrPDFOracleFixture(t *testing.T, mode string) (string, []string, PDFValidationOptions) {
	t.Helper()
	dir := t.TempDir()
	ids := []string{"page_ocr_pdf_001", "page_ocr_pdf_002"}
	texts := []string{"ALPHA ONE", "BRAVO TWO"}
	sources := make([][]byte, 2)
	paths := make([]string, 2)
	for i := range sources {
		bitmap := image.NewRGBA(image.Rect(0, 0, 80, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 80; x++ {
				bitmap.SetRGBA(x, y, color.RGBA{uint8(x * 3), uint8(y * 2), uint8(40 + i*100), 255})
			}
		}
		var source bytes.Buffer
		if err := png.Encode(&source, bitmap); err != nil {
			t.Fatal(err)
		}
		sources[i] = source.Bytes()
		paths[i] = filepath.Join(dir, fmt.Sprintf("source%d.png", i))
		if err := os.WriteFile(paths[i], sources[i], 0600); err != nil {
			t.Fatal(err)
		}
	}
	var pages []pdfassemble.Page
	var layers []pdfassemble.TextLayer
	var oracles []any
	for i, id := range ids {
		sourceIndex, textIndex := i, i
		if mode == "swapped-pages" {
			sourceIndex, textIndex = 1-i, 1-i
		}
		if mode == "wrong-page-text" {
			textIndex = 1 - i
		}
		if mode == "lost-pixels" && i == 0 {
			sourceIndex = 1
		}
		sourceData, layerData := sources[sourceIndex], textLayerPDF(texts[textIndex])
		pages = append(pages, pdfassemble.Page{ID: id, Size: "a5", Color: "color", Width: 80, Height: 100, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(sourceData)), nil }})
		layers = append(layers, pdfassemble.TextLayer{PageID: id, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(layerData)), nil }})
		sha := hashBytes(sources[i])
		if mode == "wrong-source-hash" && i == 0 {
			sha = hashBytes(sources[1])
		}
		spans := []OCROracleSpan{{Text: texts[i], Order: 0, Box: [4]float64{0, 0, 80, 100}}}
		if mode == "wrong-text-order" && i == 0 {
			spans = []OCROracleSpan{{Text: "ONE", Order: 0, Box: [4]float64{0, 0, 40, 100}}, {Text: "ALPHA", Order: 1, Box: [4]float64{40, 0, 40, 100}}}
		}
		box := [4]float64{0, 0, 148.0 / 25.4 * 72, 210.0 / 25.4 * 72}
		if mode == "wrong-geometry" && i == 0 {
			box[2] += 10
		}
		oracles = append(oracles, map[string]any{"ocr": OCRPageOracle{PageID: id, SourceSHA256: sha, Width: 80, Height: 100, Spans: spans}, "source_image_path": paths[i], "media_box": box, "rotation": 0})
	}
	pdf, err := pdfassemble.BuildWithLayers(context.Background(), pages, layers, "lossless", pdfassemble.Options{MaxInputBytes: 4 << 20, MaxOutputBytes: 8 << 20, MaxPagePixels: 1000000, MaxPages: 8, JPEGQuality: 92})
	if err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(dir, "ocr.pdf")
	if err = os.WriteFile(pdfPath, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	// Decode the additive option as supplied by a caller. Older validators
	// silently ignore it and incorrectly accept the page-swap negatives.
	raw, err := json.Marshal(map[string]any{"page_oracles": oracles})
	if err != nil {
		t.Fatal(err)
	}
	var options PDFValidationOptions
	if err = json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	return pdfPath, ids, options
}

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

func TestDecodeImageFileRejectsOversizedInput(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "oversized.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxArtifactBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeImageFile(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized image input was not rejected: %v", err)
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
