//go:build linux

// Package ocrresult binds provider output references to coordinator-owned sealed
// metadata. File content validators and document publication remain separate.
package ocrresult

import (
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/pkg/ocrplugin"
)

type Page struct {
	Input    ocrplugin.InputPage
	Artifact artifactscope.Artifact
}

// BindSealed consumes the complete sealed OCR scope, never provider-supplied file
// metadata. It returns copied records in request order after exact validation.
func BindSealed(request ocrplugin.Request, result ocrplugin.Result, sealed []artifactscope.Artifact, maxBytes int64) ([]Page, error) {
	summaries := make([]ocrplugin.ResultArtifact, len(sealed))
	byID := make(map[string]artifactscope.Artifact, len(sealed))
	for i, artifact := range sealed {
		summaries[i] = ocrplugin.ResultArtifact{ID: artifact.ID, Kind: ocrplugin.OutputKind(artifact.Kind), Size: artifact.Size}
		byID[artifact.ID] = artifact
	}
	ordered, err := ocrplugin.BindResult(request, result, summaries, maxBytes)
	if err != nil {
		return nil, err
	}
	pages := make([]Page, len(ordered))
	for i, result := range ordered {
		pages[i] = Page{Input: request.Pages[i], Artifact: byID[result.ArtifactID]}
	}
	return pages, nil
}
