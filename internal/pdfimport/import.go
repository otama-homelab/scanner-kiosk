// Package pdfimport prepares bounded standalone PDF pages without rasterizing
// text or vectors. Call it only in the limited processing worker, never HTTP.
package pdfimport

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

var (
	ErrInvalid   = errors.New("invalid or unsupported PDF")
	ErrLimit     = errors.New("PDF import limit exceeded")
	ErrUnsafe    = errors.New("PDF contains unsupported interactive or external content")
	ErrEncrypted = errors.New("password-protected PDFs cannot be imported")
)

type Options struct {
	MaxInputBytes, MaxOutputBytes int64
	MaxPages                      int
}
type Page struct {
	PDF                       []byte
	WidthPoints, HeightPoints float64
}
type boundedBuffer struct {
	data      bytes.Buffer
	remaining int64
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.remaining {
		return 0, ErrLimit
	}
	n, err := b.data.Write(p)
	b.remaining -= int64(n)
	return n, err
}

// Split returns all pages or no result. Original bytes, page geometry, text and
// vector commands remain untouched; output is a structural PDF page extraction.
// The caller owns byte/page quotas and OS CPU/RAM/process limits for this work.
func Split(ctx context.Context, input []byte, options Options) ([]Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<30 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 1<<30 || options.MaxPages < 1 || options.MaxPages > 10000 {
		return nil, ErrInvalid
	}
	if int64(len(input)) > options.MaxInputBytes {
		return nil, ErrLimit
	}
	if len(input) == 0 {
		return nil, ErrInvalid
	}
	configuration := model.NewStatelessConfiguration()
	configuration.Optimize = false
	configuration.Limits = model.ResourceLimits{MaxInputBytes: options.MaxInputBytes, MaxObjectBytes: 4 << 20, MaxStreamBytes: min(options.MaxInputBytes, 64<<20), MaxDecodeBytes: 16 << 20, MaxImagePixels: 64 << 20, MaxImageBytes: 128 << 20, MaxObjectCount: 100000, MaxObjectStreamCount: 20000, MaxObjectStreamFirst: 4 << 20, MaxXRefEntries: 100000, MaxRecursionDepth: 64}
	parsed, err := api.ReadContext(ctx, bytes.NewReader(input), configuration)
	if err != nil {
		return nil, normalize(ctx, err)
	}
	if parsed.Encrypt != nil {
		return nil, ErrEncrypted
	}
	catalog, err := parsed.Catalog()
	if err != nil {
		return nil, normalize(ctx, err)
	}
	if err = checkObject(ctx, parsed.XRefTable, catalog, 0); err != nil {
		return nil, err
	}
	for _, entry := range parsed.Table {
		if entry != nil && !entry.Free {
			if err = checkObject(ctx, parsed.XRefTable, entry.Object, 0); err != nil {
				return nil, err
			}
		}
	}
	if err = api.ValidateContext(ctx, parsed); err != nil {
		return nil, normalize(ctx, err)
	}
	if parsed.PageCount < 1 || parsed.PageCount > options.MaxPages {
		return nil, ErrLimit
	}
	dimensions, err := parsed.PageDims(ctx)
	if err != nil {
		return nil, normalize(ctx, err)
	}
	if len(dimensions) != parsed.PageCount {
		return nil, ErrInvalid
	}
	out := make([]Page, 0, parsed.PageCount)
	var total int64
	for i, dimension := range dimensions {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if !finiteDimension(dimension.Width) || !finiteDimension(dimension.Height) {
			return nil, ErrLimit
		}
		buffer := &boundedBuffer{remaining: options.MaxOutputBytes - total}
		if err = api.Trim(ctx, bytes.NewReader(input), buffer, []string{strconv.Itoa(i + 1)}, configuration); err != nil {
			return nil, normalize(ctx, err)
		}
		raw := buffer.data.Bytes()
		if len(raw) == 0 {
			return nil, ErrInvalid
		}
		total += int64(len(raw))
		out = append(out, Page{PDF: raw, WidthPoints: dimension.Width, HeightPoints: dimension.Height})
	}
	return out, nil
}
func finiteDimension(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 14400
}
func normalize(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrLimit) || errors.Is(err, model.ErrInputSizeLimit) || errors.Is(err, model.ErrMaxRecursionDepthExceeded) || errors.Is(err, filter.ErrDecodeLimitExceeded) {
		return ErrLimit
	}
	if errors.Is(err, pdfcpu.ErrEncrypted) || errors.Is(err, pdfcpu.ErrWrongPassword) {
		return ErrEncrypted
	}
	return ErrInvalid
}
func decodedName(value string) string {
	if decoded, err := types.DecodeName(value); err == nil {
		return decoded
	}
	return value
}

// Indirect objects are visited once through the xref table; only semantic scalar
// names are dereferenced here, so page-tree Parent links cannot create recursion.
func checkObject(ctx context.Context, xref *model.XRefTable, object types.Object, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return ErrLimit
	}
	switch value := object.(type) {
	case types.Dict:
		for rawKey, child := range value {
			key := decodedName(rawKey)
			switch key {
			case "OpenAction", "AA", "JS", "JavaScript", "EmbeddedFiles", "EF", "XFA", "AcroForm", "RichMediaContent", "RichMediaSettings", "Collection", "3DD", "Ref", "OPI", "FFilter", "FDecodeParms":
				return ErrUnsafe
			}
			if key == "S" || key == "Type" || key == "Subtype" {
				resolved, err := xref.Dereference(child)
				if err != nil {
					return normalize(ctx, err)
				}
				if name, ok := resolved.(types.Name); ok {
					nameValue := decodedName(string(name))
					if key == "S" {
						switch nameValue {
						case "JavaScript", "Launch", "GoToR", "GoToE", "URI", "SubmitForm", "ImportData", "Rendition", "Movie", "Sound":
							return ErrUnsafe
						}
					}
					if key == "Type" && (nameValue == "Filespec" || nameValue == "EmbeddedFile") {
						return ErrUnsafe
					}
					if key == "Subtype" {
						switch nameValue {
						case "PS", "RichMedia", "3D", "Movie", "Screen", "Sound", "Widget":
							return ErrUnsafe
						}
					}
				}
			}
			if err := checkObject(ctx, xref, child, depth+1); err != nil {
				return err
			}
		}
	case types.Array:
		for _, child := range value {
			if err := checkObject(ctx, xref, child, depth+1); err != nil {
				return err
			}
		}
	case types.StreamDict:
		for key := range value.Dict {
			if decodedName(key) == "F" {
				return ErrUnsafe
			}
		}
		return checkObject(ctx, xref, value.Dict, depth+1)
	case *types.StreamDict:
		if value != nil {
			return checkObject(ctx, xref, *value, depth+1)
		}
	}
	return nil
}
