package pdfassemble

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestLosslessSamplesPreservePixelsAndBoundAllocations(t *testing.T) {
	for _, kind := range []string{"rgba", "nrgba", "gray", "ycbcr"} {
		t.Run(kind, func(t *testing.T) {
			bounds := image.Rect(3, 4, 29, 35)
			var bitmap image.Image
			switch kind {
			case "gray":
				p := image.NewGray(bounds)
				for y := 4; y < 35; y++ {
					for x := 3; x < 29; x++ {
						p.SetGray(x, y, color.Gray{uint8(x + y)})
					}
				}
				bitmap = p
			case "ycbcr":
				p := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio444)
				for i := range p.Y {
					p.Y[i] = 120
					p.Cb[i] = 80
					p.Cr[i] = 150
				}
				bitmap = p
			default:
				if kind == "rgba" {
					p := image.NewRGBA(bounds)
					for y := 4; y < 35; y++ {
						for x := 3; x < 29; x++ {
							p.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 77, 255})
						}
					}
					bitmap = p
				} else {
					p := image.NewNRGBA(bounds)
					for y := 4; y < 35; y++ {
						for x := 3; x < 29; x++ {
							p.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 77, 255})
						}
					}
					bitmap = p
				}
			}
			var want []byte
			for y := 4; y < 35; y++ {
				for x := 3; x < 29; x++ {
					if kind == "gray" {
						want = append(want, color.GrayModel.Convert(bitmap.At(x, y)).(color.Gray).Y)
					} else {
						c := color.NRGBAModel.Convert(bitmap.At(x, y)).(color.NRGBA)
						want = append(want, c.R, c.G, c.B)
					}
				}
			}
			got, space, err := losslessSamples(context.Background(), bitmap)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("pixels changed: %v", err)
			}
			expectedSpace := "DeviceRGB"
			if kind == "gray" {
				expectedSpace = "DeviceGray"
			}
			if space != expectedSpace {
				t.Fatal(space)
			}
			if kind != "ycbcr" {
				n := testing.AllocsPerRun(2, func() { _, _, err = losslessSamples(context.Background(), bitmap) })
				if err != nil || n > 2 {
					t.Fatalf("per-pixel allocations: %g %v", n, err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, _, err = losslessSamples(ctx, bitmap); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
