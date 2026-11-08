package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scanner-kiosk/internal/evaluation"
)

func TestCandidateCLIHelper(t *testing.T) {
	if os.Getenv("EVAL_CANDIDATE_CLI_HELPER") != "1" {
		return
	}
	output := os.Getenv("SCANNER_EVAL_OUTPUT")
	originalID := os.Getenv("SCANNER_EVAL_ORIGINAL_ID")
	pages := strings.Split(os.Getenv("SCANNER_EVAL_SOURCE_PAGES"), ",")
	if output == "" || originalID == "" || len(pages) == 0 {
		os.Exit(9)
	}
	if err := os.MkdirAll(filepath.Join(output, "texts"), 0o700); err != nil {
		os.Exit(10)
	}
	if err := os.WriteFile(filepath.Join(output, "texts", "ocr.txt"), []byte("cli helper text\n"), 0o600); err != nil {
		os.Exit(10)
	}
	result := map[string]any{
		"format": "scanner-eval-candidate-result", "version": 1, "candidate_id": "candidate_cli_helper_001",
		"artifacts": []any{map[string]any{
			"id": "artifact_cli_ocr_0001", "role": "ocr-text", "path": "texts/ocr.txt",
			"media_type": "text/plain; charset=utf-8", "source_pages": pages,
			"parents": []string{originalID}, "algorithm": "cli-helper", "algorithm_version": "1",
		}},
	}
	body, err := json.Marshal(result)
	if err != nil {
		os.Exit(10)
	}
	if err := os.WriteFile(filepath.Join(output, "result.json"), body, 0o600); err != nil {
		os.Exit(10)
	}
	os.Exit(0)
}

func TestCLIHelper(t *testing.T) {
	if os.Getenv("SCANNER_EVAL_TEST_HELPER") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("SCANNER_EVAL_TEST_ARGS")), &args); err != nil {
		os.Exit(2)
	}
	var stderr bytes.Buffer
	code := run(context.Background(), args, os.Stdout, &stderr)
	if code != 0 {
		_, _ = os.Stderr.WriteString(stderr.String())
	}
	os.Exit(code)
}

func TestGenerateAndListA4FixtureEndToEnd(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	output := filepath.Join(work, "fixtures")
	manifest := filepath.Join(work, "manifest.json")

	result := runHelper(t, "generate-fixtures", "--catalog", catalog, "--output", output, "--manifest", manifest)
	if result.code != 0 {
		t.Fatalf("generate failed: %s", result.stderr)
	}
	body, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Fixtures []struct {
			ID           string   `json:"id"`
			SHA256       string   `json:"sha256"`
			ExpectedText []string `json:"expected_text"`
			Availability string   `json:"availability"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(body, &generated); err != nil {
		t.Fatal(err)
	}
	var a4 bool
	for _, fixture := range generated.Fixtures {
		if fixture.ID == "fixture_a4_ja_en_001" && fixture.SHA256 == "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08" &&
			fixture.Availability == "available" && len(fixture.ExpectedText) == 3 {
			a4 = true
		}
	}
	if !a4 {
		t.Fatalf("A4 fixture missing: %+v", generated.Fixtures)
	}
	if _, err := os.Stat(filepath.Join(output, "fixture_a4_ja_en_001.png")); err != nil {
		t.Fatal(err)
	}

	result = runHelper(t, "list-fixtures", "--catalog", catalog)
	if result.code != 0 {
		t.Fatalf("list failed: %s", result.stderr)
	}
	var listed struct {
		Fixtures []struct {
			ID         string   `json:"id"`
			Roles      []string `json:"expected_outputs"`
			Available  string   `json:"availability"`
			ExpectedTx []string `json:"expected_text"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &listed); err != nil {
		t.Fatal(err)
	}
	a4 = false
	for _, fixture := range listed.Fixtures {
		if fixture.ID == "fixture_a4_ja_en_001" && fixture.Available == "available" && len(fixture.ExpectedTx) == 3 {
			a4 = true
		}
	}
	if !a4 {
		t.Fatalf("A4 fixture missing from list: %s", result.stdout)
	}

	cmd := exec.Command("go", "list", "-deps", "./cmd/scanner-eval")
	cmd.Dir = root
	deps, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"scanner-kiosk/internal/apiruntime", "scanner-kiosk/internal/workerruntime"} {
		if strings.Contains(string(deps), forbidden) {
			t.Fatalf("evaluation CLI imports production runtime %s", forbidden)
		}
	}
}

