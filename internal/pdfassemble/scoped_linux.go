//go:build linux

package pdfassemble

import (
	"context"
	"io"
	"scanner-kiosk/internal/artifactscope"
)

// ScopedPage describes an input from the coordinator's authorized job snapshot.
// GenerateScoped resolves IDs through the scope; callers cannot supply paths.
type ScopedPage struct {
	ID, Size, Color         string
	Width, Height, Rotation int
}

// GenerateScoped writes an ordinary image PDF, never a searchable-PDF OCR output.
// The caller owns seal, publication/read-back, release and failed-job cleanup.
func GenerateScoped(ctx context.Context, scope *artifactscope.Scope, ordered []ScopedPage, encoding string, options Options) (string, error) {
	if scope == nil {
		return "", ErrInvalid
	}
	pages := make([]Page, len(ordered))
	for i, page := range ordered {
		id := page.ID
		pages[i] = Page{ID: id, Size: page.Size, Color: page.Color, Width: page.Width, Height: page.Height, Rotation: page.Rotation, Open: func(ctx context.Context) (io.ReadCloser, error) { return scope.OpenInput(ctx, id) }}
	}
	data, err := Build(ctx, pages, encoding, options)
	if err != nil {
		return "", err
	}
	writer, err := scope.CreateArtifact(ctx, artifactscope.PDF)
	if err != nil {
		return "", err
	}
	_, writeErr := writer.Write(data)
	closeErr := writer.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return writer.Reference(), nil
}
