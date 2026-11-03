// Package pdfassemble creates image PDFs using the unmodified upstream pdfcpu
// library. It operates on ordered job-scoped input streams, never browser paths.
package pdfassemble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"scanner-kiosk/internal/captureimage"
)

var (
	ErrInvalid = errors.New("invalid PDF assembly request")
	ErrLimit   = errors.New("PDF assembly limit exceeded")
	ErrFailed  = errors.New("PDF assembly failed")
)

type Options struct {
	MaxInputBytes, MaxOutputBytes, MaxPagePixels int64
	MaxPages, JPEGQuality                        int
}
type Page struct {
	ID, Size, Color         string
	Width, Height, Rotation int
	Open                    func(context.Context) (io.ReadCloser, error)
}
type limitedBuffer struct {
	data  bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Len() int      { return b.data.Len() }
func (b *limitedBuffer) Bytes() []byte { return b.data.Bytes() }
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(b.Len()) {
		return 0, ErrLimit
	}
	return b.data.Write(p)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Build returns a bounded, structurally validated PDF. Encoding is jpeg or
// lossless. JPEG input is preserved byte-for-byte in jpeg mode; PNG pixels are
// preserved in lossless mode. Re-encoding never modifies an original artifact.
// Run in the limited worker/native processing process, not the HTTP process.
func Build(ctx context.Context, pages []Page, encoding string, options Options) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(pages) == 0 || len(pages) > options.MaxPages || options.MaxPages < 1 || options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<30 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 1<<30 || options.MaxPagePixels < 1 || options.JPEGQuality < 1 || options.JPEGQuality > 100 || (encoding != "jpeg" && encoding != "lossless") {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, page := range pages {
		if page.ID == "" || seen[page.ID] || page.Open == nil || page.Width < 1 || page.Height < 1 || int64(page.Width) > options.MaxPagePixels/int64(page.Height) || (page.Color != "color" && page.Color != "gray") || (page.Rotation != 0 && page.Rotation != 90 && page.Rotation != 180 && page.Rotation != 270) {
			return nil, ErrInvalid
		}
		if _, _, ok := paper(page.Size); !ok {
			return nil, ErrInvalid
		}
		seen[page.ID] = true
	}
	inputs := make([]io.ReadSeeker, 0, len(pages))
	var total int64
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input, err := page.Open(ctx)
		if err != nil || input == nil {
			return nil, ErrFailed
		}
		raw, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, input}, options.MaxInputBytes+1))
		closeErr := input.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if readErr != nil || closeErr != nil {
			return nil, ErrFailed
		}
		if int64(len(raw)) > options.MaxInputBytes {
			return nil, ErrLimit
		}
		info, err := captureimage.Validate(ctx, bytes.NewReader(raw), captureimage.Options{MaxBytes: options.MaxInputBytes, MaxPixels: options.MaxPagePixels, Width: page.Width, Height: page.Height, Color: page.Color})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrInvalid
		}
		prepared := raw
		transcode := (encoding == "jpeg" && info.MediaType != "image/jpeg") || (encoding == "lossless" && info.MediaType != "image/png")
		if transcode {
			bitmap, _, err := image.Decode(contextReader{ctx, bytes.NewReader(raw)})
			if err != nil {
				return nil, ErrFailed
			}
			encoded := &limitedBuffer{limit: options.MaxInputBytes}
			if encoding == "jpeg" {
				err = jpeg.Encode(encoded, bitmap, &jpeg.Options{Quality: options.JPEGQuality})
			} else {
				// JPEG decoders return YCbCr; normalize to explicit 8-bit RGB.
				rgb := image.NewRGBA(bitmap.Bounds())
				draw.Draw(rgb, rgb.Bounds(), bitmap, bitmap.Bounds().Min, draw.Src)
				err = png.Encode(encoded, rgb)
			}
			if err != nil {
				if errors.Is(err, ErrLimit) {
					return nil, ErrLimit
				}
				return nil, ErrFailed
			}
			prepared = encoded.Bytes()
		}
		width, height, _ := paper(page.Size)
		details, err := pdfcpu.ParseImportDetails(fmt.Sprintf("dimensions:%.8f %.8f, position:c, scalefactor:1 rel", width, height), types.POINTS)
		if err != nil {
			return nil, ErrFailed
		}
		conf := model.NewStatelessConfiguration()
		single := &limitedBuffer{limit: options.MaxOutputBytes - total}
		if err = api.ImportImages(ctx, nil, single, []io.Reader{bytes.NewReader(prepared)}, details, conf); err != nil {
			return nil, normalized(ctx, err)
		}
		encodedPage := single.Bytes()
		if page.Rotation != 0 {
			rotated := &limitedBuffer{limit: options.MaxOutputBytes - total}
			if err = api.Rotate(ctx, bytes.NewReader(encodedPage), rotated, page.Rotation, []string{"1"}, conf); err != nil {
				return nil, normalized(ctx, err)
			}
			encodedPage = rotated.Bytes()
		}
		total += int64(len(encodedPage))
		inputs = append(inputs, bytes.NewReader(encodedPage))
	}
	output := &limitedBuffer{limit: options.MaxOutputBytes}
	conf := model.NewStatelessConfiguration()
	if err := api.MergeRaw(ctx, inputs, output, false, conf); err != nil {
		return nil, normalized(ctx, err)
	}
	if err := api.Validate(ctx, bytes.NewReader(output.Bytes()), conf, nil); err != nil {
		return nil, normalized(ctx, err)
	}
	count, err := api.PageCount(ctx, bytes.NewReader(output.Bytes()), conf)
	if err != nil || count != len(pages) {
		return nil, ErrFailed
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
func normalized(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrLimit) {
		return ErrLimit
	}
	return ErrFailed
}
func paper(size string) (float64, float64, bool) {
	var w, h float64
	switch size {
	case "a4":
		w, h = 210, 297
	case "a5":
		w, h = 148, 210
	case "a6":
		w, h = 105, 148
	case "letter":
		w, h = 215.9, 279.4
	case "photo10x15":
		w, h = 100, 150
	default:
		return 0, 0, false
	}
	return w / 25.4 * 72, h / 25.4 * 72, true
}
