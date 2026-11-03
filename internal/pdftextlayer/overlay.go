// Package pdftextlayer overlays trusted OCR text-only PDFs on image PDFs using
// unmodified upstream pdfcpu. Provider/result ownership validation is separate.
package pdftextlayer

import (
	"bytes"
	"context"
	"errors"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"io"
	"math"
)

var ErrInvalid = errors.New("invalid OCR PDF layer")
var ErrLimit = errors.New("OCR PDF composition limit exceeded")
var ErrFailed = errors.New("OCR PDF composition failed")

func init() {
	// PDF watermark imports still load a default configuration despite the explicit stateless configuration.
	api.DisableConfigDir()
}

type bounded struct {
	data  bytes.Buffer
	limit int64
}

func (b *bounded) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(b.data.Len()) {
		return 0, ErrLimit
	}
	return b.data.Write(p)
}

// Overlay composes exactly one unrotated image page and one image-free OCR layer.
// Width/height describe the bitmap recognized by OCR. Both are centered and fitted
// proportionally, matching image import. Rotate the combined page only afterwards.
// Layers must originate from a trusted invisible-text renderer; image-free PDF
// structure alone is not proof that arbitrary PDF graphics are invisible.
func Overlay(ctx context.Context, base, layer []byte, width, height int, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width < 1 || height < 1 || maxBytes < 1 || maxBytes > 1<<30 {
		return nil, ErrInvalid
	}
	if len(base) == 0 || len(layer) == 0 || int64(len(base)) > maxBytes || int64(len(layer)) > maxBytes {
		return nil, ErrLimit
	}
	conf := model.NewStatelessConfiguration()
	for index, data := range [][]byte{base, layer} {
		count, err := api.PageCount(ctx, bytes.NewReader(data), conf)
		if err != nil {
			return nil, normalize(ctx, err)
		}
		if count != 1 {
			return nil, ErrInvalid
		}
		boxes, err := api.Boxes(ctx, bytes.NewReader(data), nil, conf)
		if err != nil {
			return nil, normalize(ctx, err)
		}
		if len(boxes) != 1 || boxes[0].Rot != 0 {
			return nil, ErrInvalid
		}
		media, crop := boxes[0].MediaBox(), boxes[0].CropBox()
		if media == nil || crop == nil || media.Width() <= 0 || media.Height() <= 0 || media.LL.X != 0 || media.LL.Y != 0 || math.Abs(crop.Width()-media.Width()) > .01 || math.Abs(crop.Height()-media.Height()) > .01 || math.Abs(crop.LL.X-media.LL.X) > .01 || math.Abs(crop.LL.Y-media.LL.Y) > .01 {
			return nil, ErrInvalid
		}
		images, err := api.Images(ctx, bytes.NewReader(data), nil, conf)
		if err != nil {
			return nil, normalize(ctx, err)
		}
		n := 0
		for _, page := range images {
			n += len(page)
		}
		if (index == 0 && n != 1) || (index == 1 && n != 0) {
			return nil, ErrInvalid
		}
		if index == 1 {
			ratio := media.Width() / media.Height()
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) || math.Abs(ratio/(float64(width)/float64(height))-1) > .001 {
				return nil, ErrInvalid
			}
		}
	}
	stamp, err := api.PDFWatermarkForReadSeeker(ctx, bytes.NewReader(layer), 1, "position:c, scalefactor:1 rel, rotation:0, opacity:1", true, false, types.POINTS)
	if err != nil {
		return nil, normalize(ctx, err)
	}
	output := &bounded{limit: maxBytes}
	if err = api.AddWatermarks(ctx, bytes.NewReader(base), output, []string{"1"}, stamp, conf); err != nil {
		return nil, normalize(ctx, err)
	}
	if err = api.Validate(ctx, bytes.NewReader(output.data.Bytes()), conf, nil); err != nil {
		return nil, normalize(ctx, err)
	}
	if count, err := api.PageCount(ctx, bytes.NewReader(output.data.Bytes()), conf); err != nil || count != 1 {
		if err != nil {
			return nil, normalize(ctx, err)
		}
		return nil, ErrFailed
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return output.data.Bytes(), nil
}
func normalize(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrLimit) {
		return ErrLimit
	}
	return ErrFailed
}

var _ io.Writer = (*bounded)(nil)
