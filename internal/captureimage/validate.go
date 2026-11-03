// Package captureimage validates staged scanner bitmaps before record publication.
// It does not acquire devices or certify optical DPI: the adapter supplies exact
// expected dimensions from its validated capture/geometry settings.
package captureimage

import (
	"context"
	"crypto/sha256"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
)

var (
	ErrImage = errors.New("invalid or unsupported captured image")
	ErrLimit = errors.New("captured image exceeds limits")
)

type Options struct {
	MaxBytes, MaxPixels int64
	Width, Height       int
	Color               string
}
type Info struct {
	Width, Height int
	MediaType     string
	Size          int64
	SHA256        [32]byte
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

// Validate reads the whole immutable staged bitmap, bounds dimensions before full
// allocation, fully decodes it, and returns actual size/hash and image metadata.
// The caller must provide a private regular file (or trusted memory fixture), stop
// all writers first, and sync/close it before handing metadata to the DB service.
// Decode runs in the resource-limited worker, never in an HTTP request process.
func Validate(ctx context.Context, input io.ReadSeeker, options Options) (Info, error) {
	info, _, err := Decode(ctx, input, options)
	return info, err
}

// Decode applies exactly the Validate contract and returns the verified decoded
// bitmap for worker consumers, avoiding a second full-size image allocation.
// On failure no decoded bitmap is returned. The input is rewound on success.
func Decode(ctx context.Context, input io.ReadSeeker, options Options) (Info, image.Image, error) {
	var out Info
	if input == nil || options.MaxBytes < 1 || options.MaxBytes > 1<<40 || options.MaxPixels < 1 || options.Width < 1 || options.Height < 1 || (options.Color != "gray" && options.Color != "color") {
		return out, nil, ErrImage
	}
	if int64(options.Width) > options.MaxPixels/int64(options.Height) {
		return out, nil, ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return out, nil, err
	}
	size, err := input.Seek(0, io.SeekEnd)
	if err != nil || size <= 0 {
		return out, nil, ErrImage
	}
	if size > options.MaxBytes {
		return out, nil, ErrLimit
	}
	seek := func() error {
		_, err := input.Seek(0, io.SeekStart)
		if err != nil {
			return ErrImage
		}
		return nil
	}
	if err = seek(); err != nil {
		return out, nil, err
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(contextReader{ctx, input}, options.MaxBytes+1))
	if ctx.Err() != nil {
		return out, nil, ctx.Err()
	}
	if err != nil || n != size {
		return out, nil, ErrImage
	}
	if n > options.MaxBytes {
		return out, nil, ErrLimit
	}
	if err = seek(); err != nil {
		return out, nil, err
	}
	cfg, format, err := image.DecodeConfig(contextReader{ctx, io.LimitReader(input, options.MaxBytes)})
	if ctx.Err() != nil {
		return out, nil, ctx.Err()
	}
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 {
		return out, nil, ErrImage
	}
	if int64(cfg.Width) > options.MaxPixels/int64(cfg.Height) {
		return out, nil, ErrLimit
	}
	if cfg.Width != options.Width || cfg.Height != options.Height {
		return out, nil, ErrImage
	}
	if err = seek(); err != nil {
		return out, nil, err
	}
	bitmap, decodedFormat, err := image.Decode(contextReader{ctx, io.LimitReader(input, options.MaxBytes)})
	if ctx.Err() != nil {
		return out, nil, ctx.Err()
	}
	if err != nil || decodedFormat != format || bitmap.Bounds().Dx() != cfg.Width || bitmap.Bounds().Dy() != cfg.Height {
		return out, nil, ErrImage
	}
	mode := ""
	switch bitmap.(type) {
	case *image.Gray:
		mode = "gray"
	case *image.RGBA, *image.NRGBA, *image.YCbCr:
		mode = "color"
	default:
		return out, nil, ErrImage
	}
	if mode != options.Color {
		return out, nil, ErrImage
	}
	if opaque, ok := bitmap.(interface{ Opaque() bool }); !ok || !opaque.Opaque() {
		return out, nil, ErrImage
	}
	out = Info{Width: cfg.Width, Height: cfg.Height, MediaType: "image/" + format, Size: size}
	copy(out.SHA256[:], digest.Sum(nil))
	if err = seek(); err != nil {
		return Info{}, nil, err
	}
	return out, bitmap, nil
}
