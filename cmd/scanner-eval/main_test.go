package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scanner-kiosk/internal/evaluation"
	"scanner-kiosk/internal/pdfassemble"
)

func TestAnalyzeOCRCommand(t *testing.T) {
	oracle := filepath.Join("..", "..", "scripts", "fixtures", "evaluation", "ocr-oracles.json")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "manifest.json")
	body, err := os.ReadFile(oracle)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	partial := map[string]any{"format": "scanner-eval-ocr-run-manifest", "version": 1, "evidence_root": root, "oracle_sha256": hex.EncodeToString(sum[:]), "pairs": []any{}}
	body, _ = json.Marshal(partial)
	if err := os.WriteFile(manifest, body, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(root, "missing.json"), manifest} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"analyze-ocr", "--oracle", oracle, "--manifest", name, "--require-engine", "tesseract-5.5.4", "--require-engine", "ndlocr-lite-1.3.0", "--require-complete-evidence"}, &stdout, &stderr)
		if code == 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), root) {
			t.Fatalf("missing/partial evidence or path disclosure accepted: %d %q %q", code, stdout.String(), stderr.String())
		}
	}
}

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
	if os.Getenv("EVAL_CANDIDATE_CLI_MODE") == "complete" {
		raw, err := os.ReadFile(os.Getenv("SCANNER_EVAL_INPUT"))
		if err != nil {
			os.Exit(12)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			os.Exit(12)
		}
		pdf, err := pdfassemble.Build(context.Background(), []pdfassemble.Page{{
			ID: pages[0], Size: "a4", Color: "color", Width: config.Width, Height: config.Height,
			Open: func(context.Context) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(raw)), nil
			},
		}}, "lossless", pdfassemble.Options{
			MaxInputBytes: 64 << 20, MaxOutputBytes: 64 << 20, MaxPagePixels: 80_000_000,
			MaxPages: 8, JPEGQuality: 92,
		})
		if err != nil {
			os.Exit(12)
		}
		if err := os.WriteFile(filepath.Join(output, "candidate.pdf"), pdf, 0o600); err != nil {
			os.Exit(12)
		}
		result := map[string]any{
			"format": "scanner-eval-candidate-result", "version": 1, "candidate_id": "candidate_cli_helper_001",
			"artifacts": []any{map[string]any{
				"id": "artifact_cli_pdf_0001", "role": "pdf", "path": "candidate.pdf",
				"media_type": "application/pdf", "source_pages": pages,
				"parents": []string{originalID}, "algorithm": "cli-helper", "algorithm_version": "1",
			}},
		}
		body, err := json.Marshal(result)
		if err != nil {
			os.Exit(12)
		}
		if err := os.WriteFile(filepath.Join(output, "result.json"), body, 0o600); err != nil {
			os.Exit(12)
		}
		os.Exit(0)
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
		Original *struct {
			SHA256 string `json:"sha256"`
		} `json:"original"`
		Artifacts []struct {
			Role string `json:"role"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &artifactCatalog); err != nil {
		t.Fatalf("invalid catalog: %v: %s", err, result.stdout)
	}
	if artifactCatalog.Status != "incomplete" || artifactCatalog.Original == nil ||
		artifactCatalog.Original.SHA256 == "" ||
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
	expectedDir := filepath.Join(work, "benchmark-expected-failure")
	expectedResult := runHelper(t, "benchmark", "--catalog", catalog, "--candidate", configPath,
		"--fixture", "fixture_invalid_input_001", "--output", expectedDir, "--cold", "1", "--warm", "1",
		"--timeout-seconds", "20", "--max-rss-mib", "2048", "--max-temp-disk-mib", "1024")
	if expectedResult.code != 0 {
		t.Fatalf("expected-failure benchmark exit=%d stderr=%s", expectedResult.code, expectedResult.stderr)
	}
	var expectedSummary struct {
		Status               string                       `json:"status"`
		FixtureID            string                       `json:"fixture_id"`
		FixtureSHA256        string                       `json:"fixture_sha256"`
		Settings             evaluation.BenchmarkSettings `json:"settings"`
		ExpectedFailureClass string                       `json:"expected_failure_class"`
		ObservedFailureClass string                       `json:"observed_failure_class"`
		FailureMatch         *bool                        `json:"failure_match"`
		Samples              []any                        `json:"samples"`
	}
	if err := json.Unmarshal([]byte(expectedResult.stdout), &expectedSummary); err != nil {
		t.Fatal(err)
	}
	if expectedSummary.Status != "expected-failure" || expectedSummary.ExpectedFailureClass != "invalid-input" ||
		expectedSummary.ObservedFailureClass != "invalid-input" || expectedSummary.FailureMatch == nil ||
		!*expectedSummary.FailureMatch || len(expectedSummary.Samples) != 0 {
		t.Fatalf("unexpected expected-failure benchmark: %+v", expectedSummary)
	}
	comparisonPath := filepath.Join(work, "comparison.json")
	compare := runHelper(t, "compare", "--receipt", runDir, "--receipt", expectedDir, "--output", comparisonPath)
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
			FixtureID            string                       `json:"fixture_id"`
			FixtureSHA256        string                       `json:"fixture_sha256"`
			Settings             evaluation.BenchmarkSettings `json:"settings"`
			ExpectedFailureClass string                       `json:"expected_failure_class"`
			ObservedFailureClass string                       `json:"observed_failure_class"`
			FailureMatch         *bool                        `json:"failure_match"`
			Entries              []struct {
				Mode                 string   `json:"mode"`
				Status               string   `json:"status"`
				Samples              int      `json:"samples"`
				P50Millis            *int64   `json:"p50_millis"`
				P95Millis            *int64   `json:"p95_millis"`
				PeakRSSBytes         int64    `json:"peak_rss_bytes"`
				SampledTempDiskBytes int64    `json:"sampled_temp_disk_bytes"`
				FailureClasses       []string `json:"failure_classes"`
			} `json:"entries"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &comparison); err != nil {
		t.Fatal(err)
	}
	if len(comparison.Groups) != 2 {
		t.Fatalf("comparison group count=%d, want 2: %+v", len(comparison.Groups), comparison)
	}
	var foundOrdinary, foundExpected bool
	for _, group := range comparison.Groups {
		if group.FixtureID == summary.FixtureID {
			foundOrdinary = true
			if group.FixtureSHA256 != summary.FixtureSHA256 || group.Settings != summary.Settings ||
				len(group.Entries) != 2 {
				t.Fatalf("comparison lost ordinary condition identity: %+v", group)
			}
			for _, entry := range group.Entries {
				if entry.Mode != "cold" && entry.Mode != "warm" || len(entry.FailureClasses) == 0 {
					t.Fatalf("comparison merged an ordinary mode or failure: %+v", entry)
				}
			}
		}
		if group.FixtureID == expectedSummary.FixtureID {
			foundExpected = true
			if group.FixtureSHA256 != expectedSummary.FixtureSHA256 || group.Settings != expectedSummary.Settings ||
				group.ExpectedFailureClass != expectedSummary.ExpectedFailureClass ||
				group.ObservedFailureClass != expectedSummary.ObservedFailureClass ||
				group.FailureMatch == nil || !*group.FailureMatch || len(group.Entries) != 2 {
				t.Fatalf("comparison lost expected-failure identity: %+v", group)
			}
			for _, entry := range group.Entries {
				if entry.Samples != 0 || entry.P50Millis != nil || entry.P95Millis != nil ||
					entry.PeakRSSBytes != 0 || entry.SampledTempDiskBytes != 0 {
					t.Fatalf("comparison fabricated expected-failure metrics: %+v", entry)
				}
			}
		}
	}
	if !foundOrdinary || !foundExpected {
		t.Fatalf("comparison omitted an ordinary or expected-failure group: %+v", comparison)
	}
}

