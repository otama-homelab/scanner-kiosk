// Evaluation glue only: both engines use the production compositor unchanged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/pkg/ocrplugin"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("OCR composition unavailable\n")
		os.Exit(1)
	}
}
func run() error {
	var request struct {
		Pages  []ocrplugin.PositionedPage `json:"pages"`
		Images []string                   `json:"images"`
		Font   string                     `json:"font"`
		Output string                     `json:"output"`
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if len(request.Pages) < 1 || len(request.Pages) > 256 || len(request.Images) != len(request.Pages) {
		return io.ErrUnexpectedEOF
	}
	font, err := os.ReadFile(request.Font)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pages := []pdfassemble.Page{}
	layers := []pdfassemble.TextLayer{}
	for i, page := range request.Pages {
		input := ocrplugin.InputPage{ID: page.PageID, Width: page.Width, Height: page.Height}
		layer, err := pdftextlayer.RenderPositioned(ctx, page, input, font, 64<<20)
		if err != nil {
			return err
		}
		imagePath := request.Images[i]
		pages = append(pages, pdfassemble.Page{ID: page.PageID, Size: "a4", Color: "color", Width: page.Width, Height: page.Height, MediaType: "image/png", Open: func(context.Context) (io.ReadCloser, error) { return os.Open(imagePath) }})
		layers = append(layers, pdfassemble.TextLayer{PageID: page.PageID, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(layer)), nil }})
	}
	data, err := pdfassemble.BuildWithLayers(ctx, pages, layers, "lossless", pdfassemble.Options{MaxInputBytes: 200 << 20, MaxOutputBytes: 200 << 20, MaxPagePixels: 80_000_000, MaxPages: 256, JPEGQuality: 90})
	if err != nil {
		return err
	}
	return os.WriteFile(request.Output, data, 0600)
}
