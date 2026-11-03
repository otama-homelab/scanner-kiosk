// Package imageexport produces one JPEG/PNG from a validated captured bitmap.
package imageexport

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"

	"scanner-kiosk/internal/captureimage"
)

var ErrInvalid = errors.New("invalid image export")
var ErrLimit = errors.New("image export limit exceeded")

type Options struct {
	MaxInputBytes, MaxOutputBytes, MaxPixels int64
	JPEGQuality                              int
}
type Page struct {
	Width, Height, Rotation int
	Color                   string
}
type bounded struct {
	data  bytes.Buffer
	limit int64
	ctx   context.Context
}

func (b *bounded) Write(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > b.limit-int64(b.data.Len()) {
		return 0, ErrLimit
	}
	return b.data.Write(p)
}

// Build preserves the exact original encoding when format matches and rotation
// is zero. Otherwise it decodes/rotates and encodes once, never modifying input.
// It preserves pixel dimensions (swapping axes for 90/270), not optical DPI proof
// or ancillary image metadata. Execute in the resource-limited worker.
func Build(ctx context.Context, raw []byte, page Page, format string, options Options) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<30 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 1<<30 || options.MaxPixels < 1 || options.JPEGQuality < 1 || options.JPEGQuality > 100 || (format != "jpeg" && format != "png") || (page.Rotation != 0 && page.Rotation != 90 && page.Rotation != 180 && page.Rotation != 270) {
		return nil, ErrInvalid
	}
	info, input, err := captureimage.Decode(ctx, bytes.NewReader(raw), captureimage.Options{MaxBytes: options.MaxInputBytes, MaxPixels: options.MaxPixels, Width: page.Width, Height: page.Height, Color: page.Color})
	if err != nil {
		return nil, err
	}
	if info.MediaType == "image/"+format && page.Rotation == 0 {
		if int64(len(raw)) > options.MaxOutputBytes {
			return nil, ErrLimit
		}
		return bytes.Clone(raw), nil
	}
	width, height := page.Width, page.Height
	if page.Rotation == 90 || page.Rotation == 270 {
		width, height = height, width
	}
	var output draw.Image
	bounds := image.Rect(0, 0, width, height)
	if page.Color == "gray" {
		output = image.NewGray(bounds)
	} else {
		output = image.NewRGBA(bounds)
	}
	for y := 0; y < page.Height; y++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < page.Width; x++ {
			dx, dy := x, y
			switch page.Rotation {
			case 90:
				dx, dy = page.Height-1-y, x
			case 180:
				dx, dy = page.Width-1-x, page.Height-1-y
			case 270:
				dx, dy = y, page.Width-1-x
			}
			output.Set(dx, dy, input.At(x, y))
		}
	}
	var bitmap image.Image = output
	encoded := &bounded{limit: options.MaxOutputBytes, ctx: ctx}
	if format == "jpeg" {
		err = jpeg.Encode(encoded, bitmap, &jpeg.Options{Quality: options.JPEGQuality})
	} else {
		err = png.Encode(encoded, bitmap)
	}
	if err != nil {
		return nil, err
	}
	// Verify the finished encoded bitmap before it reaches publication.
	if _, err = captureimage.Validate(ctx, bytes.NewReader(encoded.data.Bytes()), captureimage.Options{MaxBytes: options.MaxOutputBytes, MaxPixels: options.MaxPixels, Width: width, Height: height, Color: page.Color}); err != nil {
		return nil, err
	}
	return encoded.data.Bytes(), nil
}

// ReadBounded reads a scoped stream without letting encoded input grow unbounded.
func ReadBounded(ctx context.Context, input io.Reader, limit int64) ([]byte, error) {
	if input == nil || limit < 1 || limit > 1<<30 {
		return nil, ErrInvalid
	}
	buffer := &bounded{limit: limit, ctx: ctx}
	if _, err := io.Copy(buffer, io.LimitReader(input, limit+1)); err != nil {
		return nil, err
	}
	return buffer.data.Bytes(), nil
}
