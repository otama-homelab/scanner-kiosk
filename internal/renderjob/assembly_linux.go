//go:build linux

package renderjob

import (
	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
)

func assemblyRequest(work documents.RenderWork, inputs map[string]artifactscope.Input, pages []pdfassemble.ScopedPage, options pdfassemble.Options) pdfassemble.ProcessRequest {
	request := pdfassemble.ProcessRequest{Encoding: work.Snapshot.Settings.PDFEncoding, Options: options, Pages: make([]pdfassemble.ProcessPage, len(pages))}
	for i, page := range pages {
		input := inputs[page.ID]
		request.Pages[i] = pdfassemble.ProcessPage{ID: work.Snapshot.Pages[i].ID, Size: page.Size, Color: page.Color, MediaType: page.MediaType, Width: page.Width, Height: page.Height, Rotation: page.Rotation, Input: artifactread.Input{RelativePath: input.RelativePath, Size: input.Size, SHA256: input.SHA256}}
	}
	return request
}
