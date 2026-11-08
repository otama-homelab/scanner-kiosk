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
	"errors"
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
			ID                   string   `json:"id"`
			SHA256               string   `json:"sha256"`
			ExpectedText         []string `json:"expected_text"`
			ExpectedFailureClass string   `json:"expected_failure_class"`
			Availability         string   `json:"availability"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(body, &generated); err != nil {
		t.Fatal(err)
	}
	var a4 bool
	var expectedFailures int
	for _, fixture := range generated.Fixtures {
		if fixture.ID == "fixture_a4_ja_en_001" && fixture.SHA256 == "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08" &&
			fixture.Availability == "available" && len(fixture.ExpectedText) == 3 {
			a4 = true
		}
		if fixture.ExpectedFailureClass != "" {
			expectedFailures++
			if fixture.Availability != "expected-failure" {
				t.Fatalf("%s generated as %s", fixture.ID, fixture.Availability)
			}
		}
	}
	if !a4 || expectedFailures != 3 {
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
			ID                   string   `json:"id"`
			Roles                []string `json:"expected_outputs"`
			Available            string   `json:"availability"`
			ExpectedTx           []string `json:"expected_text"`
			ExpectedFailureClass string   `json:"expected_failure_class"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &listed); err != nil {
		t.Fatal(err)
	}
	a4 = false
	expectedFailures = 0
	for _, fixture := range listed.Fixtures {
		if fixture.ID == "fixture_a4_ja_en_001" && fixture.Available == "available" && len(fixture.ExpectedTx) == 3 {
			a4 = true
		}
		if fixture.ExpectedFailureClass != "" {
			expectedFailures++
			if fixture.Available != "expected-failure" {
				t.Fatalf("%s listed as %s", fixture.ID, fixture.Available)
			}
		}
	}
	if !a4 || expectedFailures != 3 {
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

func TestCLIRejectsIncompleteCatalog(t *testing.T) {
	catalog := writeIncompleteCatalog(t)
	result := runHelper(t, "list-fixtures", "--catalog", catalog)
	if result.code != 1 {
		t.Fatalf("incomplete catalog exit=%d stderr=%s", result.code, result.stderr)
	}
	if result.stdout != "" || !strings.Contains(result.stderr, "evaluation unavailable") {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if strings.Contains(result.stderr, "fixture_photo_001") || strings.Contains(result.stderr, catalog) {
		t.Fatalf("validation error leaked content: %s", result.stderr)
	}
}

func TestCLIGenerateRejectsIncompleteCatalog(t *testing.T) {
	catalog := writeIncompleteCatalog(t)
	output := filepath.Join(t.TempDir(), "fixtures")
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	result := runHelper(t, "generate-fixtures", "--catalog", catalog, "--output", output, "--manifest", manifest)
	if result.code != 1 {
		t.Fatalf("incomplete catalog exit=%d stderr=%s", result.code, result.stderr)
	}
	if result.stdout != "" || !strings.Contains(result.stderr, "evaluation unavailable") {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output directory created before coverage validation: %v", err)
	}
	if strings.Contains(result.stderr, "fixture_photo_001") || strings.Contains(result.stderr, catalog) {
		t.Fatalf("validation error leaked content: %s", result.stderr)
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
		"sources":        []any{map[string]string{"name": "scanner-kiosk", "version": "test", "license": "test-only"}},
		"operator_steps": []string{},
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

func TestPrivateFixtureRunCLI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	publicCatalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	privateCatalog, privateRoot, _, digest := writePrivateCLIFixture(t, work, "fixture_owner_private_001", "private.png")
	output := filepath.Join(work, "result")
	configPath := writeCLICandidateConfig(t, work)
	result := runHelper(t, "run", "--catalog", publicCatalog, "--candidate", configPath,
		"--fixture", "fixture_owner_private_001", "--output", output,
		"--private-catalog", privateCatalog, "--private-root", privateRoot)
	if result.code != 0 {
		t.Fatalf("private run exit=%d stderr=%s", result.code, result.stderr)
	}
	var catalog struct {
		Status        string `json:"status"`
		FixtureID     string `json:"fixture_id"`
		FixtureSHA256 string `json:"fixture_sha256"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &catalog); err != nil {
		t.Fatalf("invalid result: %v: %s", err, result.stdout)
	}
	if catalog.Status != "complete" || catalog.FixtureID != "fixture_owner_private_001" || catalog.FixtureSHA256 != digest {
		t.Fatalf("unexpected private run result: %+v", catalog)
	}
	body, err := os.ReadFile(filepath.Join(output, "artifact-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{result.stdout, result.stderr, string(body)} {
		for _, secret := range []string{privateRoot, "private.png"} {
			if strings.Contains(output, secret) {
				t.Fatalf("private source leaked %q: %s", secret, output)
			}
		}
	}
}

func TestRunPrivateFixtureFailsClosed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	publicCatalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, privateCatalog, privateRoot string)
		args   func(privateCatalog, privateRoot string) []string
	}{
		{
			name: "missing file",
			mutate: func(t *testing.T, _, privateRoot string) {
				if err := os.Remove(filepath.Join(privateRoot, "owner", "private.png")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "public fixture",
			mutate: func(t *testing.T, _, privateRoot string) {
				if err := os.Chmod(filepath.Join(privateRoot, "owner", "private.png"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "public catalog",
			mutate: func(t *testing.T, privateCatalog, _ string) {
				if err := os.Chmod(privateCatalog, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			mutate: func(t *testing.T, _, privateRoot string) {
				path := filepath.Join(privateRoot, "owner", "private.png")
				target := filepath.Join(privateRoot, "owner", "target.png")
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, body, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip("symlinks unavailable")
				}
			},
		},
		{
			name: "changed bytes",
			mutate: func(t *testing.T, _, privateRoot string) {
				body := makePrivatePNG(t, color.RGBA{R: 12, G: 34, B: 56, A: 255})
				if err := os.WriteFile(filepath.Join(privateRoot, "owner", "private.png"), body, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "changed size",
			mutate: func(t *testing.T, privateCatalog, _ string) {
				rewritePrivateCLICatalog(t, privateCatalog, func(catalog *evaluation.PrivateCatalog) {
					catalog.Fixtures[0].Bytes++
				})
			},
		},
		{
			name: "duplicate id",
			mutate: func(t *testing.T, privateCatalog, _ string) {
				rewritePrivateCLICatalog(t, privateCatalog, func(catalog *evaluation.PrivateCatalog) {
					catalog.Fixtures[0].ID = "fixture_photo_001"
				})
			},
		},
		{
			name: "one-sided flags",
			args: func(privateCatalog, _ string) []string {
				return []string{"--private-catalog", privateCatalog}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := t.TempDir()
			privateCatalog, privateRoot, _, _ := writePrivateCLIFixture(t, work, "fixture_owner_private_001", "private.png")
			if test.mutate != nil {
				test.mutate(t, privateCatalog, privateRoot)
			}
			output := filepath.Join(work, "result")
			args := []string{"run", "--catalog", publicCatalog, "--candidate", writeCLICandidateConfig(t, work),
				"--fixture", "fixture_owner_private_001", "--output", output}
			if test.args != nil {
				args = append(args, test.args(privateCatalog, privateRoot)...)
			} else {
				args = append(args, "--private-catalog", privateCatalog, "--private-root", privateRoot)
			}
			result := runHelper(t, args...)
			if result.code == 0 {
				t.Fatalf("unsafe private fixture accepted: stdout=%s stderr=%s", result.stdout, result.stderr)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate output committed: %v", err)
			}
			for _, stream := range []string{result.stdout, result.stderr} {
				for _, secret := range []string{privateRoot, "private.png"} {
					if strings.Contains(stream, secret) {
						t.Fatalf("private source leaked %q: %s", secret, stream)
					}
				}
			}
		})
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
		Status        string                       `json:"status"`
		FixtureID     string                       `json:"fixture_id"`
		FixtureSHA256 string                       `json:"fixture_sha256"`
		Settings      evaluation.BenchmarkSettings `json:"settings"`
		Samples       []struct {
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
	body, err := os.ReadFile(comparisonPath)
	if err != nil {
		t.Fatal(err)
	}
	var comparison struct {
		Groups []struct {
			FixtureID     string                       `json:"fixture_id"`
			FixtureSHA256 string                       `json:"fixture_sha256"`
			Settings      evaluation.BenchmarkSettings `json:"settings"`
			Entries       []struct {
				Mode           string   `json:"mode"`
				FailureClasses []string `json:"failure_classes"`
			} `json:"entries"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &comparison); err != nil {
		t.Fatal(err)
	}
	if len(comparison.Groups) != 1 ||
		comparison.Groups[0].FixtureID != summary.FixtureID ||
		comparison.Groups[0].FixtureSHA256 != summary.FixtureSHA256 ||
		comparison.Groups[0].Settings != summary.Settings ||
		len(comparison.Groups[0].Entries) != 2 {
		t.Fatalf("comparison lost condition identity: %+v", comparison)
	}
	for _, entry := range comparison.Groups[0].Entries {
		if entry.Mode != "cold" && entry.Mode != "warm" || len(entry.FailureClasses) == 0 {
			t.Fatalf("comparison merged a mode or failure: %+v", entry)
		}
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

func writeIncompleteCatalog(t *testing.T) string {
	t.Helper()
	catalog := filepath.Join(t.TempDir(), "incomplete.json")
	fixture := map[string]any{
		"id":               "fixture_photo_001",
		"source_class":     "public_synthetic",
		"privacy":          "public-synthetic",
		"media_type":       "image/png",
		"page_ids":         []string{"page_photo_001"},
		"document_type":    "photo",
		"expected_outputs": []string{"pdf"},
		"sha256":           "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
		"bytes":            12018,
		"recipe":           map[string]string{"kind": "pattern", "variant": "photo"},
	}
	body, err := json.Marshal(map[string]any{
		"format": "scanner-eval-fixtures", "version": 1,
		"fixtures": []any{fixture},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return catalog
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

func makePrivatePNG(t *testing.T, fill color.RGBA) []byte {
	t.Helper()
	bitmap := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			bitmap.SetRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, bitmap); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func writePrivateCLIFixture(t *testing.T, root, fixtureID, filename string) (string, string, string, string) {
	t.Helper()
	privateRoot := filepath.Join(root, "private-root")
	if err := os.MkdirAll(filepath.Join(privateRoot, "owner"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := makePrivatePNG(t, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	relativePath := filepath.Join("owner", filename)
	if err := os.WriteFile(filepath.Join(privateRoot, relativePath), body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	catalog := evaluation.PrivateCatalog{
		Format: evaluation.CatalogFormat, Version: evaluation.CatalogVersion,
		Fixtures: []evaluation.PrivateFixture{{
			ID: fixtureID, Privacy: evaluation.PrivacyOwnerPrivate, MediaType: "image/png",
			PageIDs: []string{"page_owner_private_001"}, Languages: []string{"ja"},
			DocumentType: "owner-text", ExpectedOutputs: []evaluation.ArtifactRole{evaluation.RoleOCRText},
			RelativePath: relativePath, SHA256: digest, Bytes: int64(len(body)),
		}},
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	privateCatalog := filepath.Join(root, "private-catalog.json")
	if err := os.WriteFile(privateCatalog, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return privateCatalog, privateRoot, relativePath, digest
}

func rewritePrivateCLICatalog(t *testing.T, path string, mutate func(*evaluation.PrivateCatalog)) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var catalog evaluation.PrivateCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	mutate(&catalog)
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
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
		"sources":        []any{map[string]string{"name": "scanner-kiosk", "version": "test", "license": "test-only"}},
		"operator_steps": []string{},
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