func TestPrivateFixtureBenchmarkEndToEnd(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	publicCatalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	privateCatalog, privateRoot, _, digest := writePrivateCLIFixtureForOutputs(
		t, work, "fixture_owner_private_pdf_001", "private.pdf.png", []evaluation.ArtifactRole{evaluation.RolePDF},
	)
	runDir := filepath.Join(work, "benchmark")
	configPath := writeCLICandidateConfigMode(t, work, "complete")
	result := runHelper(t, "benchmark", "--catalog", publicCatalog, "--candidate", configPath,
		"--fixture", "fixture_owner_private_pdf_001", "--output", runDir, "--cold", "1", "--warm", "1",
		"--timeout-seconds", "20", "--max-rss-mib", "2048", "--max-temp-disk-mib", "1024",
		"--private-catalog", privateCatalog, "--private-root", privateRoot)
	if result.code != 0 {
		t.Fatalf("private benchmark exit=%d stderr=%s", result.code, result.stderr)
	}
	var summary struct {
		Status        string `json:"status"`
		FixtureID     string `json:"fixture_id"`
		FixtureSHA256 string `json:"fixture_sha256"`
		Samples       []struct {
			FixtureSHA256 string `json:"fixture_sha256"`
		} `json:"samples"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &summary); err != nil {
		t.Fatalf("invalid summary: %v: %s", err, result.stdout)
	}
	if summary.Status != "complete" || summary.FixtureID != "fixture_owner_private_pdf_001" ||
		summary.FixtureSHA256 != digest || len(summary.Samples) != 2 {
		t.Fatalf("unexpected private benchmark: %+v", summary)
	}
	for _, sample := range summary.Samples {
		if sample.FixtureSHA256 != digest {
			t.Fatalf("sample lost private fixture identity: %+v", sample)
		}
	}
	if err := filepath.WalkDir(runDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range []string{privateRoot, "private.pdf.png"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("private source leaked into %s: %s", path, body)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	comparisonPath := filepath.Join(work, "comparison.json")
	compare := runHelper(t, "compare", "--receipt", runDir, "--output", comparisonPath)
	if compare.code != 0 {
		t.Fatalf("compare failed: %s", compare.stderr)
	}
	body, err := os.ReadFile(comparisonPath)
	if err != nil {
		t.Fatal(err)
	}
	var comparison struct {
		Groups []struct {
			FixtureID     string `json:"fixture_id"`
			FixtureSHA256 string `json:"fixture_sha256"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &comparison); err != nil {
		t.Fatal(err)
	}
	if len(comparison.Groups) != 1 || comparison.Groups[0].FixtureID != summary.FixtureID ||
		comparison.Groups[0].FixtureSHA256 != digest {
		t.Fatalf("comparison lost private fixture identity: %+v", comparison)
	}
}

