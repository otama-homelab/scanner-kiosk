package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateHelper(t *testing.T) {
	if os.Getenv("EVAL_CANDIDATE_HELPER") != "1" {
		return
	}
	if os.Getenv("SCANNER_EVAL_SECRET") != "" {
		os.Exit(8)
	}
	input := os.Getenv("SCANNER_EVAL_INPUT")
	output := os.Getenv("SCANNER_EVAL_OUTPUT")
	originalID := os.Getenv("SCANNER_EVAL_ORIGINAL_ID")
	pages := strings.Split(os.Getenv("SCANNER_EVAL_SOURCE_PAGES"), ",")
	if input == "" || output == "" || originalID == "" || len(pages) == 0 {
		os.Exit(9)
	}
	mode := os.Getenv("EVAL_CANDIDATE_MODE")
	if err := os.MkdirAll(filepath.Join(output, "texts"), 0o700); err != nil {
		os.Exit(10)
	}
	textPath := filepath.Join(output, "texts", "ocr.txt")
	if mode == "symlink" {
		if err := os.Symlink(input, textPath); err != nil {
			os.Exit(10)
		}
	} else if err := os.WriteFile(textPath, []byte("synthetic candidate text\n"), 0o600); err != nil {
		os.Exit(10)
	}
	artifactPath := "texts/ocr.txt"
	if mode == "traversal" {
		artifactPath = "../escape.txt"
	}
	result := map[string]any{
		"format":       "scanner-eval-candidate-result",
		"version":      1,
		"candidate_id": "candidate_helper_001",
		"artifacts": []any{map[string]any{
			"id": "artifact_candidate_ocr_001", "role": "ocr-text", "path": artifactPath,
			"media_type": "text/plain; charset=utf-8", "source_pages": pages,
			"parents": []string{originalID}, "algorithm": "fixture-helper", "algorithm_version": "1",
			"parameters": map[string]string{"mode": "synthetic"},
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

func TestCandidateRunEndToEnd(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "")

	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	})
	if !errors.Is(err, ErrUnavailable) || catalog.Status != "incomplete" {
		t.Fatalf("expected an incomplete text-only run: catalog=%+v err=%v", catalog, err)
	}
	if catalog.Original.SHA256 != "e2a8b58d65452c9a7e4b471481e2149c9d14502cc75b1c48120ceef9bc860e08" ||
		len(catalog.Artifacts) != 1 || catalog.Artifacts[0].Role != RoleOCRText ||
		catalog.Artifacts[0].SHA256 == "" || catalog.Artifacts[0].Bytes == 0 {
		t.Fatalf("unexpected normalized catalog: %+v", catalog)
	}
	body, err := os.ReadFile(filepath.Join(output, "artifact-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), os.TempDir()) || strings.Contains(string(body), "ocr.txt") {
		t.Fatalf("catalog leaked local paths: %s", body)
	}
}

func TestCandidateRejectsUnsafeOutput(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	for _, mode := range []string{"traversal", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "result")
			configPath := writeCandidateConfig(t, output, mode)
			_, err := RunCandidate(context.Background(), CandidateRunRequest{
				CatalogPath: catalogPath, CandidatePath: configPath,
				FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
			})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe %s output accepted: %v", mode, err)
			}
		})
	}
}

func TestCandidateEnvironmentIsolated(t *testing.T) {
	t.Setenv("SCANNER_EVAL_SECRET", "must-not-reach-candidate")
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "")
	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	})
	if errors.Is(err, ErrUnavailable) && catalog.Status == "" {
		t.Fatalf("candidate inherited the injected secret: %v", err)
	}
}

func writeCandidateConfig(t *testing.T, output, mode string) string {
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
	environment := []string{"EVAL_CANDIDATE_HELPER=1"}
	if mode != "" {
		environment = append(environment, "EVAL_CANDIDATE_MODE="+mode)
	}
	config := map[string]any{
		"format": "scanner-eval-candidate", "version": 1,
		"candidate_id": "candidate_helper_001", "executable": executable,
		"executable_sha256": hex.EncodeToString(sum[:]), "argv": []string{"-test.run=^TestCandidateHelper$"},
		"environment": environment, "candidate_version": "1",
		"sources":        []any{map[string]string{"name": "scanner-kiosk", "version": "test", "license": "test-only"}},
		"dependencies":   []any{},
		"operator_steps": []string{},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(output), "candidate-"+filepath.Base(filepath.Dir(output))+".json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
