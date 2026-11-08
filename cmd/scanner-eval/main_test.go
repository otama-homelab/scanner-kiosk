package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	if len(generated.Fixtures) != 1 || generated.Fixtures[0].ID != "fixture_a4_ja_en_001" ||
		generated.Fixtures[0].SHA256 != "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08" ||
		generated.Fixtures[0].Availability != "available" {
		t.Fatalf("unexpected manifest: %+v", generated.Fixtures)
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
	if len(listed.Fixtures) != 1 || listed.Fixtures[0].ID != "fixture_a4_ja_en_001" ||
		listed.Fixtures[0].Available != "available" || len(listed.Fixtures[0].ExpectedTx) != 3 {
		t.Fatalf("unexpected list: %s", result.stdout)
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
