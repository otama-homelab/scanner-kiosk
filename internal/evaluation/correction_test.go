package evaluation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
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
	path, oracles := writeCorrectionSurvivalFixture(t, original, corrected, image.Rect(10, 10, 12, 11),
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
	path, oracles := writeCorrectionSurvivalFixture(t, original, whiteTestImage(100, 50), image.Rect(10, 10, 30, 11),
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
		image.Rect(10, 10, 30, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	comparison, err := AnalyzeCorrectionCatalogs([]string{path}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	if comparison.Rows[0].Quality.Status != "failed" || comparison.Rows[0].FailureClass != "empty-oracle-region" {
		t.Fatalf("empty oracle region passed: %+v", comparison.Rows[0])
	}

	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	published, err := LoadCorrectionOracles(
		filepath.Join(root, "scripts", "fixtures", "evaluation", "correction-oracles.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fixtureBody, err := ResolveFixture(catalogPath, "", "", "fixture_white_on_white_001")
	if err != nil {
		t.Fatal(err)
	}
	fixtureImage, err := png.Decode(bytes.NewReader(fixtureBody))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range published.Entries {
		if entry.FixtureID != "fixture_white_on_white_001" {
			continue
		}
		for _, region := range entry.ContentRegions {
			foreground := 0
			for y := region.Y; y < region.Y+region.Height; y++ {
				for x := region.X; x < region.X+region.Width; x++ {
					if isForeground(fixtureImage, x, y, entry.BackgroundRGB) {
						foreground++
					}
				}
			}
			if foreground == 0 {
				t.Fatalf("white-on-white content oracle has no measurable foreground: %+v", region)
			}
		}
		return
	}
	t.Fatal("white-on-white oracle entry is missing")
}

func TestWhiteOnWhiteFixtureIdentity(t *testing.T) {
	catalog := loadFixtureCatalogForTest(t)
	for _, fixture := range catalog.Fixtures {
		if fixture.ID != "fixture_white_on_white_001" {
			continue
		}
		bitmap, err := renderPattern(fixture.Recipe.Variant, nil)
		if err != nil {
			t.Fatal(err)
		}
		var body bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := encoder.Encode(&body, bitmap); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body.Bytes())
		actual := fmt.Sprintf("sha256=%x bytes=%d", sum, body.Len())
		if fixture.SHA256 != fmt.Sprintf("%x", sum) || fixture.Bytes != int64(body.Len()) {
			t.Fatalf("white-on-white fixture identity: %s", actual)
		}
		return
	}
	t.Fatal("white-on-white fixture definition is missing")
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

func TestCorrectionBenchmarkIdentity(t *testing.T) {
	path, oracles := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
		image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	benchmarkPath := writeCorrectionBenchmark(t, path)

	comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks([]string{path}, []string{benchmarkPath}, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatalf("identity-matched analysis failed: %+v %v", comparison, err)
	}
	row := comparison.Rows[0]
	resources := row.Resources
	if row.Status != "passed" || resources.Status != "complete" ||
		resources.BenchmarkRunID == "" || resources.CandidateConfigSHA256 != strings.Repeat("c", 64) ||
		resources.Metrics.Cold.P50Millis == nil || resources.Metrics.Warm.P95Millis == nil ||
		len(resources.Sources) != 1 || resources.Sources[0].License != "test-only" ||
		len(resources.OperatorSteps) != 1 || resources.OperatorSteps[0] != "run the fixture helper" {
		t.Fatalf("benchmark evidence was not attached separately: %+v", row)
	}
}

func TestCorrectionRejectsConfigMismatch(t *testing.T) {
	qualityPath, oracles := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
		image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	rewriteCorrectionCatalogConfig(t, qualityPath, strings.Repeat("c", 64))
	benchmarkCatalogPath, _ := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
		image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	rewriteCorrectionCatalogConfig(t, benchmarkCatalogPath, strings.Repeat("f", 64))
	benchmarkPath := writeCorrectionBenchmark(t, benchmarkCatalogPath)
	rewriteCorrectionBenchmark(t, benchmarkPath, func(summary *BenchmarkSummary) {
		summary.CandidateConfigSHA256 = strings.Repeat("f", 64)
		for index := range summary.Samples {
			summary.Samples[index].CandidateConfigSHA256 = summary.CandidateConfigSHA256
			summary.Samples[index].SourceHashesBefore["candidate_config"] = summary.CandidateConfigSHA256
			summary.Samples[index].SourceHashesAfter["candidate_config"] = summary.CandidateConfigSHA256
		}
	})

	comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks(
		[]string{qualityPath}, []string{benchmarkPath}, oracles,
	)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	row := comparison.Rows[0]
	if comparison.Status != "incomplete" || row.Status != "failed" ||
		row.Quality.Status != "passed" || row.Resources.Status != "invalid" ||
		row.Resources.FailureClass != "resource-identity-mismatch" {
		t.Fatalf("config-only mismatch attached resources: %+v", comparison)
	}
}

func TestCorrectionRequiresBenchmarkEvidence(t *testing.T) {
	path, oracles := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
		image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
	comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks([]string{path}, nil, oracles)
	if err != nil || len(comparison.Rows) != 1 {
		t.Fatal(err)
	}
	row := comparison.Rows[0]
	if comparison.Status != "incomplete" || row.Status != "failed" ||
		row.Quality.Status != "passed" || row.Resources.Status != "unavailable" ||
		row.Resources.FailureClass != "resource-unavailable" {
		t.Fatalf("quality-only analysis passed without benchmark evidence: %+v", comparison)
	}
}

func TestCorrectionRejectsMismatchedResources(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*BenchmarkSummary)
	}{
		{name: "fixture", mutate: func(summary *BenchmarkSummary) {
			summary.FixtureID = "fixture_other_001"
		}},
		{name: "candidate version", mutate: func(summary *BenchmarkSummary) {
			summary.Candidate.Version = "2"
		}},
		{name: "executable hash", mutate: func(summary *BenchmarkSummary) {
			summary.Candidate.ExecutableSHA256 = repeatTestHash("f")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, oracles := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
				image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
			benchmarkPath := writeCorrectionBenchmark(t, path)
			body, err := os.ReadFile(benchmarkPath)
			if err != nil {
				t.Fatal(err)
			}
			var summary BenchmarkSummary
			if err := json.Unmarshal(body, &summary); err != nil {
				t.Fatal(err)
			}
			test.mutate(&summary)
			for index := range summary.Samples {
				summary.Samples[index].FixtureID = summary.FixtureID
				summary.Samples[index].Candidate = summary.Candidate
			}
			encoded, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(benchmarkPath, encoded, 0o600); err != nil {
				t.Fatal(err)
			}

			comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks([]string{path}, []string{benchmarkPath}, oracles)
			if err != nil || len(comparison.Rows) != 1 {
				t.Fatal(err)
			}
			row := comparison.Rows[0]
			if row.Status != "failed" || comparison.Status != "incomplete" ||
				row.Quality.Status != "passed" || row.Resources.Status != "invalid" ||
				row.Resources.FailureClass != "resource-identity-mismatch" {
				t.Fatalf("mismatched identity became resource evidence: %+v", row)
			}
		})
	}
}

func TestCorrectionRejectsUnsafeResourceArtifacts(t *testing.T) {
	for _, test := range []struct {
		name         string
		prepare      func(*testing.T, string, string)
		failureClass string
	}{
		{
			name: "missing catalog",
			prepare: func(t *testing.T, _, benchmarkPath string) {
				rewriteCorrectionBenchmark(t, benchmarkPath, func(summary *BenchmarkSummary) {
					for index := range summary.Samples {
						summary.Samples[index].ArtifactCatalog = "missing.json"
					}
				})
			},
			failureClass: "resource-artifact-catalog",
		},
		{
			name: "traversal",
			prepare: func(t *testing.T, _, benchmarkPath string) {
				rewriteCorrectionBenchmark(t, benchmarkPath, func(summary *BenchmarkSummary) {
					for index := range summary.Samples {
						summary.Samples[index].ArtifactCatalog = "../artifact-catalog.json"
					}
				})
			},
			failureClass: "resource-artifact-catalog",
		},
		{
			name: "artifact hash",
			prepare: func(t *testing.T, catalogPath, _ string) {
				body, err := os.ReadFile(catalogPath)
				if err != nil {
					t.Fatal(err)
				}
				var catalog ArtifactCatalog
				if err := json.Unmarshal(body, &catalog); err != nil {
					t.Fatal(err)
				}
				catalog.Artifacts[0].SHA256 = repeatTestHash("f")
				encoded, err := json.Marshal(catalog)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(catalogPath, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			failureClass: "resource-artifact-hash",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, oracles := writeCorrectionSurvivalFixture(t, whiteImageWithContent(t), whiteImageWithContent(t),
				image.Rect(10, 10, 20, 11), "[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
			benchmarkPath := writeCorrectionBenchmark(t, path)
			test.prepare(t, path, benchmarkPath)
			comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks([]string{path}, []string{benchmarkPath}, oracles)
			if err != nil || len(comparison.Rows) != 1 {
				t.Fatal(err)
			}
			if comparison.Rows[0].Resources.FailureClass != test.failureClass ||
				comparison.Rows[0].Resources.Status != "invalid" ||
				comparison.Status != "incomplete" {
				t.Fatalf("unsafe resource evidence was not classified: %+v", comparison.Rows[0])
			}
		})
	}
}

func rewriteCorrectionBenchmark(t *testing.T, path string, mutate func(*BenchmarkSummary)) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var summary BenchmarkSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		t.Fatal(err)
	}
	mutate(&summary)
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func rewriteCorrectionCatalogConfig(t *testing.T, path, configSHA string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	catalog["candidate_config_sha256"] = json.RawMessage(strconv.Quote(configSHA))
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCorrectionFourArmClosure(t *testing.T) {
	cleanPath, oracles := writeCorrectionSurvivalFixtureCandidate(t, "candidate_clean_001",
		whiteImageWithContent(t), whiteImageWithContent(t), image.Rect(10, 10, 20, 11))
	cleanBenchmark := writeCorrectionBenchmark(t, cleanPath)
	destructivePath, oracles := writeCorrectionSurvivalFixtureCandidate(t, "candidate_destructive_001",
		whiteImageWithContent(t), whiteTestImage(100, 50), image.Rect(10, 10, 20, 11))
	destructiveBenchmark := writeCorrectionBenchmark(t, destructivePath)
	validPath, oracles := writeCorrectionSurvivalFixtureCandidate(t, "candidate_valid_001",
		whiteImageWithContent(t), whiteImageWithContent(t), image.Rect(10, 10, 20, 11))
	validBenchmark := writeCorrectionBenchmark(t, validPath)
	unavailablePath, _ := writeCorrectionSurvivalFixtureCandidate(t, "candidate_survival_001",
		whiteImageWithContent(t), whiteImageWithContent(t), image.Rect(10, 10, 20, 11))
	if err := os.Remove(filepath.Join(filepath.Dir(unavailablePath), "artifacts", "artifact_correction_survival_001")); err != nil {
		t.Fatal(err)
	}

	artifacts := []string{cleanPath, unavailablePath, destructivePath, validPath}
	benchmarks := []string{validBenchmark, destructiveBenchmark, cleanBenchmark}
	comparison, err := AnalyzeCorrectionCatalogsWithBenchmarks(artifacts, benchmarks, oracles)
	if err != nil || len(comparison.Rows) != 4 {
		t.Fatalf("four-arm comparison failed: %+v %v", comparison, err)
	}
	var clean, unavailable, destructive, valid bool
	for _, row := range comparison.Rows {
		switch row.CandidateID {
		case "candidate_clean_001":
			clean = row.Quality.Status == "passed" && row.Resources.Status == "complete"
		case "candidate_destructive_001":
			destructive = row.Quality.Status == "failed" && row.Resources.Status == "complete"
		case "candidate_valid_001":
			valid = row.Quality.Status == "passed" && row.Resources.Status == "complete"
		case "candidate_survival_001":
			unavailable = row.Quality.Status != "passed" && row.Resources.Status == "unavailable"
		}
	}
	if !clean || !unavailable || !destructive || !valid || comparison.Status != "incomplete" {
		t.Fatalf("four arms were collapsed or fabricated: %+v", comparison)
	}

	reverseArtifacts := append([]string(nil), artifacts...)
	for left, right := 0, len(reverseArtifacts)-1; left < right; left, right = left+1, right-1 {
		reverseArtifacts[left], reverseArtifacts[right] = reverseArtifacts[right], reverseArtifacts[left]
	}
	reverseBenchmarks := append([]string(nil), benchmarks...)
	for left, right := 0, len(reverseBenchmarks)-1; left < right; left, right = left+1, right-1 {
		reverseBenchmarks[left], reverseBenchmarks[right] = reverseBenchmarks[right], reverseBenchmarks[left]
	}
	reversed, err := AnalyzeCorrectionCatalogsWithBenchmarks(reverseArtifacts, reverseBenchmarks, oracles)
	if err != nil || !reflect.DeepEqual(comparison, reversed) {
		t.Fatalf("comparison depends on receipt order: %+v != %+v (%v)", comparison, reversed, err)
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
	return writeCorrectionSurvivalFixtureWithTransform(t, "candidate_survival_001", original, corrected,
		region, quad, width, height, rotation)
}

func writeCorrectionSurvivalFixtureCandidate(t *testing.T, candidateID string, original, corrected *image.RGBA,
	region image.Rectangle) (string, CorrectionOracleFile) {
	return writeCorrectionSurvivalFixtureWithTransform(t, candidateID, original, corrected, region,
		"[[0,0],[100,0],[100,50],[0,50]]", 100, 50, 0)
}

func writeCorrectionSurvivalFixtureWithTransform(t *testing.T, candidateID string, original, corrected *image.RGBA,
	region image.Rectangle, quad string, width, height int, rotation int) (string, CorrectionOracleFile) {
	t.Helper()
	root := t.TempDir()
	runRoot := filepath.Join(root, "run")
	artifacts := filepath.Join(runRoot, "artifacts")
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
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
		FixtureSHA256:         "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
		CandidateConfigSHA256: strings.Repeat("c", 64),
		Candidate: CandidateReceipt{
			ID: candidateID, Version: "1", ExecutableSHA256: repeatTestHash("a"),
			Sources:       []CandidatePin{{Name: "fixture", Version: "1", License: "test-only"}},
			OperatorSteps: []string{"run the fixture helper"},
			Parameters:    map[string]string{},
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
	path := filepath.Join(runRoot, "artifact-catalog.json")
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

func whiteImageWithContent(t *testing.T) *image.RGBA {
	t.Helper()
	bitmap := whiteTestImage(100, 50)
	for x := 10; x < 20; x++ {
		bitmap.SetRGBA(x, 10, color.RGBA{R: 20, G: 30, B: 40, A: 255})
	}
	return bitmap
}

func writeCorrectionBenchmark(t *testing.T, catalogPath string) string {
	t.Helper()
	body, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	var catalog ArtifactCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	configSHA := strings.Repeat("c", 64)
	relative, err := filepath.Rel(filepath.Dir(catalogPath), catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	summary := BenchmarkSummary{
		Format: BenchmarkFormat, Version: BenchmarkVersion, RunID: "benchmark_" + catalog.Candidate.ID,
		Status: "complete", StartedAt: "2026-11-09T00:00:00Z", FinishedAt: "2026-11-09T00:00:03Z",
		FixtureID: catalog.FixtureID, FixtureSHA256: catalog.FixtureSHA256,
		Candidate: catalog.Candidate, CandidateConfigSHA256: configSHA,
		Parameters:         map[string]string{},
		InvocationTemplate: []string{"/usr/bin/time", "{candidate}"},
		Observer: ObserverEvidence{Path: "/usr/bin/time", SHA256: repeatTestHash("d"),
			Version: "time", License: "GPL-3.0-or-later"},
		Settings: BenchmarkSettings{ColdRepetitions: 1, WarmRepetitions: 2, TimeoutSeconds: 900,
			MaxRSSBytes: 1 << 30, MaxTempDiskBytes: 1 << 30},
		Metrics: BenchmarkMetrics{
			Cold: ModeMetrics{Samples: 1, P50Millis: int64TestPointer(100), P95Millis: int64TestPointer(100),
				PeakRSSBytes: 1000},
			Warm: ModeMetrics{Samples: 2, P50Millis: int64TestPointer(80), P95Millis: int64TestPointer(90),
				PeakRSSBytes: 900},
		},
	}
	for index, mode := range []string{"cold", "warm", "warm"} {
		summary.Samples = append(summary.Samples, BenchmarkSample{
			SampleID: summary.RunID + "_" + strconv.Itoa(index+1), Mode: mode, Sequence: index + 1,
			StartedAt: "2026-11-09T00:00:00Z", FinishedAt: "2026-11-09T00:00:01Z",
			FixtureID: summary.FixtureID, FixtureSHA256: summary.FixtureSHA256,
			Candidate: summary.Candidate, CandidateConfigSHA256: configSHA,
			Parameters: summary.Parameters, WallMillis: 100, PeakRSSBytes: 1000,
			Status: "complete", ValidationStatus: "passed",
			ArtifactCatalog: relative, ReceiptPath: "receipts/" + strconv.Itoa(index+1) + ".json",
			SourceHashesBefore: map[string]string{
				"fixture": summary.FixtureSHA256, "candidate_config": configSHA,
				"executable": summary.Candidate.ExecutableSHA256,
			},
			SourceHashesAfter: map[string]string{
				"fixture": summary.FixtureSHA256, "candidate_config": configSHA,
				"executable": summary.Candidate.ExecutableSHA256,
			},
		})
	}
	benchmarkPath := filepath.Join(filepath.Dir(catalogPath), "benchmark.json")
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(benchmarkPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return benchmarkPath
}

func int64TestPointer(value int64) *int64 {
	return &value
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
		FixtureSHA256: repeatTestHash("a"), CandidateConfigSHA256: repeatTestHash("c"), Status: status,
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
