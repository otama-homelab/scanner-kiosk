// Package thumbnail creates bounded source-orientation JPEG derivatives in the
// resource-limited worker. Rotation remains page metadata, never baked in twice.
package thumbnail

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"

	"scanner-kiosk/internal/captureimage"
)

var ErrInvalid = errors.New("invalid thumbnail options")
var ErrLimit = errors.New("thumbnail output exceeds limit")

type Options struct {
	Input          captureimage.Options
	MaxEdge        int
	MaxOutputBytes int64
	Quality        int
}
type Result struct {
	Data          []byte
	Width, Height int
}
type writer struct {
	ctx   context.Context
	data  bytes.Buffer
	limit int64
}

func (w *writer) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.limit-int64(w.data.Len()) {
		return 0, ErrLimit
	}
	return w.data.Write(p)
}

// Build validates the complete original before shrinking, never upscales, and
// allocates only one full decoded source plus a bounded derivative. Box averaging
// keeps fine document strokes better than nearest-neighbor sampling. Options
// bound allocation, not a hard decoder CPU/RSS sandbox; execute only in a worker.
func Build(ctx context.Context, input io.ReadSeeker, options Options) (Result, error) {
	if options.MaxEdge < 1 || options.MaxEdge > 2048 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 8<<20 || options.Quality < 1 || options.Quality > 95 {
		return Result{}, ErrInvalid
	}
	info, source, err := captureimage.Decode(ctx, input, options.Input)
	if err != nil {
		return Result{}, err
	}
	width, height := info.Width, info.Height
	if width > options.MaxEdge || height > options.MaxEdge {
		if width >= height {
			height = max(1, int(int64(height)*int64(options.MaxEdge)/int64(width)))
			width = options.MaxEdge
		} else {
			width = max(1, int(int64(width)*int64(options.MaxEdge)/int64(height)))
			height = options.MaxEdge
		}
	}
	var output image.Image
	if options.Input.Color == "gray" {
		output = image.NewGray(image.Rect(0, 0, width, height))
	} else {
		output = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	for y := 0; y < height; y++ {
		if err = ctx.Err(); err != nil {
			return Result{}, err
		}
		y0, y1 := y*info.Height/height, (y+1)*info.Height/height
		for x := 0; x < width; x++ {
			x0, x1 := x*info.Width/width, (x+1)*info.Width/width
			var r, g, b, count uint64
			for sy := y0; sy < y1; sy++ {
				if err = ctx.Err(); err != nil {
					return Result{}, err
				}
				for sx := x0; sx < x1; sx++ {
					red, green, blue, _ := source.At(sx, sy).RGBA()
					r += uint64(red)
					g += uint64(green)
					b += uint64(blue)
					count++
				}
			}
			switch bitmap := output.(type) {
			case *image.Gray:
				bitmap.SetGray(x, y, colorGray(r/count))
			case *image.RGBA:
				bitmap.SetRGBA(x, y, colorRGB(r/count, g/count, b/count))
			}
		}
	}
	encoded := &writer{ctx: ctx, limit: options.MaxOutputBytes}
	if err = jpeg.Encode(encoded, output, &jpeg.Options{Quality: options.Quality}); err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{Data: encoded.data.Bytes(), Width: width, Height: height}, nil
}

func colorGray(v uint64) color.Gray { return color.Gray{Y: uint8(v >> 8)} }
func colorRGB(r, g, b uint64) color.RGBA {
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
}
