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
	return overlay(ctx, base, layer, width, height, maxBytes, false)
}

// OverlayPDF fits a trusted OCR layer to the original PDF's unrotated CropBox.
// Original content, page boxes and intrinsic rotation are retained.
func OverlayPDF(ctx context.Context, base, layer []byte, maxBytes int64) ([]byte, error) {
	return overlay(ctx, base, layer, 0, 0, maxBytes, true)
}

const TextOriginRetainedSource = "retained-source"
const TextOriginRecognition = "recognition"

// PDFTextOrigin inspects a bounded single-page source in the native isolation
// boundary. Any reachable font, including fonts in Forms, conservatively retains
// source text. This is provenance, not a claim that extracted text equals OCR.
func PDFTextOrigin(ctx context.Context, source []byte, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes < 1 || maxBytes > 1<<30 || len(source) == 0 || int64(len(source)) > maxBytes {
		return "", ErrLimit
	}
	conf := model.NewStatelessConfiguration()
	parsed, err := api.ReadValidateAndOptimize(ctx, bytes.NewReader(source), conf, nil)
	if err != nil {
		return "", normalize(ctx, err)
	}
	if parsed.PageCount != 1 {
		return "", ErrInvalid
	}
	if _, _, _, err = parsed.PageDict(ctx, 1, true); err != nil {
		return "", normalize(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(parsed.Optimize.FontObjects) > 0 || len(parsed.Optimize.FormFontObjects) > 0 {
		return TextOriginRetainedSource, nil
	}
	return TextOriginRecognition, nil
}

func overlay(ctx context.Context, base, layer []byte, width, height int, maxBytes int64, imported bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (!imported && (width < 1 || height < 1)) || maxBytes < 1 || maxBytes > 1<<30 {
		return nil, ErrInvalid
	}
	if len(base) == 0 || len(layer) == 0 || int64(len(base)) > maxBytes || int64(len(layer)) > maxBytes {
		return nil, ErrLimit
	}
	conf := model.NewStatelessConfiguration()
	conf.Optimize = false
	var rotation int
	var sourceRatio float64
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
		if len(boxes) != 1 || (boxes[0].Rot%90 != 0) || (index == 1 && boxes[0].Rot != 0) || (!imported && boxes[0].Rot != 0) {
			return nil, ErrInvalid
		}
		media, crop := boxes[0].MediaBox(), boxes[0].CropBox()
		if media == nil || crop == nil || !validRectangle(media) || !validRectangle(crop) || crop.LL.X < media.LL.X || crop.LL.Y < media.LL.Y || crop.UR.X > media.UR.X || crop.UR.Y > media.UR.Y {
			return nil, ErrInvalid
		}
		if index == 1 || !imported {
			if media.LL.X != 0 || media.LL.Y != 0 || math.Abs(crop.Width()-media.Width()) > .01 || math.Abs(crop.Height()-media.Height()) > .01 || math.Abs(crop.LL.X-media.LL.X) > .01 || math.Abs(crop.LL.Y-media.LL.Y) > .01 {
				return nil, ErrInvalid
			}
		}
		if index == 0 && imported {
			rotation = boxes[0].Rot
			sourceRatio = crop.Width() / crop.Height()
		}
		images, err := api.Images(ctx, bytes.NewReader(data), nil, conf)
		if err != nil {
			return nil, normalize(ctx, err)
		}
		n := 0
		for _, page := range images {
			n += len(page)
		}
		if (index == 0 && !imported && n != 1) || (index == 1 && n != 0) {
			return nil, ErrInvalid
		}
		if index == 1 {
			ratio := media.Width() / media.Height()
			expected := float64(width) / float64(height)
			if imported {
				expected = sourceRatio
			}
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) || math.Abs(ratio/expected-1) > .001 {
				return nil, ErrInvalid
			}
		}
	}
	// pdfcpu flattens rotated watermarked pages; remove and restore only the
	// rotation metadata so the original MediaBox/CropBox remain unchanged.
	if rotation != 0 {
		unrotated := &bounded{limit: maxBytes}
		if err := api.Rotate(ctx, bytes.NewReader(base), unrotated, -rotation, []string{"1"}, conf); err != nil {
			return nil, normalize(ctx, err)
		}
		base = unrotated.data.Bytes()
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
	if rotation != 0 {
		rotated := &bounded{limit: maxBytes}
		if err = api.Rotate(ctx, bytes.NewReader(output.data.Bytes()), rotated, rotation, []string{"1"}, conf); err != nil {
			return nil, normalize(ctx, err)
		}
		output = rotated
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

func validRectangle(r *types.Rectangle) bool {
	for _, v := range []float64{r.LL.X, r.LL.Y, r.UR.X, r.UR.Y} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 14400 {
			return false
		}
	}
	return r.Width() > 0 && r.Height() > 0 && r.Width() <= 14400 && r.Height() <= 14400
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