func TestCatalogRejectsDuplicateID(t *testing.T) {
	catalog := filepath.Join(t.TempDir(), "duplicate.json")
	fixture := map[string]any{
		"id":               "fixture_a4_ja_en_001",
		"source_class":     "public_synthetic",
		"privacy":          "public-synthetic",
		"media_type":       "image/png",
		"page_ids":         []string{"page_a4_ja_en_001"},
		"languages":        []string{"ja", "en"},
		"document_type":    "a4-text",
		"expected_outputs": []string{"ocr-text", "positioned-text", "pdf"},
		"sha256":           "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08",
		"bytes":            69624,
		"recipe":           map[string]string{"kind": "copy", "source": "missing.png"},
	}
	body, err := json.Marshal(map[string]any{
		"format": "scanner-eval-fixtures", "version": 1,
		"fixtures": []any{fixture, fixture},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result := runHelper(t, "list-fixtures", "--catalog", catalog)
	if result.code == 0 {
		t.Fatal("duplicate ID accepted")
	}
	if strings.Contains(result.stderr, "fixture_a4_ja_en_001") {
		t.Fatal("error leaked fixture identifier")
	}
}

func TestRunCommandReturnsNormalizedCatalog(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	config := map[string]any{
		"format": "scanner-eval-candidate", "version": 1,
		"candidate_id": "candidate_cli_helper_001", "executable": executable,
		"executable_sha256": hex.EncodeToString(sum[:]), "argv": []string{"-test.run=^TestCandidateCLIHelper$"},
		"environment": []string{"EVAL_CANDIDATE_CLI_HELPER=1"}, "candidate_version": "1",
		"sources": []any{map[string]string{"name": "scanner-kiosk", "version": "test", "license": "test-only"}},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(filepath.Dir(output), "candidate.json")
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	result := runHelper(t, "run", "--catalog", catalog, "--candidate", configPath, "--fixture", "fixture_a4_ja_en_001", "--output", output)
	if result.code != 1 {
		t.Fatalf("incomplete run exit=%d stderr=%s", result.code, result.stderr)
	}
	var artifactCatalog struct {
		Status   string `json:"status"`
		Original struct {
			SHA256 string `json:"sha256"`
		} `json:"original"`
		Artifacts []struct {
			Role string `json:"role"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &artifactCatalog); err != nil {
		t.Fatalf("invalid catalog: %v: %s", err, result.stdout)
	}
	if artifactCatalog.Status != "incomplete" || artifactCatalog.Original.SHA256 == "" ||
		len(artifactCatalog.Artifacts) != 1 || artifactCatalog.Artifacts[0].Role != "ocr-text" {
		t.Fatalf("unexpected catalog: %+v", artifactCatalog)
	}
}

func TestBenchmarkAndCompareCommandsPreserveFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	runDir := filepath.Join(work, "benchmark")
	configPath := writeCLICandidateConfig(t, work)
	result := runHelper(t, "benchmark", "--catalog", catalog, "--candidate", configPath,
		"--fixture", "fixture_a4_ja_en_001", "--output", runDir, "--cold", "1", "--warm", "1",
		"--timeout-seconds", "20", "--max-rss-mib", "2048", "--max-temp-disk-mib", "1024")
	if result.code != 1 {
		t.Fatalf("benchmark failure exit=%d stderr=%s", result.code, result.stderr)
	}
	var summary struct {
		Status  string `json:"status"`
		Samples []struct {
			Status       string `json:"status"`
			FailureClass string `json:"failure_class"`
		} `json:"samples"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status == "complete" || len(summary.Samples) != 2 {
		t.Fatalf("unexpected benchmark summary: %+v", summary)
	}
	for _, sample := range summary.Samples {
		if sample.Status == "complete" || sample.FailureClass == "" {
			t.Fatalf("failed sample was not preserved: %+v", sample)
		}
	}
	comparisonPath := filepath.Join(work, "comparison.json")
	compare := runHelper(t, "compare", "--receipt", runDir, "--output", comparisonPath)
	if compare.code != 0 {
		t.Fatalf("compare failed: %s", compare.stderr)
	}
	if _, err := os.Stat(comparisonPath); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeCorrectionsCommand(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	artifactPath := filepath.Join(work, "artifact-catalog.json")
	originalID := "artifact_original_correction_001"
	correctionID := "artifact_candidate_correction_001"
	body := makeCorrectionPNG(t)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if err := os.Mkdir(filepath.Join(work, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{originalID, correctionID} {
		if err := os.WriteFile(filepath.Join(work, "artifacts", id), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	catalog := evaluation.ArtifactCatalog{
		Format: evaluation.ArtifactCatalogFormat, Version: evaluation.ArtifactCatalogVersion,
		RunID: "run_correction_cli_001", FixtureID: "fixture_card_001",
		FixtureSHA256: "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
		Candidate: evaluation.CandidateReceipt{
			ID: "candidate_correction_cli_001", Version: "1", ExecutableSHA256: strings.Repeat("a", 64),
			Sources:    []evaluation.CandidatePin{{Name: "fixture", Version: "1", License: "test-only"}},
			Parameters: map[string]string{},
		},
		Status: "incomplete",
		Original: evaluation.Artifact{
			ID: originalID, Role: evaluation.RoleOriginal, MediaType: "image/png", SHA256: digest,
			Bytes: int64(len(body)), SourcePages: []string{"page_card_001"},
		},
		Artifacts: []evaluation.Artifact{{
			ID: correctionID, Role: evaluation.RoleCorrection, MediaType: "image/png", SHA256: digest,
			Bytes: int64(len(body)), SourcePages: []string{"page_card_001"}, Parents: []string{originalID},
			Algorithm: "fixture-crop", AlgorithmVersion: "1",
			Parameters: map[string]string{
				"crop_quad": "[[0,0],[960,0],[960,640],[0,640]]", "rotation_degrees": "0",
				"output_width": "960", "output_height": "640",
				"confidence_kind": "unavailable", "fallback_reason": "",
			},
		}},
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	result := runHelper(t, "analyze-corrections",
		"--catalog", filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json"),
		"--oracles", filepath.Join(root, "scripts", "fixtures", "evaluation", "correction-oracles.json"),
		"--artifacts", artifactPath)
	if result.code != 1 {
		t.Fatalf("incomplete measurement exit=%d stderr=%s", result.code, result.stderr)
	}
	var comparison struct {
		Status string `json:"status"`
		Rows   []struct {
			Quality struct {
				Status string `json:"status"`
			} `json:"quality"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Status != "incomplete" || len(comparison.Rows) != 1 || comparison.Rows[0].Quality.Status != "passed" {
		t.Fatalf("unexpected correction comparison: %+v", comparison)
	}
}

func makeCorrectionPNG(t *testing.T) []byte {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 960, 640))
	for y := range 640 {
		for x := range 960 {
			bitmap.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for y := 20; y < 24; y++ {
		for x := 20; x < 940; x++ {
			bitmap.SetRGBA(x, y, color.RGBA{40, 90, 140, 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, bitmap); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func writeCLICandidateConfig(t *testing.T, work string) string {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	config := map[string]any{
		"format": "scanner-eval-candidate", "version": 1,
		"candidate_id": "candidate_cli_helper_001", "executable": executable,
		"executable_sha256": hex.EncodeToString(sum[:]), "argv": []string{"-test.run=^TestCandidateCLIHelper$"},
		"environment": []string{"EVAL_CANDIDATE_CLI_HELPER=1"}, "candidate_version": "1",
		"sources": []any{map[string]string{"name": "scanner-kiosk", "version": "test", "license": "test-only"}},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "candidate-cli.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type cliResult struct {
	stdout, stderr string
	code           int
}

func runHelper(t *testing.T, args ...string) cliResult {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelper$")
	cmd.Env = append(os.Environ(),
		"SCANNER_EVAL_TEST_HELPER=1",
		"SCANNER_EVAL_TEST_ARGS="+string(encoded),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}
