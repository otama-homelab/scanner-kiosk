package pdfassemble

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// The caller supplies a fully decoded, bounded, opaque captureimage bitmap.
// Copy concrete rows to avoid pdfcpu's per-pixel interface allocations.
func losslessSamples(ctx context.Context, bitmap image.Image) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	bounds := bitmap.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if _, ok := bitmap.(*image.YCbCr); ok {
		rgb := image.NewRGBA(bounds)
		draw.Draw(rgb, bounds, bitmap, bounds.Min, draw.Src)
		bitmap = rgb
	}
	channels := 3
	space := model.DeviceRGBCS
	if _, ok := bitmap.(*image.Gray); ok {
		channels = 1
		space = model.DeviceGrayCS
	}
	out := make([]byte, width*height*channels)
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		row := out[y*width*channels : (y+1)*width*channels]
		switch p := bitmap.(type) {
		case *image.Gray:
			i := p.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			copy(row, p.Pix[i:i+width])
		case *image.RGBA:
			i := p.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			for x := 0; x < width; x++ {
				copy(row[x*3:x*3+3], p.Pix[i+x*4:i+x*4+3])
			}
		case *image.NRGBA:
			i := p.PixOffset(bounds.Min.X, bounds.Min.Y+y)
			for x := 0; x < width; x++ {
				copy(row[x*3:x*3+3], p.Pix[i+x*4:i+x*4+3])
			}
		default:
			return nil, "", ErrInvalid
		}
	}
	return out, space, nil
}

func losslessPagePDF(ctx context.Context, size string, bitmap image.Image, limit int64) ([]byte, error) {
	samples, space, err := losslessSamples(ctx, bitmap)
	if err != nil {
		return nil, err
	}
	width, height, _ := paper(size)
	conf := model.NewStatelessConfiguration()
	conf.Optimize = false
	document, err := pdfcpu.CreateContextWithXRefTable(conf, &types.Dim{Width: width, Height: height})
	if err != nil {
		return nil, normalized(ctx, err)
	}
	bounds := bitmap.Bounds()
	stream, err := model.CreateFlateImageStreamDict(document.XRefTable, samples, nil, bounds.Dx(), bounds.Dy(), 8, space)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	imageRef, err := document.IndRefForNewObject(*stream)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	scale := math.Min(width/float64(bounds.Dx()), height/float64(bounds.Dy()))
	w, h := float64(bounds.Dx())*scale, float64(bounds.Dy())*scale
	content, err := document.NewStreamDictForBuf([]byte(fmt.Sprintf("q %.5f 0 0 %.5f %.5f %.5f cm /Im0 Do Q", w, h, (width-w)/2, (height-h)/2)))
	if err != nil {
		return nil, normalized(ctx, err)
	}
	if err = content.Encode(); err != nil {
		return nil, normalized(ctx, err)
	}
	contentRef, err := document.IndRefForNewObject(*content)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	parent, err := document.Pages()
	if err != nil {
		return nil, normalized(ctx, err)
	}
	pages, err := document.DereferenceDict(*parent)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	page := types.Dict{"Type": types.Name("Page"), "Parent": *parent, "MediaBox": types.RectForDim(width, height).Array(), "Resources": types.Dict{"XObject": types.Dict{"Im0": *imageRef}}, "Contents": *contentRef}
	pageRef, err := document.IndRefForNewObject(page)
	if err != nil {
		return nil, normalized(ctx, err)
	}
	if err = document.SetValid(*pageRef); err != nil {
		return nil, normalized(ctx, err)
	}
	if err = model.AppendPageTree(pageRef, 1, pages); err != nil {
		return nil, normalized(ctx, err)
	}
	document.PageCount = 1
	output := &limitedBuffer{limit: limit}
	if err = api.Write(ctx, document, output, conf); err != nil {
		return nil, normalized(ctx, err)
	}
	return output.Bytes(), nil
}
