package pdfassemble

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in artificial exports are consumed by an independent pikepdf verifier.
func TestIndependentVerificationFixtures(t *testing.T) {
	root := os.Getenv("SCANNER_TEST_PDF_OUTPUT_DIR")
	if root == "" {
		t.Skip("requires independent verification directory")
	}
	rgbJPEG := source(t, "rgb-jpeg", "jpeg")
	rgbPNG := source(t, "rgb-png", "png")
	rgbPNG.Size = "a6"
	gray := image.NewGray(image.Rect(0, 0, 80, 100))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i % 251)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, gray); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()
	grayPNG := Page{ID: "gray-png", Size: "a5", Color: "gray", Width: 80, Height: 100, Rotation: 90, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
	for _, entry := range []struct {
		name string
		page Page
	}{{"source-jpeg.jpg", rgbJPEG}, {"source-rgb.png", rgbPNG}, {"source-gray.png", grayPNG}} {
		input, err := entry.page.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(input)
		input.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, entry.name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	input, err := rgbJPEG.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(input)
	input.Close()
	if err != nil {
		t.Fatal(err)
	}
	rgb := image.NewRGBA(decoded.Bounds())
	draw.Draw(rgb, rgb.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	var expected bytes.Buffer
	if err = png.Encode(&expected, rgb); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source-jpeg-decoded.png"), expected.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, encoding := range []string{"jpeg", "lossless"} {
		pdf, err := Build(context.Background(), []Page{grayPNG, rgbJPEG, rgbPNG}, encoding, opts())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, encoding+".pdf"), pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
	alpha, beta := rgbJPEG, rgbPNG
	alpha.Rotation = 90
	beta.Size = "a5"
	beta.Rotation = 180
	for _, encoding := range []string{"jpeg", "lossless"} {
		pdf, err := BuildWithLayers(context.Background(), []Page{beta, alpha}, []TextLayer{layer(alpha.ID, "ALPHA ONE"), layer(beta.ID, "BETA TWO")}, encoding, opts())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "ocr-"+encoding+".pdf"), pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}

}
