package ocrplugin

import "errors"

var ErrResult = errors.New("invalid OCR result set")
var ErrResultLimit = errors.New("OCR result exceeds reserved output bytes")

// ResultArtifact is coordinator-supplied metadata from its sealed job scope.
// Providers cannot supply this list. Paths, ownership and file hashes remain
// with the coordinator/scope; this contract never resolves a provider path.
type ResultArtifact struct {
	ID   string
	Kind OutputKind
	Size int64
}

// BindResult verifies exact page/artifact coverage and returns results in the
// immutable request order. It requires prior model/capability validation and a
// successfully sealed scope. It validates metadata, not OCR/PDF/JSON contents.
func BindResult(request Request, result Result, artifacts []ResultArtifact, maxBytes int64) ([]PageResult, error) {
	if !nonempty(request.JobID) || request.InputRevision < 1 || !nonempty(request.ModelID) || !nonempty(request.ModelVersion) || result.ModelVersion != request.ModelVersion || len(request.Pages) == 0 || len(request.Pages) > 10000 || len(result.Pages) != len(request.Pages) || len(artifacts) != len(request.Pages) || maxBytes < 1 || maxBytes > 1<<40 {
		return nil, ErrResult
	}
	switch request.Output {
	case Text, PositionedText, PDFTextLayer, SearchablePDF:
	default:
		return nil, ErrResult
	}
	pages := make(map[string]bool, len(request.Pages))
	inputs := make(map[string]bool, len(request.Pages))
	for _, page := range request.Pages {
		if !nonempty(page.ID) || !nonempty(page.ArtifactID) || pages[page.ID] {
			return nil, ErrResult
		}
		pages[page.ID] = true
		inputs[page.ArtifactID] = true
	}
	produced := make(map[string]ResultArtifact, len(artifacts))
	var total int64
	for _, artifact := range artifacts {
		if !nonempty(artifact.ID) || inputs[artifact.ID] || artifact.Kind != request.Output || artifact.Size < 0 || (artifact.Size == 0 && artifact.Kind != Text) {
			return nil, ErrResult
		}
		if _, duplicate := produced[artifact.ID]; duplicate {
			return nil, ErrResult
		}
		if artifact.Size > maxBytes-total {
			return nil, ErrResultLimit
		}
		total += artifact.Size
		produced[artifact.ID] = artifact
	}
	byPage := make(map[string]PageResult, len(result.Pages))
	used := make(map[string]bool, len(result.Pages))
	for _, page := range result.Pages {
		if !pages[page.PageID] || page.Kind != request.Output || used[page.ArtifactID] {
			return nil, ErrResult
		}
		if _, duplicate := byPage[page.PageID]; duplicate {
			return nil, ErrResult
		}
		if _, exists := produced[page.ArtifactID]; !exists {
			return nil, ErrResult
		}
		byPage[page.PageID] = page
		used[page.ArtifactID] = true
	}
	if len(used) != len(produced) {
		return nil, ErrResult
	}
	ordered := make([]PageResult, len(request.Pages))
	for i, page := range request.Pages {
		bound, exists := byPage[page.ID]
		if !exists {
			return nil, ErrResult
		}
		ordered[i] = bound
	}
	return ordered, nil
}
