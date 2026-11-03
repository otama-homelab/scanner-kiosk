package captureimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func fixture(t *testing.T, format, mode string) []byte {
	t.Helper()
	var bitmap image.Image
	if mode == "gray" {
		bitmap = image.NewGray(image.Rect(0, 0, 80, 100))
	} else {
		rgb := image.NewRGBA(image.Rect(0, 0, 80, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 80; x++ {
				rgb.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 255, 255})
			}
		}
		bitmap = rgb
	}
	var out bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&out, bitmap)
	} else {
		err = jpeg.Encode(&out, bitmap, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func options(mode string) Options {
	return Options{MaxBytes: 1 << 20, MaxPixels: 10000, Width: 80, Height: 100, Color: mode}
}
func TestValidatedPNGAndJPEGMetadataMatchesActualBytes(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		for _, mode := range []string{"gray", "color"} {
			t.Run(format+"/"+mode, func(t *testing.T) {
				data := fixture(t, format, mode)
				input := bytes.NewReader(data)
				info, err := Validate(context.Background(), input, options(mode))
				if err != nil {
					t.Fatal(err)
				}
				if info.Width != 80 || info.Height != 100 || info.MediaType != "image/"+format || info.Size != int64(len(data)) || info.SHA256 != sha256.Sum256(data) {
					t.Fatal("incorrect bitmap metadata")
				}
				if input.Len() != len(data) {
					t.Fatal("input not rewound")
				}
			})
		}
	}
}
func TestRejectsInvalidTruncatedOrMismatchedBitmap(t *testing.T) {
	for _, mode := range []string{"garbage", "truncated", "geometry", "color", "bytes", "pixels", "transparent", "depth16"} {
		t.Run(mode, func(t *testing.T) {
			data := fixture(t, "png", "color")
			opts := options("color")
			switch mode {
			case "garbage":
				data = []byte("not an image")
			case "truncated":
				data = data[:len(data)/2]
			case "geometry":
				opts.Width = 81
			case "color":
				opts.Color = "gray"
			case "bytes":
				opts.MaxBytes = 1
			case "pixels":
				opts.MaxPixels = 7999
			case "transparent":
				var b bytes.Buffer
				png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 80, 100)))
				data = b.Bytes()
			case "depth16":
				var b bytes.Buffer
				png.Encode(&b, image.NewGray16(image.Rect(0, 0, 80, 100)))
				data = b.Bytes()
				opts.Color = "gray"
			}
			if _, err := Validate(context.Background(), bytes.NewReader(data), opts); err == nil {
				t.Fatal("invalid captured image accepted")
			}
		})
	}
}
func TestCancelledImageValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Validate(ctx, bytes.NewReader(fixture(t, "png", "gray")), options("gray")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled validation continued")
	}
}

func TestDecodeReturnsVerifiedBitmapAndNoBitmapOnFailure(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		for _, mode := range []string{"gray", "color"} {
			t.Run(format+mode, func(t *testing.T) {
				raw := fixture(t, format, mode)
				input := bytes.NewReader(raw)
				info, bitmap, err := Decode(context.Background(), input, options(mode))
				if err != nil || bitmap == nil || bitmap.Bounds().Dx() != info.Width || bitmap.Bounds().Dy() != info.Height || info.SHA256 != sha256.Sum256(raw) {
					t.Fatal(info, err)
				}
				if position, err := input.Seek(0, 1); err != nil || position != 0 {
					t.Fatal("not rewound", position, err)
				}
				invalid := options(mode)
				invalid.Width++
				if _, bitmap, err = Decode(context.Background(), bytes.NewReader(raw), invalid); err == nil || bitmap != nil {
					t.Fatal("invalid bitmap escaped", err)
				}
			})
		}
	}
}
