package pdfassemble

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
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