func TestPrivateFixtureBenchmarkMissingIsUnavailable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	publicCatalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	privateCatalog, privateRoot, _, _ := writePrivateCLIFixtureForOutputs(
		t, work, "fixture_owner_private_pdf_001", "private.pdf.png", []evaluation.ArtifactRole{evaluation.RolePDF},
	)
	if err := os.Remove(filepath.Join(privateRoot, "owner", "private.pdf.png")); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(work, "benchmark")
	result := runHelper(t, "benchmark", "--catalog", publicCatalog,
		"--candidate", writeCLICandidateConfigMode(t, work, "complete"),
		"--fixture", "fixture_owner_private_pdf_001", "--output", runDir, "--cold", "1", "--warm", "1",
		"--timeout-seconds", "20", "--max-rss-mib", "2048", "--max-temp-disk-mib", "1024",
		"--private-catalog", privateCatalog, "--private-root", privateRoot)
	if result.code != 1 {
		t.Fatalf("missing private benchmark exit=%d stderr=%s", result.code, result.stderr)
	}
	var summary struct {
		Status       string `json:"status"`
		FailureClass string `json:"failure_class"`
		Samples      []any  `json:"samples"`
		Metrics      struct {
			Cold struct {
				Samples int `json:"samples"`
			} `json:"cold"`
			Warm struct {
				Samples int `json:"samples"`
			} `json:"warm"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &summary); err != nil {
		t.Fatalf("invalid unavailable summary: %v: %s", err, result.stdout)
	}
	if summary.Status != "unavailable" || summary.FailureClass != "fixture-unavailable" ||
		len(summary.Samples) != 0 || summary.Metrics.Cold.Samples != 0 || summary.Metrics.Warm.Samples != 0 {
		t.Fatalf("missing private fixture became a sample: %+v", summary)
	}
	if _, err := os.Stat(filepath.Join(runDir, "repetitions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate repetitions were created: %v", err)
	}
}

func TestNegativeFixtureRunReconcilesExpectedFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	catalog, err := evaluation.LoadCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"fixture_invalid_input_001":      "invalid-input",
		"fixture_oversized_input_001":    "oversized-input",
		"fixture_dependency_failure_001": "dependency-unavailable",
	}
	for fixtureID, class := range expected {
		t.Run(fixtureID, func(t *testing.T) {
			var wantSHA string
			for _, fixture := range catalog.Fixtures {
				if fixture.ID == fixtureID {
					wantSHA = fixture.SHA256
				}
			}
			work := t.TempDir()
			output := filepath.Join(work, "result")
			marker := filepath.Join(work, "candidate-launched")
			result := runHelper(t, "run", "--catalog", catalogPath,
				"--candidate", writeMarkerCandidateConfig(t, work, marker),
				"--fixture", fixtureID, "--output", output)
			if result.code != 0 {
				t.Fatalf("expected failure exit=%d stderr=%s", result.code, result.stderr)
			}
			var receipt struct {
				Status               string `json:"status"`
				FixtureID            string `json:"fixture_id"`
				FixtureSHA256        string `json:"fixture_sha256"`
				ExpectedFailureClass string `json:"expected_failure_class"`
				ObservedFailureClass string `json:"observed_failure_class"`
				FailureMatch         *bool  `json:"failure_match"`
				Artifacts            []any  `json:"artifacts"`
			}
			if err := json.Unmarshal([]byte(result.stdout), &receipt); err != nil {
				t.Fatalf("invalid expected-failure receipt: %v: %s", err, result.stdout)
			}
			if receipt.Status != "expected-failure" || receipt.FixtureID != fixtureID ||
				receipt.FixtureSHA256 != wantSHA || receipt.ExpectedFailureClass != class ||
				receipt.ObservedFailureClass != class || receipt.FailureMatch == nil || !*receipt.FailureMatch ||
				len(receipt.Artifacts) != 0 {
				t.Fatalf("unexpected expected-failure receipt: %+v", receipt)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(result.stdout), &raw); err != nil {
				t.Fatal(err)
			}
			if _, ok := raw["original"]; ok {
				t.Fatalf("expected-failure receipt fabricated an original: %s", result.stdout)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate process was launched: %v", err)
			}
			body, err := os.ReadFile(filepath.Join(output, "artifact-catalog.json"))
			if err != nil {
				t.Fatal(err)
			}
			var persisted struct {
				Status               string `json:"status"`
				ExpectedFailureClass string `json:"expected_failure_class"`
				ObservedFailureClass string `json:"observed_failure_class"`
				FailureMatch         *bool  `json:"failure_match"`
			}
			if err := json.Unmarshal(body, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Status != receipt.Status || persisted.ExpectedFailureClass != receipt.ExpectedFailureClass ||
				persisted.ObservedFailureClass != receipt.ObservedFailureClass ||
				persisted.FailureMatch == nil || !*persisted.FailureMatch {
				t.Fatalf("persisted receipt differs: stdout=%+v file=%+v", receipt, persisted)
			}
		})
	}
}

func TestNegativeFixtureBenchmarkReconcilesExpectedFailure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	expected := map[string]string{
		"fixture_invalid_input_001":      "invalid-input",
		"fixture_oversized_input_001":    "oversized-input",
		"fixture_dependency_failure_001": "dependency-unavailable",
	}
	for fixtureID, class := range expected {
		t.Run(fixtureID, func(t *testing.T) {
			work := t.TempDir()
			runDir := filepath.Join(work, "benchmark")
			marker := filepath.Join(work, "candidate-launched")
			result := runHelper(t, "benchmark", "--catalog", catalogPath,
				"--candidate", writeMarkerCandidateConfig(t, work, marker),
				"--fixture", fixtureID, "--output", runDir, "--cold", "1", "--warm", "1",
				"--timeout-seconds", "20", "--max-rss-mib", "2048", "--max-temp-disk-mib", "1024")
			if result.code != 0 {
				t.Fatalf("expected-failure benchmark exit=%d stderr=%s", result.code, result.stderr)
			}
			var summary struct {
				Status               string `json:"status"`
				FixtureID            string `json:"fixture_id"`
				ExpectedFailureClass string `json:"expected_failure_class"`
				ObservedFailureClass string `json:"observed_failure_class"`
				FailureMatch         *bool  `json:"failure_match"`
				Samples              []any  `json:"samples"`
				Metrics              struct {
					Cold struct {
						Samples int `json:"samples"`
					} `json:"cold"`
					Warm struct {
						Samples int `json:"samples"`
					} `json:"warm"`
				} `json:"metrics"`
			}
			if err := json.Unmarshal([]byte(result.stdout), &summary); err != nil {
				t.Fatalf("invalid expected-failure summary: %v: %s", err, result.stdout)
			}
			if summary.Status != "expected-failure" || summary.FixtureID != fixtureID ||
				summary.ExpectedFailureClass != class || summary.ObservedFailureClass != class ||
				summary.FailureMatch == nil || !*summary.FailureMatch || len(summary.Samples) != 0 ||
				summary.Metrics.Cold.Samples != 0 || summary.Metrics.Warm.Samples != 0 {
				t.Fatalf("unexpected expected-failure summary: %+v", summary)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate process was launched: %v", err)
			}
			if _, err := os.Stat(filepath.Join(runDir, "repetitions")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate repetitions were created: %v", err)
			}
		})
	}
}

func TestNegativeFixtureFailureMismatchFailsClosed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	publicCatalog := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	work := t.TempDir()
	privateCatalog, privateRoot, _, digest := writePrivateCLIFixtureForOutputs(
		t, work, "fixture_owner_private_mismatch_001", "private.png", nil,
	)
	rewritePrivateCLICatalog(t, privateCatalog, func(catalog *evaluation.PrivateCatalog) {
		catalog.Fixtures[0].ExpectedFailureClass = "invalid-input"
	})
	output := filepath.Join(work, "result")
	marker := filepath.Join(work, "candidate-launched")
	result := runHelper(t, "run", "--catalog", publicCatalog,
		"--candidate", writeMarkerCandidateConfig(t, work, marker),
		"--fixture", "fixture_owner_private_mismatch_001", "--output", output,
		"--private-catalog", privateCatalog, "--private-root", privateRoot)
	if result.code != 1 {
		t.Fatalf("mismatched expected failure exit=%d stderr=%s", result.code, result.stderr)
	}
	var receipt struct {
		Status               string `json:"status"`
		FixtureID            string `json:"fixture_id"`
		FixtureSHA256        string `json:"fixture_sha256"`
		ExpectedFailureClass string `json:"expected_failure_class"`
		ObservedFailureClass string `json:"observed_failure_class"`
		FailureMatch         *bool  `json:"failure_match"`
		Artifacts            []any  `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &receipt); err != nil {
		t.Fatalf("invalid mismatch receipt: %v: %s", err, result.stdout)
	}
	if receipt.Status == "expected-failure" || receipt.FixtureID != "fixture_owner_private_mismatch_001" ||
		receipt.FixtureSHA256 != digest || receipt.ExpectedFailureClass != "invalid-input" ||
		receipt.ObservedFailureClass != "expected-class-mismatch" ||
		receipt.FailureMatch == nil || *receipt.FailureMatch || len(receipt.Artifacts) != 0 {
		t.Fatalf("unexpected mismatch receipt: %+v", receipt)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate process was launched: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(output, "artifact-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		ExpectedFailureClass string `json:"expected_failure_class"`
		ObservedFailureClass string `json:"observed_failure_class"`
		FailureMatch         *bool  `json:"failure_match"`
	}
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ExpectedFailureClass != receipt.ExpectedFailureClass ||
		persisted.ObservedFailureClass != receipt.ObservedFailureClass ||
		persisted.FailureMatch == nil || *persisted.FailureMatch {
		t.Fatalf("persisted mismatch receipt differs: stdout=%+v file=%+v", receipt, persisted)
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
		FixtureSHA256:         "84e3dc4c4334c0fedaa3a5caedef884587c40497402328a59a4ace1759514f81",
		CandidateConfigSHA256: strings.Repeat("c", 64),
		Candidate: evaluation.CandidateReceipt{
			ID: "candidate_correction_cli_001", Version: "1", ExecutableSHA256: strings.Repeat("a", 64),
			Sources:       []evaluation.CandidatePin{{Name: "fixture", Version: "1", License: "test-only"}},
			OperatorSteps: []string{},
			Parameters:    map[string]string{},
		},
		Status: "complete",
		Original: &evaluation.Artifact{
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
	benchmarkPath := writeCorrectionCLIBenchmark(t, work, catalog)
	result := runHelper(t, "analyze-corrections",
		"--catalog", filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json"),
		"--oracles", filepath.Join(root, "scripts", "fixtures", "evaluation", "correction-oracles.json"),
		"--artifacts", artifactPath,
		"--benchmark", benchmarkPath)
	if result.code != 0 {
		t.Fatalf("incomplete measurement exit=%d stderr=%s", result.code, result.stderr)
	}
	var comparison struct {
		Status string `json:"status"`
		Rows   []struct {
			Quality struct {
				Status              string `json:"status"`
				ContentMeasurements []struct {
					OriginalForeground int `json:"original_foreground_pixels"`
					MappedForeground   int `json:"mapped_foreground_pixels"`
					RetainedForeground int `json:"retained_foreground_pixels"`
				} `json:"content_measurements"`
			} `json:"quality"`
			Resources struct {
				Status                string `json:"status"`
				CandidateConfigSHA256 string `json:"candidate_config_sha256"`
				Metrics               struct {
					Cold evaluation.ModeMetrics `json:"cold"`
					Warm evaluation.ModeMetrics `json:"warm"`
				} `json:"metrics"`
			} `json:"resources"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Status != "complete" || len(comparison.Rows) != 1 || comparison.Rows[0].Quality.Status != "passed" ||
		len(comparison.Rows[0].Quality.ContentMeasurements) != 1 ||
		comparison.Rows[0].Quality.ContentMeasurements[0].OriginalForeground == 0 ||
		comparison.Rows[0].Quality.ContentMeasurements[0].MappedForeground !=
			comparison.Rows[0].Quality.ContentMeasurements[0].RetainedForeground ||
		comparison.Rows[0].Resources.Status != "complete" ||
		comparison.Rows[0].Resources.CandidateConfigSHA256 != strings.Repeat("c", 64) ||
		comparison.Rows[0].Resources.Metrics.Cold.Samples != 1 ||
		comparison.Rows[0].Resources.Metrics.Warm.Samples != 1 {
		t.Fatalf("unexpected correction comparison: %+v", comparison)
	}

	withoutBenchmark := runHelper(t, "analyze-corrections",
		"--catalog", filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json"),
		"--oracles", filepath.Join(root, "scripts", "fixtures", "evaluation", "correction-oracles.json"),
		"--artifacts", artifactPath)
	if withoutBenchmark.code == 0 || withoutBenchmark.stdout != "" {
		t.Fatalf("quality-only CLI run passed: code=%d stdout=%s", withoutBenchmark.code, withoutBenchmark.stdout)
	}
}

func writeCorrectionCLIBenchmark(t *testing.T, root string, catalog evaluation.ArtifactCatalog) string {
	t.Helper()
	configSHA := strings.Repeat("c", 64)
	cold, warm := int64(100), int64(80)
	summary := evaluation.BenchmarkSummary{
		Format: evaluation.BenchmarkFormat, Version: evaluation.BenchmarkVersion,
		RunID: "benchmark_correction_cli_001", Status: "complete",
		StartedAt: "2026-11-09T00:00:00Z", FinishedAt: "2026-11-09T00:00:02Z",
		FixtureID: catalog.FixtureID, FixtureSHA256: catalog.FixtureSHA256,
		Candidate: catalog.Candidate, CandidateConfigSHA256: configSHA,
		Parameters:         map[string]string{},
		InvocationTemplate: []string{"/usr/bin/time", "{candidate}"},
		Observer: evaluation.ObserverEvidence{
			Path: "/usr/bin/time", SHA256: strings.Repeat("d", 64), Version: "time",
			License: "GPL-3.0-or-later",
		},
		Settings: evaluation.BenchmarkSettings{
			ColdRepetitions: 1, WarmRepetitions: 1, TimeoutSeconds: 900,
			MaxRSSBytes: 1 << 30, MaxTempDiskBytes: 1 << 30,
		},
		Metrics: evaluation.BenchmarkMetrics{
			Cold: evaluation.ModeMetrics{Samples: 1, P50Millis: &cold, P95Millis: &cold, PeakRSSBytes: 1000},
			Warm: evaluation.ModeMetrics{Samples: 1, P50Millis: &warm, P95Millis: &warm, PeakRSSBytes: 900},
		},
	}
	for index, mode := range []string{"cold", "warm"} {
		summary.Samples = append(summary.Samples, evaluation.BenchmarkSample{
			SampleID: summary.RunID + "_" + mode, Mode: mode, Sequence: index + 1,
			StartedAt: summary.StartedAt, FinishedAt: summary.FinishedAt,
			FixtureID: summary.FixtureID, FixtureSHA256: summary.FixtureSHA256,
			Candidate: summary.Candidate, CandidateConfigSHA256: configSHA,
			Parameters: map[string]string{}, WallMillis: 100, PeakRSSBytes: 1000,
			Status: "complete", ValidationStatus: "passed",
			ArtifactCatalog: "artifact-catalog.json", ReceiptPath: "receipts/" + mode + ".json",
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
	body, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "benchmark.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
	for y := 120; y < 140; y++ {
		for x := 120; x < 300; x++ {
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
	return writePrivateCLIFixtureForOutputs(t, root, fixtureID, filename, []evaluation.ArtifactRole{evaluation.RoleOCRText})
}

func writePrivateCLIFixtureForOutputs(t *testing.T, root, fixtureID, filename string, outputs []evaluation.ArtifactRole) (string, string, string, string) {
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
			DocumentType: "owner-text", ExpectedOutputs: outputs,
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
	return writeCLICandidateConfigMode(t, work, "")
}

func writeCLICandidateConfigMode(t *testing.T, work, mode string) string {
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
	environment := []string{"EVAL_CANDIDATE_CLI_HELPER=1"}
	if mode != "" {
		environment = append(environment, "EVAL_CANDIDATE_CLI_MODE="+mode)
	}
	config := map[string]any{
		"format": "scanner-eval-candidate", "version": 1,
		"candidate_id": "candidate_cli_helper_001", "executable": executable,
		"executable_sha256": hex.EncodeToString(sum[:]), "argv": []string{"-test.run=^TestCandidateCLIHelper$"},
		"environment": environment, "candidate_version": "1",
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

func writeMarkerCandidateConfig(t *testing.T, work, marker string) string {
	t.Helper()
	executable, err := filepath.EvalSymlinks("/bin/sh")
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
		"candidate_id": "candidate_marker_001", "executable": executable,
		"executable_sha256": hex.EncodeToString(sum[:]), "argv": []string{"-c", "touch " + marker},
		"environment": []string{}, "candidate_version": "1",
		"sources":        []any{map[string]string{"name": "shell", "version": "test", "license": "test-only"}},
		"dependencies":   []any{},
		"operator_steps": []string{},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "candidate-marker.json")
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
