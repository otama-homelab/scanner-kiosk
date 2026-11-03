//go:build linux

package ocrcomposition_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/internal/ocrresult"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestInstalledTesseractPDFComposition(t *testing.T) {
	binary := os.Getenv("SCANNER_TEST_TESSERACT_BINARY")
	if binary == "" {
		t.Skip("requires installed Tesseract and synthetic fixture")
	}
	ctx := context.Background()
	source, err := os.ReadFile(os.Getenv("SCANNER_TEST_OCR_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	geometry, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"SCANNER_OCR_TESSERACT_PATH": binary, "SCANNER_OCR_TESSERACT_DATA_DIR": os.Getenv("SCANNER_TEST_TESSDATA")}
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := runtime.NewWorker(ctx, core, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "original.png"), source, 0600); err != nil {
		t.Fatal(err)
	}
	inputID := "original_layer_input_001"
	inputs := map[string]artifactscope.Input{inputID: {RelativePath: "original.png", Size: int64(len(source)), SHA256: sha256.Sum256(source)}}
	limits := artifactscope.Limits{MaxInputBytes: 32 << 20, MaxOutputBytes: 16 << 20, MaxTotalOutputBytes: 64 << 20, MaxOutputs: 2}

	budget, err := artifactscope.NewBudget(limits.MaxTotalOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	ocrScope, err := artifactscope.NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer ocrScope.Close()
	defaults := worker.Defaults()
	request := ocrplugin.Request{JobID: "job_composition_001", InputRevision: 1, ModelID: defaults.ModelID, ModelVersion: defaults.ModelVersion, Languages: defaults.Languages, QualityPreset: defaults.QualityPreset, Output: ocrplugin.PDFTextLayer, Pages: []ocrplugin.InputPage{{ID: "page_composition_001", ArtifactID: inputID, Width: geometry.Width, Height: geometry.Height, DPI: 300}}}
	second := request.Pages[0]
	second.ID = "page_composition_002"
	request.Pages = append(request.Pages, second)
	provider, err := worker.Registry.Select(defaults.PluginID, request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Run(ctx, request, ocrScope)
	if err != nil {
		t.Fatal(err)
	}
	layers, err := ocrScope.Seal(ctx)

	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(result.Pages) // Native result order must not control page pairing.
	bound, err := ocrresult.BindSealed(request, result, layers, limits.MaxTotalOutputBytes)
	if err != nil || len(bound) != 2 {
		t.Fatalf("invalid layer result: %v", err)
	}
	layer := bound[0].Artifact
	for _, page := range bound {
		artifact := page.Artifact
		inputs[artifact.ID] = artifactscope.Input{RelativePath: artifact.RelativePath, Size: artifact.Size, SHA256: artifact.SHA256}
	}
	finalScope, err := artifactscope.NewWithBudget(root, inputs, limits, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer finalScope.Close()
	reader, err := finalScope.OpenInput(ctx, layer.ID)
	if err != nil {
		t.Fatal(err)
	}
	layerPDF, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatal("layer input failed")
	}
	base, err := pdfassemble.Build(ctx, []pdfassemble.Page{{ID: inputID, Size: "a4", Color: "color", Width: geometry.Width, Height: geometry.Height, Open: func(ctx context.Context) (io.ReadCloser, error) { return finalScope.OpenInput(ctx, inputID) }}}, "lossless", pdfassemble.Options{MaxInputBytes: 32 << 20, MaxOutputBytes: 16 << 20, MaxPagePixels: 40000000, MaxPages: 1, JPEGQuality: 92})
	if err != nil {
		t.Fatal(err)
	}
	composed, err := pdftextlayer.Overlay(ctx, base, layerPDF, geometry.Width, geometry.Height, 16<<20)
	if err != nil {
		t.Fatal(err)
	}

	multiPages := []pdfassemble.Page{
		{ID: request.Pages[1].ID, Size: "letter", Color: "color", Width: geometry.Width, Height: geometry.Height, Rotation: 90, Open: func(ctx context.Context) (io.ReadCloser, error) { return finalScope.OpenInput(ctx, inputID) }},
		{ID: request.Pages[0].ID, Size: "a4", Color: "color", Width: geometry.Width, Height: geometry.Height, Open: func(ctx context.Context) (io.ReadCloser, error) { return finalScope.OpenInput(ctx, inputID) }},
	}
	textLayers := make([]pdfassemble.TextLayer, 0, 2)
	for _, page := range bound {
		artifactID := page.Artifact.ID
		textLayers = append(textLayers, pdfassemble.TextLayer{PageID: page.Input.ID, Open: func(ctx context.Context) (io.ReadCloser, error) { return finalScope.OpenInput(ctx, artifactID) }})
	}
	policy := pdfassemble.Options{MaxInputBytes: 32 << 20, MaxOutputBytes: min(limits.MaxOutputBytes, budget.Remaining()), MaxPagePixels: 40000000, MaxPages: 2, JPEGQuality: 92}
	multiBase, err := pdfassemble.Build(ctx, multiPages, "lossless", policy)
	if err != nil {
		t.Fatal(err)
	}
	multiComposed, err := pdfassemble.BuildWithLayers(ctx, multiPages, textLayers, "lossless", policy)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := finalScope.CreateArtifact(ctx, artifactscope.PDF)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(multiComposed); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	outputs, err := finalScope.Seal(ctx)
	if err != nil || len(outputs) != 1 {
		t.Fatalf("composition seal: %v", err)
	}

	var intermediateBytes int64
	for _, artifact := range layers {
		intermediateBytes += artifact.Size
	}
	if budget.Used() != intermediateBytes+outputs[0].Size {
		t.Fatal("OCR and final PDF did not share output charge")
	}
	text, err := exec.Command("pdftotext", "-layout", filepath.Join(root, outputs[0].RelativePath), "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	compact := strings.Join(strings.Fields(string(text)), "")
	for _, expected := range []string{"日本語の書類", "ScannerOCR2026", "300dpi"} {
		if !strings.Contains(compact, expected) {
			t.Fatal("final PDF missing synthetic OCR text")
		}
	}
	if output := os.Getenv("SCANNER_TEST_OCR_COMPOSE_OUTPUT"); output != "" {
		for name, data := range map[string][]byte{"base.pdf": base, "composed.pdf": composed, "layer.pdf": layerPDF, "multi-base.pdf": multiBase, "multi-composed.pdf": multiComposed} {
			if err = os.WriteFile(filepath.Join(output, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = finalScope.Close(); err != nil {
		t.Fatal(err)
	}
	if budget.Used() != intermediateBytes {
		t.Fatal("final cleanup changed retained OCR charge")
	}
	if err = ocrScope.Close(); err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 0 {
		t.Fatal("confirmed pipeline cleanup retained output charge")
	}
	if _, err = os.Stat(filepath.Join(root, outputs[0].RelativePath)); !os.IsNotExist(err) {
		t.Fatal("composed staging retained")
	}
	retained, err := os.ReadFile(filepath.Join(root, "original.png"))
	if err != nil || !bytes.Equal(retained, source) {
		t.Fatal("original changed")
	}
}
