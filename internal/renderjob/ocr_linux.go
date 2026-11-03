//go:build linux

package renderjob

import (
	"context"
	"errors"
	"io"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/ocrresult"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/pkg/ocrplugin"
)

// OCRSelector supplies configured providers through the common versioned port.
// Render jobs pin provider/model/version/settings; no fallback is selected here.
type OCRSelector interface {
	SelectOCR(context.Context, string, ocrplugin.Request) (ocrplugin.Provider, error)
}

func prepareOCR(ctx context.Context, selector OCRSelector, root string, work documents.RenderWork, inputs map[string]artifactscope.Input, pages []pdfassemble.ScopedPage, ocrScope *artifactscope.Scope, budget *artifactscope.Budget, options pdfassemble.Options) (final *artifactscope.Scope, reference string, err error) {
	settings := work.Snapshot.Settings.OCR
	req := ocrplugin.Request{JobID: work.Job.ID, InputRevision: work.Job.Revision, ModelID: settings.ModelID, ModelVersion: settings.ModelVersion, Languages: append([]string(nil), settings.Languages...), QualityPreset: settings.QualityPreset, Output: ocrplugin.PDFTextLayer}
	for i, page := range pages {
		req.Pages = append(req.Pages, ocrplugin.InputPage{ID: work.Snapshot.Pages[i].ID, ArtifactID: page.ID, Width: page.Width, Height: page.Height, DPI: work.Snapshot.Pages[i].DPI})
	}
	provider, err := selector.SelectOCR(ctx, settings.ProviderID, req)
	if err != nil {
		return nil, "", err
	}
	invocation := req
	invocation.Pages = append([]ocrplugin.InputPage(nil), req.Pages...)
	invocation.Languages = append([]string(nil), req.Languages...)
	result, err := provider.Run(ctx, invocation, ocrScope)
	if err != nil {
		return nil, "", err
	}
	sealed, err := ocrScope.Seal(ctx)
	if err != nil {
		return nil, "", err
	}
	bound, err := ocrresult.BindSealed(req, result, sealed, work.ReserveBytes)
	if err != nil {
		return nil, "", err
	}
	combined := make(map[string]artifactscope.Input, len(inputs)+len(bound))
	for id, input := range inputs {
		combined[id] = input
	}
	for _, page := range bound {
		a := page.Artifact
		combined[a.ID] = artifactscope.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, budget.Remaining())
	if options.MaxOutputBytes < 1 {
		return nil, "", artifactscope.ErrLimit
	}
	final, err = artifactscope.NewWithBudget(root, combined, artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: 1}, budget)
	if err != nil {
		if errors.Is(err, artifactscope.ErrStorage) {
			err = errors.Join(ocrplugin.ErrReconciliation, err)
		}
		return nil, "", err
	}
	images := make([]pdfassemble.Page, len(pages))
	layers := make([]pdfassemble.TextLayer, len(bound))
	for i, page := range pages {
		inputID := page.ID
		images[i] = pdfassemble.Page{ID: work.Snapshot.Pages[i].ID, Size: page.Size, Color: page.Color, Width: page.Width, Height: page.Height, Rotation: page.Rotation, Open: func(ctx context.Context) (io.ReadCloser, error) { return final.OpenInput(ctx, inputID) }}
		layerID := bound[i].Artifact.ID
		layers[i] = pdfassemble.TextLayer{PageID: bound[i].Input.ID, Open: func(ctx context.Context) (io.ReadCloser, error) { return final.OpenInput(ctx, layerID) }}
	}
	output, err := pdfassemble.BuildWithLayers(ctx, images, layers, work.Snapshot.Settings.PDFEncoding, options)
	if err != nil {
		return final, "", err
	}
	writer, err := final.CreateArtifact(ctx, artifactscope.PDF)
	if err != nil {
		return final, "", err
	}
	_, writeErr := writer.Write(output)
	closeErr := writer.Close()
	if writeErr != nil || closeErr != nil {
		return final, "", errors.Join(writeErr, closeErr)
	}
	if err = ocrScope.Close(); err != nil {
		return final, "", err
	}
	return final, writer.Reference(), nil
}
