package thumbnail

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"scanner-kiosk/internal/captureimage"
)

func fixture(t *testing.T, w, h int, gray bool, format string) []byte {
	t.Helper()
	var bitmap image.Image
	if gray {
		img := image.NewGray(image.Rect(0, 0, w, h))
		for i := range img.Pix {
			img.Pix[i] = 180
		}
		bitmap = img
	} else {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 220, G: 40, B: 60, A: 255})
			}
		}
		bitmap = img
	}
	var b bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&b, bitmap, &jpeg.Options{Quality: 95})
	} else {
		err = png.Encode(&b, bitmap)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func options(w, h int, gray bool) Options {
	mode := "color"
	if gray {
		mode = "gray"
	}
	return Options{Input: captureimage.Options{MaxBytes: 1 << 20, MaxPixels: 1 << 20, Width: w, Height: h, Color: mode}, MaxEdge: 64, MaxOutputBytes: 1 << 20, Quality: 90}
}
func TestDerivativeDimensionsColorAndImmutableOriginal(t *testing.T) {
	for _, test := range []struct {
		name, format string
		w, h, ow, oh int
		gray         bool
	}{{"landscape", "png", 160, 80, 64, 32, false}, {"portrait", "jpeg", 80, 160, 32, 64, false}, {"gray", "png", 160, 80, 64, 32, true}, {"small", "jpeg", 20, 10, 20, 10, true}, {"thin", "png", 1, 160, 1, 64, false}} {
		t.Run(test.name, func(t *testing.T) {
			raw := fixture(t, test.w, test.h, test.gray, test.format)
			before := bytes.Clone(raw)
			output, err := Build(context.Background(), bytes.NewReader(raw), options(test.w, test.h, test.gray))
			if err != nil {
				t.Fatal(err)
			}
			bitmap, format, err := image.Decode(bytes.NewReader(output.Data))
			if err != nil || format != "jpeg" || bitmap.Bounds().Dx() != test.ow || bitmap.Bounds().Dy() != test.oh || output.Width != test.ow || output.Height != test.oh {
				t.Fatal(output.Width, output.Height, format, err)
			}
			if !bytes.Equal(raw, before) {
				t.Fatal("original modified")
			}
			r, g, b, _ := bitmap.At(0, 0).RGBA()
			if test.gray {
				if r != g || g != b {
					t.Fatal("gray became colored")
				}
			} else if r < g*3 || r < b*2 {
				t.Fatal("source color lost", r, g, b)
			}
		})
	}
}
func TestRejectsInvalidUntrustedAndOverBudgetInput(t *testing.T) {
	raw := fixture(t, 16, 8, false, "png")
	tests := []struct {
		name     string
		change   func(*Options)
		input    []byte
		expected error
	}{
		{"edge", func(o *Options) { o.MaxEdge = 2049 }, raw, ErrInvalid},
		{"quality", func(o *Options) { o.Quality = 0 }, raw, ErrInvalid},
		{"byte budget", func(o *Options) { o.Input.MaxBytes = 1 }, raw, captureimage.ErrLimit},
		{"pixel budget", func(o *Options) { o.Input.MaxPixels = 100 }, raw, captureimage.ErrLimit},
		{"geometry", func(o *Options) { o.Input.Width = 8 }, raw, captureimage.ErrImage},
		{"color", func(o *Options) { o.Input.Color = "gray" }, raw, captureimage.ErrImage},
		{"truncated", func(o *Options) {}, raw[:len(raw)/2], captureimage.ErrImage},
		{"output", func(o *Options) { o.MaxOutputBytes = 1 }, raw, ErrLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := options(16, 8, false)
			test.change(&o)
			result, err := Build(context.Background(), bytes.NewReader(test.input), o)
			if !errors.Is(err, test.expected) || result.Data != nil || result.Width != 0 {
				t.Fatal(result, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := Build(ctx, bytes.NewReader(raw), options(16, 8, false)); !errors.Is(err, context.Canceled) || result.Data != nil {
		t.Fatal(result, err)
	}
}
func TestFineStrokesSurviveDownsampling(t *testing.T) {
	bitmap := image.NewGray(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			value := uint8(255)
			if x%2 == 0 {
				value = 0
			}
			bitmap.SetGray(x, y, color.Gray{Y: value})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, bitmap); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), bytes.NewReader(raw.Bytes()), options(128, 128, true))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := decoded.At(32, 32).RGBA()
	if r>>8 < 110 || r>>8 > 145 {
		t.Fatal("thin strokes disappeared", r>>8)
	}
}
