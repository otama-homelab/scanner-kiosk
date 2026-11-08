package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCorrectionProvenance(t *testing.T) {
	path, oracles := correctionCatalogForTest(t, "correction")
	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatalf("analysis failed: %+v %v", comparison, err)
	}
	row := comparison.Rows[0]
	if row.OriginalSHA256 != "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81" ||
		row.CorrectionID == "" || row.CorrectionSHA256 == "" || !row.Quality.TransformValid ||
		row.Transform.OutputWidth != 960 || row.Transform.OutputHeight != 640 {
		t.Fatalf("unexpected correction provenance: %+v", row)
	}
	if row.MeasurementStatus != "incomplete" || comparison.Status != "incomplete" {
		t.Fatalf("partial candidate evidence was reported complete: %+v", comparison)
	}
}

func TestCorrectionQuality(t *testing.T) {
	path, oracles := correctionCatalogForTest(t, "correction")
	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	quality := comparison.Rows[0].Quality
	if quality.Status != "passed" || quality.EdgeRegions != 1 || quality.EdgeRegionsRetained != 1 ||
		quality.ContentRegionsRetained != quality.ContentRegions ||
		quality.OriginalForeground == 0 || quality.CorrectionForeground == 0 ||
		quality.ForegroundRetention <= 0 {
		t.Fatalf("unexpected correction quality: %+v", quality)
	}
}

func TestCorrectionRejectsDestructiveCrop(t *testing.T) {
	path, oracles := correctionCatalogForTest(t, "destructive_correction")
	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	row := comparison.Rows[0]
	if row.Quality.Status != "failed" || row.Quality.EdgeRegionsRetained != 0 ||
		row.FailureClass != "edge-or-content-loss" {
		t.Fatalf("destructive crop was not rejected: %+v", row)
	}
}

