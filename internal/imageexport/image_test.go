package imageexport

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func fixture(t *testing.T, format string) ([]byte, *image.Gray) {
	t.Helper()
	bitmap := image.NewGray(image.Rect(0, 0, 3, 2))
	copy(bitmap.Pix, []byte{10, 30, 50, 90, 120, 150})
	var buffer bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buffer, bitmap)
	} else {
		err = jpeg.Encode(&buffer, bitmap, &jpeg.Options{Quality: 95})
	}
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), bitmap
}
func options() Options {
	return Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPixels: 1000, JPEGQuality: 92}
}
func TestPreservesOriginalEncodingAndIndependentBytes(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			raw, _ := fixture(t, format)
			output, err := Build(context.Background(), raw, Page{Width: 3, Height: 2, Color: "gray"}, format, options())
			if err != nil || !bytes.Equal(output, raw) {
				t.Fatalf("original stream changed: %v", err)
			}
			output[0] ^= 1
			if bytes.Equal(output, raw) {
				t.Fatal("output aliases original")
			}
		})
	}
}
func TestLosslessRotationPixels(t *testing.T) {
	cases := []struct {
		angle, width, height int
		pixels               []byte
	}{
		{90, 2, 3, []byte{90, 10, 120, 30, 150, 50}},
		{180, 3, 2, []byte{150, 120, 90, 50, 30, 10}},
		{270, 2, 3, []byte{50, 150, 30, 120, 10, 90}},
	}
	for _, test := range cases {
		raw, _ := fixture(t, "png")
		output, err := Build(context.Background(), raw, Page{Width: 3, Height: 2, Rotation: test.angle, Color: "gray"}, "png", options())
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds().Dx() != test.width || decoded.Bounds().Dy() != test.height {
			t.Fatal("wrong rotated dimensions")
		}
		for y := 0; y < test.height; y++ {
			for x := 0; x < test.width; x++ {
				if color.GrayModel.Convert(decoded.At(x, y)).(color.Gray).Y != test.pixels[y*test.width+x] {
					t.Fatal("clockwise rotation has incorrect pixels")
				}
			}
		}
	}
}

func TestFormatConversionAndBounds(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		inputFormat := "png"
		if format == "png" {
			inputFormat = "jpeg"
		}
		raw, _ := fixture(t, inputFormat)
		output, err := Build(context.Background(), raw, Page{Width: 3, Height: 2, Color: "gray"}, format, options())
		if err != nil {
			t.Fatal(err)
		}
		_, actual, err := image.Decode(bytes.NewReader(output))
		if err != nil || actual != format {
			t.Fatal("incorrect encoded format")
		}
	}
	raw, _ := fixture(t, "png")
	policy := options()
	policy.MaxOutputBytes = 1
	if output, err := Build(context.Background(), raw, Page{Width: 3, Height: 2, Color: "gray"}, "jpeg", policy); err == nil || len(output) > 0 {
		t.Fatal("output limit ignored")
	}
	policy = options()
	policy.MaxPixels = 1
	if _, err := Build(context.Background(), raw, Page{Width: 3, Height: 2, Color: "gray"}, "png", policy); err == nil {
		t.Fatal("pixel limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, raw, Page{Width: 3, Height: 2, Color: "gray"}, "png", options()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestRGBConversionKeepsEightBitColor(t *testing.T) {
	bitmap := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			bitmap.SetRGBA(x, y, color.RGBA{uint8(x * 25), uint8(y * 30), 100, 255})
		}
	}
	for _, format := range []string{"png", "jpeg"} {
		var raw bytes.Buffer
		var err error
		if format == "png" {
			err = jpeg.Encode(&raw, bitmap, &jpeg.Options{Quality: 92})
		} else {
			err = png.Encode(&raw, bitmap)
		}
		if err != nil {
			t.Fatal(err)
		}
		output, err := Build(context.Background(), raw.Bytes(), Page{Width: 8, Height: 6, Color: "color"}, format, options())
		if err != nil {
			t.Fatal(err)
		}
		decoded, actual, err := image.Decode(bytes.NewReader(output))
		if err != nil || actual != format || decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 6 {
			t.Fatal("invalid RGB conversion")
		}
		if format == "png" {
			source, _, err := image.Decode(bytes.NewReader(raw.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 6; y++ {
				for x := 0; x < 8; x++ {
					if color.RGBAModel.Convert(decoded.At(x, y)) != color.RGBAModel.Convert(source.At(x, y)) {
						t.Fatal("JPEG-decoded pixels changed during PNG export")
					}
				}
			}
		}
	}
}
