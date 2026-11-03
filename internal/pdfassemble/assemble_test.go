package pdfassemble

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func opts() Options {
	return Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 4 << 20, MaxPagePixels: 1000000, MaxPages: 50, JPEGQuality: 92}
}
func source(t *testing.T, id, format string) Page {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 80, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 80; x++ {
			bitmap.SetRGBA(x, y, color.RGBA{uint8(x * 3), uint8(y * 2), 100, 255})
		}
	}
	var b bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&b, bitmap, &jpeg.Options{Quality: 92})
	} else {
		err = png.Encode(&b, bitmap)
	}
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), b.Bytes()...)
	return Page{ID: id, Size: "a4", Color: "color", Width: 80, Height: 100, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}
func TestBuildMixedSizeAndRotatedPDF(t *testing.T) {
	for _, encoding := range []string{"jpeg", "lossless"} {
		t.Run(encoding, func(t *testing.T) {
			a, b := source(t, "page-one", "jpeg"), source(t, "page-two", "png")
			b.Size = "a5"
			b.Rotation = 90
			out, err := Build(context.Background(), []Page{a, b}, encoding, opts())
			if err != nil {
				t.Fatal(err)
			}
			conf := model.NewStatelessConfiguration()
			count, err := api.PageCount(context.Background(), bytes.NewReader(out), conf)
			if err != nil || count != 2 {
				t.Fatal("incorrect page count")
			}
			dimensions, err := api.PageDims(context.Background(), bytes.NewReader(out), conf)
			if err != nil || len(dimensions) != 2 {
				t.Fatal("missing media boxes")
			}
			w, h, _ := paper("a4")
			if math.Abs(dimensions[0].Width-w) > .01 || math.Abs(dimensions[0].Height-h) > .01 {
				t.Fatal("incorrect A4 geometry")
			}
		})
	}
}
func TestRejectsInvalidPagesAndBounds(t *testing.T) {
	for _, mode := range []string{"duplicate", "rotation", "size", "pixels", "input", "output", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			page := source(t, "page-one", "png")
			pages := []Page{page}
			options := opts()
			switch mode {
			case "duplicate":
				pages = append(pages, page)
			case "rotation":
				pages[0].Rotation = 45
			case "size":
				pages[0].Size = "unknown"
			case "pixels":
				options.MaxPagePixels = 1
			case "input":
				options.MaxInputBytes = 1
			case "output":
				options.MaxOutputBytes = 1
			case "corrupt":
				pages[0].Open = func(context.Context) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewBufferString("not an image")), nil
				}
			}
			if out, err := Build(context.Background(), pages, "lossless", options); err == nil || len(out) > 0 {
				t.Fatal("invalid PDF request accepted")
			}
		})
	}
}
func TestCancelledBuildDoesNotOpenInput(t *testing.T) {
	page := source(t, "page-one", "png")
	page.Open = func(context.Context) (io.ReadCloser, error) {
		t.Fatal("cancelled build opened source")
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := Build(ctx, []Page{page}, "lossless", opts()); !errors.Is(err, context.Canceled) || len(out) > 0 {
		t.Fatal("cancelled PDF produced data")
	}
}