func TestCorrectionPixelSurvival(t *testing.T) {
	original := whiteTestImage(100, 50)
	for x := 10; x < 22; x++ {
		original.SetRGBA(x, 10, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	}
	corrected := whiteTestImage(50, 25)
	corrected.SetRGBA(5, 5, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	path, oracles := writeCorrectionSurvivalFixture(t, original, corrected, image.Rect(10, 10, 12, 1),
		"[[0,0],[100,0],[100,50],[0,50]]", 50, 25, 0)

	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatalf("pixel survival analysis failed: %+v %v", comparison, err)
	}
	quality := comparison.Rows[0].Quality
	if quality.Status != "passed" || len(quality.ContentMeasurements) != 1 {
		t.Fatalf("clean downscale did not report surviving pixels: %+v", quality)
	}
	region := quality.ContentMeasurements[0]
	if region.OriginalForeground != 2 || region.MappedForeground != 2 || region.RetainedForeground != 2 ||
		math.Abs(region.RetentionRatio-1) > 1e-9 {
		t.Fatalf("downscale duplicate output locations were not counted: %+v", region)
	}
}

func TestCorrectionErasedContentFails(t *testing.T) {
	original := whiteTestImage(100, 50)
	for x := 10; x < 30; x++ {
		original.SetRGBA(x, 10, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	}
	path, oracles := writeCorrectionSurvivalFixture(t, original, whiteTestImage(100, 50), image.Rect(10, 10, 20, 1),
		"[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)

	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatalf("erased-content analysis failed: %+v %v", comparison, err)
	}
	row := comparison.Rows[0]
	if row.Quality.Status != "failed" || row.FailureClass != "edge-or-content-loss" ||
		len(row.Quality.ContentMeasurements) != 1 ||
		row.Quality.ContentMeasurements[0].RetainedForeground != 0 {
		t.Fatalf("crop geometry allowed erased pixels to pass: %+v", row)
	}
}

func TestCorrectionRejectsEmptyOracleRegion(t *testing.T) {
	path, oracles := writeCorrectionSurvivalFixture(t, whiteTestImage(100, 50), whiteTestImage(100, 50),
		image.Rect(10, 10, 20, 1), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	if comparison.Rows[0].Quality.Status != "failed" || comparison.Rows[0].FailureClass != "empty-oracle-region" {
		t.Fatalf("empty oracle region passed: %+v", comparison.Rows[0])
	}
}

func TestCorrectionOracleCoverage(t *testing.T) {
	catalog := loadFixtureCatalogForTest(t)
	oracles, err := LoadCorrectionOracles(filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "correction-oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorrectionOracleCoverage(catalog, oracles); err != nil {
		t.Fatal(err)
	}
	missing := oracles
	missing.Entries = append([]CorrectionOracleEntry(nil), oracles.Entries[1:]...)
	if err := ValidateCorrectionOracleCoverage(catalog, missing); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing required oracle accepted: %v", err)
	}
}

func TestCorrectionCandidateUnavailable(t *testing.T) {
	_, oracles := correctionCatalogForTest(t, "correction")
	comparison, err := AnalyzeCorrectionCatalogs([]string{filepath.Join(t.TempDir(), "missing.json")}, oracles)
	if err != nil || len(comparison.Rows) != 1 || comparison.Rows[0].Status == "passed" ||
		comparison.Rows[0].FailureClass == "" {
		t.Fatalf("missing candidate became evidence: %+v %v", comparison, err)
	}
}

func TestCorrectionAnalysisRejectsExpectedFailureCatalog(t *testing.T) {
	oracles, err := LoadCorrectionOracles(filepath.Join(
		"..", "..", "scripts", "fixtures", "evaluation", "correction-oracles.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"expected-failure", "incomplete"} {
		t.Run(status, func(t *testing.T) {
			path := writeCorrectionCatalogWithoutOriginal(t, status)
			comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
			if err != nil || comparison.Status != "incomplete" || len(comparison.Rows) != 1 {
				t.Fatalf("nil-original catalog was not rejected: %+v %v", comparison, err)
			}
			row := comparison.Rows[0]
			if row.Status != "failed" || row.MeasurementStatus != "incomplete" ||
				row.FailureClass != "artifact-catalog" || row.OriginalSHA256 != "" {
				t.Fatalf("nil-original catalog produced evidence: %+v", row)
			}
		})
	}
}

func TestCorrectionFourArmIdentity(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "scripts", "eval", "correction", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, wanted := range []string{
		"correction_none", "correction_tesseract_internal", "correction_leptonica", "correction_opencv",
		"Tesseract 5.5.4", "Leptonica 1.87.0", "OpenCV 4.6.0",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("candidate contract missing %q", wanted)
		}
	}
}

func TestCorrectionComparison(t *testing.T) {
	path, oracles := correctionCatalogForTest(t, "correction")
	comparison, err := AnalyzeCorrectionCatalogs([]string{path, path}, oracles)
	if err != nil || len(comparison.Rows) != 2 || comparison.Format != CorrectionCompareFormat ||
		comparison.Rows[0].FixtureID > comparison.Rows[1].FixtureID {
		t.Fatalf("comparison ordering/schema failed: %+v %v", comparison, err)
	}
}

func correctionCatalogForTest(t *testing.T, mode string) (string, CorrectionOracleFile) {
	t.Helper()
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, mode)
	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_card_001", OutputDir: output,
	})
	if !errors.Is(err, ErrUnavailable) || len(catalog.Artifacts) != 1 ||
		catalog.Artifacts[0].Role != RoleCorrection {
		t.Fatalf("correction helper failed: %+v %v", catalog, err)
	}
	oracles, err := LoadCorrectionOracles(filepath.Join(root, "scripts", "fixtures", "evaluation", "correction-oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(output, "artifact-catalog.json"), oracles
}

func writeCorrectionSurvivalFixture(t *testing.T, original, corrected *image.RGBA, region image.Rectangle,
	quad string, width, height int, rotation int) (string, CorrectionOracleFile) {
	t.Helper()
	root := t.TempDir()
	artifacts := filepath.Join(root, "artifacts")
	if err := os.Mkdir(artifacts, 0o700); err != nil {
		t.Fatal(err)
	}
	writePNG := func(id string, value image.Image) string {
		var body bytes.Buffer
		if err := png.Encode(&body, value); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(artifacts, id), body.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return sha256Hex(body.Bytes())
	}
	originalID := "artifact_original_survival_001"
	correctionID := "artifact_correction_survival_001"
	originalSHA := writePNG(originalID, original)
	correctionSHA := writePNG(correctionID, corrected)
	catalog := ArtifactCatalog{
		Format: ArtifactCatalogFormat, Version: ArtifactCatalogVersion,
		RunID: "run_correction_survival_001", FixtureID: "fixture_card_001",
		FixtureSHA256: "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
		Candidate: CandidateReceipt{
			ID: "candidate_survival_001", Version: "1", ExecutableSHA256: repeatTestHash("a"),
			Sources:    []CandidatePin{{Name: "fixture", Version: "1", License: "test-only"}},
			Parameters: map[string]string{},
		},
		Status: "complete",
		Original: &Artifact{
			ID: originalID, Role: RoleOriginal, MediaType: "image/png", SHA256: originalSHA,
			Bytes: int64(encodedPNGLength(t, original)), SourcePages: []string{"page_card_001"},
		},
		Artifacts: []Artifact{{
			ID: correctionID, Role: RoleCorrection, MediaType: "image/png", SHA256: correctionSHA,
			Bytes: int64(encodedPNGLength(t, corrected)), SourcePages: []string{"page_card_001"},
			Parents: []string{originalID}, Algorithm: "fixture", AlgorithmVersion: "1",
			Parameters: map[string]string{
				"crop_quad": quad, "rotation_degrees": fmtInt(rotation),
				"output_width": fmtInt(width), "output_height": fmtInt(height),
				"confidence_kind": "unavailable", "fallback_reason": "",
			},
		}},
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "artifact-catalog.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, CorrectionOracleFile{
		Format: CorrectionOracleFormat, Version: CorrectionFormatVersion,
		Entries: []CorrectionOracleEntry{{
			FixtureID:     "fixture_card_001",
			FixtureSHA256: "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
			BackgroundRGB: [3]uint8{255, 255, 255},
			ContentRegions: []CorrectionRegion{{
				X: region.Min.X, Y: region.Min.Y, Width: region.Dx(), Height: region.Dy(),
			}},
		}},
	}
}

func whiteTestImage(width, height int) *image.RGBA {
	bitmap := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bitmap.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return bitmap
}

func encodedPNGLength(t *testing.T, value image.Image) int {
	t.Helper()
	var body bytes.Buffer
	if err := png.Encode(&body, value); err != nil {
		t.Fatal(err)
	}
	return body.Len()
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}

func writeCorrectionCatalogWithoutOriginal(t *testing.T, status string) string {
	t.Helper()
	body, err := json.Marshal(ArtifactCatalog{
		Format: ArtifactCatalogFormat, Version: ArtifactCatalogVersion,
		RunID: "run_correction_without_original", FixtureID: "fixture_card_001",
		FixtureSHA256: repeatTestHash("a"), Status: status,
		Artifacts: []Artifact{{
			ID: "artifact_correction_without_original", Role: RoleCorrection,
			MediaType: "image/png", SHA256: repeatTestHash("b"), Bytes: 1,
			SourcePages: []string{"page_card_001"},
			Parents:     []string{"artifact_original_missing"}, Algorithm: "test", AlgorithmVersion: "1",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["original"]; ok {
		t.Fatal("nil original was serialized as a zero-value artifact")
	}
	path := filepath.Join(t.TempDir(), "artifact-catalog.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadFixtureCatalogForTest(t *testing.T) Catalog {
	t.Helper()
	catalog, err := LoadCatalog(filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
