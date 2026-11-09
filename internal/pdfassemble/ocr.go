package pdfassemble

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/pdftextlayer"
)

// TextLayer binds an image-free PDF layer to a stable page ID, never an ordinal.
// The coordinator validates provider model/result identities and supplies scoped
// openers. OCR input must be unrotated; page rotation applies after composition.
type TextLayer struct {
	PageID string
	Open   func(context.Context) (io.ReadCloser, error)
}

// BuildWithLayers adds bound OCR layers without rasterizing imported PDFs,
// then rotates and merges complete pages. No partial PDF is returned.
func BuildWithLayers(ctx context.Context, pages []Page, layers []TextLayer, encoding string, options Options) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(pages) == 0 || len(pages) > options.MaxPages || options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<30 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 1<<30 {
		return nil, ErrInvalid
	}
	byID := make(map[string]TextLayer, len(layers))
	for _, layer := range layers {
		if layer.PageID == "" || layer.Open == nil {
			return nil, ErrInvalid
		}
		if _, exists := byID[layer.PageID]; exists {
			return nil, ErrInvalid
		}
		byID[layer.PageID] = layer
	}
	seen := map[string]bool{}
	layerCount := 0
	for _, page := range pages {
		if page.ID == "" || seen[page.ID] {
			return nil, ErrInvalid
		}
		_, exists := byID[page.ID]
		if page.MediaType != "application/pdf" {
			if !exists {
				return nil, ErrInvalid
			}
		}
		if exists {
			layerCount++
		}
		seen[page.ID] = true
	}
	if len(byID) != layerCount {
		return nil, ErrInvalid
	}
	inputs := make([]io.ReadSeeker, 0, len(pages))
	var total int64
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := options.MaxOutputBytes - total
		if remaining < 1 {
			return nil, ErrLimit
		}
		if _, exists := byID[page.ID]; page.MediaType == "application/pdf" && !exists {
			policy := options
			policy.MaxOutputBytes = remaining
			retained, err := Build(ctx, []Page{page}, encoding, policy)
			if err != nil {
				return nil, err
			}
			total += int64(len(retained))
			inputs = append(inputs, bytes.NewReader(retained))
			continue
		}
		angle := page.Rotation
		if angle != 0 && angle != 90 && angle != 180 && angle != 270 {
			return nil, ErrInvalid
		}
		unrotated := page
		unrotated.Rotation = 0
		policy := options
		policy.MaxOutputBytes = remaining
		base, err := Build(ctx, []Page{unrotated}, encoding, policy)
		if err != nil {
			return nil, err
		}
		input, err := byID[page.ID].Open(ctx)
		if err != nil || input == nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrFailed
		}
		layer, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, input}, options.MaxInputBytes+1))
		closeErr := input.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if readErr != nil || closeErr != nil {
			return nil, ErrFailed
		}
		if int64(len(layer)) > options.MaxInputBytes {
			return nil, ErrLimit
		}
		var composed []byte
		if page.MediaType == "application/pdf" {
			composed, err = pdftextlayer.OverlayPDF(ctx, base, layer, remaining)
		} else {
			composed, err = pdftextlayer.Overlay(ctx, base, layer, page.Width, page.Height, remaining)
		}
		if errors.Is(err, pdftextlayer.ErrLimit) {
			return nil, ErrLimit
		}
		if err != nil {
			return nil, err
		}
		if angle != 0 {
			rotated := &limitedBuffer{limit: remaining}
			if err = api.Rotate(ctx, bytes.NewReader(composed), rotated, angle, []string{"1"}, model.NewStatelessConfiguration()); err != nil {
				return nil, normalized(ctx, err)
			}
			composed = rotated.Bytes()
		}
		total += int64(len(composed))
		inputs = append(inputs, bytes.NewReader(composed))
	}
	output := &limitedBuffer{limit: options.MaxOutputBytes}
	conf := model.NewStatelessConfiguration()
	conf.Optimize = false
	if err := api.MergeRaw(ctx, inputs, output, false, conf); err != nil {
		return nil, normalized(ctx, err)
	}
	if err := api.Validate(ctx, bytes.NewReader(output.Bytes()), conf, nil); err != nil {
		return nil, normalized(ctx, err)
	}
	count, err := api.PageCount(ctx, bytes.NewReader(output.Bytes()), conf)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	if count != len(pages) {
		return nil, ErrFailed
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
