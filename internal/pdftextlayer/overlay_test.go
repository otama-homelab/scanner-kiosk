package pdftextlayer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
)

func textLayer() []byte {
	return sizedTextLayer(80, 100)
}
func sizedTextLayer(width, height float64) []byte {
	content := "BT /F1 8 Tf 3 Tr 10 50 Td (OCR layer) Tj ET"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %f %f] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", width, height), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
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
func basePDF(t *testing.T) []byte {
	t.Helper()
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, image.NewGray(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	output, err := pdfassemble.Build(context.Background(), []pdfassemble.Page{{ID: "original-page", Size: "a4", Color: "gray", Width: 80, Height: 100, Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bitmap.Bytes())), nil
	}}}, "lossless", pdfassemble.Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 1, JPEGQuality: 92})
	if err != nil {
		t.Fatal(err)
	}
	return output
}
func TestOverlayAndRejectInvalidLayers(t *testing.T) {
	base, layer := basePDF(t), textLayer()
	if output, err := pdftextlayer.Overlay(context.Background(), base, layer, 80, 100, 1<<20); err != nil || len(output) == 0 {
		t.Fatalf("compose: %v", err)
	}
	for _, mode := range []string{"image-layer", "two-pages", "aspect", "rotated", "corrupt", "byte-limit"} {
		t.Run(mode, func(t *testing.T) {
			b, l := base, layer
			width := 80
			limit := int64(1 << 20)
			switch mode {
			case "image-layer":
				l = base
			case "two-pages":
				var merged bytes.Buffer
				if err := api.MergeRaw(context.Background(), []io.ReadSeeker{bytes.NewReader(layer), bytes.NewReader(layer)}, &merged, false, model.NewStatelessConfiguration()); err != nil {
					t.Fatal(err)
				}
				l = merged.Bytes()
			case "aspect":
				width = 160
			case "rotated":
				var rotated bytes.Buffer
				if err := api.Rotate(context.Background(), bytes.NewReader(base), &rotated, 90, nil, model.NewStatelessConfiguration()); err != nil {
					t.Fatal(err)
				}
				b = rotated.Bytes()
			case "corrupt":
				l = []byte("invalid PDF")
			case "byte-limit":
				limit = 1
			}
			if output, err := pdftextlayer.Overlay(context.Background(), b, l, width, 100, limit); err == nil || len(output) > 0 {
				t.Fatal("invalid composition accepted")
			}
		})
	}
}
func TestCancelledOverlay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pdftextlayer.Overlay(ctx, nil, nil, 80, 100, 1<<20); err != context.Canceled {
		t.Fatal("cancellation lost")
	}
}

func TestOverlayPreservesSourceContent(t *testing.T) {
	ctx := context.Background()
	base := basePDF(t)
	boxes, err := api.Boxes(ctx, bytes.NewReader(base), nil, model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	output, err := pdftextlayer.OverlayPDF(ctx, base, sizedTextLayer(boxes[0].CropBox().Width(), boxes[0].CropBox().Height()), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	// Independent Poppler rendering checks that invisible text did not alter
	// source pixels. No snapshot or compositor self-validation is the oracle.
	render := func(raw []byte) image.Image {
		dir := t.TempDir()
		input := filepath.Join(dir, "input.pdf")
		prefix := filepath.Join(dir, "page")
		if err := os.WriteFile(input, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if result, err := exec.CommandContext(ctx, "pdftoppm", "-f", "1", "-singlefile", "-scale-to", "300", "-png", input, prefix).CombinedOutput(); err != nil {
			t.Fatalf("independent render: %v %s", err, result)
		}
		f, err := os.Open(prefix + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		im, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return im
	}
	a, b := render(base), render(output)
	if a.Bounds() != b.Bounds() {
		t.Fatal("source bounds changed")
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				t.Fatal("source pixels changed", x, y)
			}
		}
	}
	for _, test := range []struct {
		raw  []byte
		want string
	}{{base, pdftextlayer.TextOriginRecognition}, {textLayer(), pdftextlayer.TextOriginRetainedSource}, {output, pdftextlayer.TextOriginRetainedSource}} {
		origin, err := pdftextlayer.PDFTextOrigin(ctx, test.raw, 1<<20)
		if err != nil || origin != test.want {
			t.Fatal("wrong source provenance", origin, err)
		}
	}
	if _, err := pdftextlayer.PDFTextOrigin(ctx, []byte("broken"), 1<<20); err == nil {
		t.Fatal("unverifiable source accepted")
	}
}

func TestOverlayBoundsFinalWriter(t *testing.T) {
	base, layer := basePDF(t), textLayer()
	limit := int64(max(len(base), len(layer)))
	full, err := pdftextlayer.Overlay(context.Background(), base, layer, 80, 100, 1<<20)
	if err != nil || int64(len(full)) <= limit {
		t.Fatal("fixture must grow after composition")
	}
	output, err := pdftextlayer.Overlay(context.Background(), base, layer, 80, 100, limit)
	if !errors.Is(err, pdftextlayer.ErrLimit) || len(output) != 0 {
		t.Fatalf("final output bound bypassed: %v", err)
	}
}
