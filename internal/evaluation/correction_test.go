package evaluation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func loadFixtureCatalogForTest(t *testing.T) Catalog {
	t.Helper()
	catalog, err := LoadCatalog(filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
