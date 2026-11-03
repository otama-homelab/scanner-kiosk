package ocrplugin

import (
	"errors"
	"testing"
)

func resultFixture() (Request, Result, []ResultArtifact) {
	request := Request{JobID: "job-result", InputRevision: 1, ModelID: "installed", ModelVersion: "pinned-version", Output: PDFTextLayer, Pages: []InputPage{{ID: "page-a", ArtifactID: "original-a"}, {ID: "page-b", ArtifactID: "original-b"}}}
	result := Result{ModelVersion: request.ModelVersion, Pages: []PageResult{{PageID: "page-b", ArtifactID: "output-b", Kind: PDFTextLayer}, {PageID: "page-a", ArtifactID: "output-a", Kind: PDFTextLayer}}}
	artifacts := []ResultArtifact{{ID: "output-a", Kind: PDFTextLayer, Size: 20}, {ID: "output-b", Kind: PDFTextLayer, Size: 30}}
	return request, result, artifacts
}
func TestBindResultOrdersAndCopies(t *testing.T) {
	request, result, artifacts := resultFixture()
	ordered, err := BindResult(request, result, artifacts, 50)
	if err != nil || len(ordered) != 2 || ordered[0].PageID != "page-a" || ordered[0].ArtifactID != "output-a" || ordered[1].ArtifactID != "output-b" {
		t.Fatalf("binding: %v", err)
	}
	ordered[0].ArtifactID = "changed"
	if result.Pages[1].ArtifactID != "output-a" {
		t.Fatal("result aliases provider array")
	}
}
func TestBindResultRejectsUntrustedMetadata(t *testing.T) {
	for _, mode := range []string{"model-version", "missing-page", "foreign-page", "duplicate-page", "duplicate-output", "unscoped-output", "original-alias", "wrong-result-kind", "wrong-sealed-kind", "zero-pdf", "negative-size", "duplicate-sealed", "extra-sealed", "quota", "request-duplicate", "unknown-output"} {
		t.Run(mode, func(t *testing.T) {
			request, result, artifacts := resultFixture()
			budget := int64(50)
			switch mode {
			case "model-version":
				result.ModelVersion = "different-version"
			case "missing-page":
				result.Pages = result.Pages[:1]
			case "foreign-page":
				result.Pages[0].PageID = "foreign-page"
			case "duplicate-page":
				result.Pages[0].PageID = result.Pages[1].PageID
			case "duplicate-output":
				result.Pages[0].ArtifactID = result.Pages[1].ArtifactID
			case "unscoped-output":
				result.Pages[0].ArtifactID = "foreign-output"
			case "original-alias":
				artifacts[0].ID = request.Pages[0].ArtifactID
				result.Pages[1].ArtifactID = artifacts[0].ID
			case "wrong-result-kind":
				result.Pages[0].Kind = SearchablePDF
			case "wrong-sealed-kind":
				artifacts[0].Kind = SearchablePDF
			case "zero-pdf":
				artifacts[0].Size = 0
			case "negative-size":
				artifacts[0].Size = -1
			case "duplicate-sealed":
				artifacts[1] = artifacts[0]
			case "extra-sealed":
				artifacts = append(artifacts, ResultArtifact{ID: "unreferenced", Kind: PDFTextLayer, Size: 1})
			case "quota":
				budget = 49
			case "request-duplicate":
				request.Pages[1] = request.Pages[0]
			case "unknown-output":
				request.Output = "unknown"
			}
			ordered, err := BindResult(request, result, artifacts, budget)
			if err == nil || len(ordered) != 0 {
				t.Fatal("invalid result returned bindings")
			}
			if mode == "quota" && !errors.Is(err, ErrResultLimit) {
				t.Fatal("quota error lost")
			}
		})
	}
}
func TestEmptyTextIsValidOCRResult(t *testing.T) {
	request, result, artifacts := resultFixture()
	request.Output = Text
	for i := range result.Pages {
		result.Pages[i].Kind = Text
		artifacts[i].Kind = Text
		artifacts[i].Size = 0
	}
	if _, err := BindResult(request, result, artifacts, 1); err != nil {
		t.Fatal("blank recognized page must be valid text output")
	}
}
