package pdftextlayer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
)

func textLayer() []byte {
	content := "BT /F1 8 Tf 3 Tr 10 50 Td (OCR layer) Tj ET"
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
