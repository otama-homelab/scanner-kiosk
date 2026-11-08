package evaluation

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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"scanner-kiosk/internal/pdfassemble"
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
	candidateID := os.Getenv("EVAL_CANDIDATE_ID")
	if candidateID == "" {
		candidateID = "candidate_helper_001"
	}
	pages := strings.Split(os.Getenv("SCANNER_EVAL_SOURCE_PAGES"), ",")
	if input == "" || output == "" || originalID == "" || len(pages) == 0 {
		os.Exit(9)
	}
	mode := os.Getenv("EVAL_CANDIDATE_MODE")
	if mode == "correction" || mode == "destructive_correction" {
		raw, err := os.ReadFile(input)
		if err != nil {
			os.Exit(13)
		}
		decoded, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			os.Exit(13)
		}
		bounds := decoded.Bounds()
		rect := bounds
		if mode == "destructive_correction" {
			rect = image.Rect(bounds.Min.X+bounds.Dx()/10, bounds.Min.Y+bounds.Dy()/10,
				bounds.Max.X-bounds.Dx()/10, bounds.Max.Y-bounds.Dy()/10)
		}
		corrected := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		for y := 0; y < rect.Dy(); y++ {
			for x := 0; x < rect.Dx(); x++ {
				corrected.Set(x, y, decoded.At(rect.Min.X+x, rect.Min.Y+y))
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, corrected); err != nil {
			os.Exit(13)
		}
		if err := os.WriteFile(filepath.Join(output, "corrected.png"), encoded.Bytes(), 0o600); err != nil {
			os.Exit(13)
		}
		quad := [][2]int{{rect.Min.X, rect.Min.Y}, {rect.Max.X, rect.Min.Y}, {rect.Max.X, rect.Max.Y}, {rect.Min.X, rect.Max.Y}}
		quadJSON, err := json.Marshal(quad)
		if err != nil {
			os.Exit(13)
		}
		result := map[string]any{
			"format": "scanner-eval-candidate-result", "version": 1, "candidate_id": candidateID,
			"artifacts": []any{map[string]any{
				"id": "artifact_candidate_correction_001", "role": "correction", "path": "corrected.png",
				"media_type": "image/png", "source_pages": pages,
				"parents": []string{originalID}, "algorithm": "fixture-crop", "algorithm_version": "1",
				"parameters": map[string]string{
					"crop_quad": string(quadJSON), "rotation_degrees": "0",
					"output_width": strconv.Itoa(rect.Dx()), "output_height": strconv.Itoa(rect.Dy()),
					"confidence_kind": "unavailable", "fallback_reason": "",
				},
			}},
		}
		body, err := json.Marshal(result)
		if err != nil {
			os.Exit(13)
		}
		if err := os.WriteFile(filepath.Join(output, "result.json"), body, 0o600); err != nil {
			os.Exit(13)
		}
		os.Exit(0)
	}
	if mode == "complete" || mode == "replacement_image" {
		raw, err := os.ReadFile(input)
		if err != nil {
			os.Exit(12)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			os.Exit(12)
		}
		pdfBody := raw
		algorithm := "fixture-assembler"
		if mode == "replacement_image" {
			replacement := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
			for y := 0; y < config.Height; y++ {
				for x := 0; x < config.Width; x++ {
					replacement.SetRGBA(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), 173, 255})
				}
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, replacement); err != nil {
				os.Exit(12)
			}
			pdfBody = encoded.Bytes()
			algorithm = "fixture-assembler-replacement"
		}
		pdf, err := pdfassemble.Build(context.Background(), []pdfassemble.Page{{
			ID: pages[0], Size: "a4", Color: "color", Width: config.Width, Height: config.Height,
			Open: func(context.Context) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(pdfBody)), nil
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
			"format": "scanner-eval-candidate-result", "version": 1, "candidate_id": candidateID,
			"artifacts": []any{map[string]any{
				"id": "artifact_candidate_pdf_001", "role": "pdf", "path": "candidate.pdf",
				"media_type": "application/pdf", "source_pages": pages,
				"parents": []string{originalID}, "algorithm": algorithm, "algorithm_version": "1",
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
	if mode == "mutate" {
		if err := os.Chmod(input, 0o600); err != nil {
			os.Exit(11)
		}
		if err := os.WriteFile(input, []byte("mutated"), 0o600); err != nil {
			os.Exit(11)
		}
	}
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
	if mode == "empty_text" {
		if err := os.WriteFile(textPath, nil, 0o600); err != nil {
			os.Exit(10)
		}
	}
	artifactPath := "texts/ocr.txt"
	if mode == "traversal" {
		artifactPath = "../escape.txt"
	}
	artifact := map[string]any{
		"id": "artifact_candidate_ocr_001", "role": "ocr-text", "path": artifactPath,
		"media_type": "text/plain; charset=utf-8", "source_pages": pages,
		"parents": []string{originalID}, "algorithm": "fixture-helper", "algorithm_version": "1",
		"parameters": map[string]string{"mode": "synthetic"},
	}
	if mode == "bad_hash" {
		artifact["sha256"] = strings.Repeat("0", 64)
	}
	artifacts := []any{artifact}
	if mode == "invalid_pdf" {
		positioned := map[string]any{
			"id": "artifact_candidate_positioned_001", "role": "positioned-text", "path": "texts/positioned.json",
			"media_type": "application/json", "source_pages": pages, "parents": []string{"artifact_candidate_ocr_001"},
			"algorithm": "fixture-helper", "algorithm_version": "1",
		}
		pdf := map[string]any{
			"id": "artifact_candidate_pdf_001", "role": "pdf", "path": "candidate.pdf",
			"media_type": "application/pdf", "source_pages": pages, "parents": []string{"artifact_candidate_ocr_001"},
			"algorithm": "fixture-helper", "algorithm_version": "1",
		}
		if err := os.WriteFile(filepath.Join(output, "texts", "positioned.json"), []byte(`{"blocks":[]}`), 0o600); err != nil {
			os.Exit(10)
		}
		if err := os.WriteFile(filepath.Join(output, "candidate.pdf"), []byte("not a pdf"), 0o600); err != nil {
			os.Exit(10)
		}
		artifacts = append(artifacts, positioned, pdf)
	}
	result := map[string]any{
		"format":       "scanner-eval-candidate-result",
		"version":      1,
		"candidate_id": candidateID,
		"artifacts":    artifacts,
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

func TestCandidateRunRequiresOriginalPixelRetention(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "complete")

	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_photo_001", OutputDir: output,
	})
	if err != nil || catalog.Status != "complete" || catalog.Validation == nil ||
		catalog.Validation.Status != "passed" {
		t.Fatalf("expected a complete PDF run: catalog=%+v err=%v", catalog, err)
	}
	for _, check := range catalog.Validation.Checks {
		if check.Name == "lossless-original-pixels" && check.Status == "pass" {
			return
		}
	}
	t.Fatalf("run-path validation omitted original-pixel retention: %+v", catalog.Validation)
}

func TestCandidateRunRejectsReplacementPDFImage(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "replacement_image")

	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_photo_001", OutputDir: output,
	})
	if !errors.Is(err, ErrUnavailable) || catalog.Status != "incomplete" ||
		catalog.Validation == nil || catalog.Validation.FailureClass != "pixel-mismatch" {
		t.Fatalf("replacement-image PDF was not rejected: catalog=%+v err=%v", catalog, err)
	}
	for _, check := range catalog.Validation.Checks {
		if check.Name == "lossless-original-pixels" && check.Status == "fail" {
			return
		}
	}
	t.Fatalf("replacement-image receipt omitted the failed pixel check: %+v", catalog.Validation)
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

func TestArtifactRoleCoverage(t *testing.T) {
	fixture := fixtureForArtifactTest()
	roles := map[ArtifactRole]string{
		RoleCorrection:     "image/png",
		RoleOCRText:        "text/plain; charset=utf-8",
		RolePositionedText: "application/json",
		RoleStructure:      "application/json",
		RoleGenerated:      "application/json",
		RolePDF:            "application/pdf",
	}
	for role, mediaType := range roles {
		t.Run(string(role), func(t *testing.T) {
			result := CandidateResult{
				Format: CandidateResultFormat, Version: CandidateResultVersion, CandidateID: "candidate_role_001",
				Artifacts: []CandidateArtifact{artifactForTest(role, mediaType)},
			}
			if err := validateCandidateResult(result, result.CandidateID, fixture, "artifact_original_test"); err != nil {
				t.Fatal(err)
			}
			if validRoleMediaType(role, "application/octet-stream") {
				t.Fatalf("role %s accepted the wrong media type", role)
			}
		})
	}
}

func TestArtifactProvenance(t *testing.T) {
	fixture := fixtureForArtifactTest()
	base := func() CandidateResult {
		return CandidateResult{
			Format: CandidateResultFormat, Version: CandidateResultVersion, CandidateID: "candidate_role_001",
			Artifacts: []CandidateArtifact{artifactForTest(RoleOCRText, "text/plain; charset=utf-8")},
		}
	}
	tests := []struct {
		name   string
		mutate func(*CandidateResult)
	}{
		{"missing page", func(result *CandidateResult) { result.Artifacts[0].SourcePages = nil }},
		{"invalid page", func(result *CandidateResult) { result.Artifacts[0].SourcePages[0] = "page_other_001" }},
		{"missing parent", func(result *CandidateResult) { result.Artifacts[0].Parents = []string{"artifact_missing_parent"} }},
		{"duplicate id", func(result *CandidateResult) {
			result.Artifacts = append(result.Artifacts, result.Artifacts[0])
			result.Artifacts[1].Path = "other.txt"
		}},
		{"duplicate path", func(result *CandidateResult) {
			other := artifactForTest(RoleGenerated, "application/json")
			other.Path = result.Artifacts[0].Path
			result.Artifacts = append(result.Artifacts, other)
		}},
		{"control characters", func(result *CandidateResult) { result.Artifacts[0].Algorithm = "bad\x01algorithm" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := base()
			test.mutate(&result)
			if err := validateCandidateResult(result, result.CandidateID, fixture, "artifact_original_test"); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid provenance accepted: %v", err)
			}
		})
	}

	cycle := base()
	cycle.Artifacts = []CandidateArtifact{
		artifactForTest(RoleOCRText, "text/plain; charset=utf-8"),
		artifactForTest(RoleGenerated, "application/json"),
	}
	cycle.Artifacts[0].Parents = []string{cycle.Artifacts[1].ID}
	cycle.Artifacts[1].Parents = []string{cycle.Artifacts[0].ID}
	if err := validateCandidateResult(cycle, cycle.CandidateID, fixture, "artifact_original_test"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("parent cycle accepted: %v", err)
	}
}

func TestGeneratedTextIsNotTranscription(t *testing.T) {
	fixture := fixtureForArtifactTest()
	result := CandidateResult{
		Format: CandidateResultFormat, Version: CandidateResultVersion, CandidateID: "candidate_role_001",
		Artifacts: []CandidateArtifact{artifactForTest(RoleGenerated, "application/json")},
	}
	if err := validateCandidateResult(result, result.CandidateID, fixture, "artifact_original_test"); err != nil {
		t.Fatal(err)
	}
	artifacts := []Artifact{{
		ID: result.Artifacts[0].ID, Role: RoleGenerated, MediaType: result.Artifacts[0].MediaType,
		SourcePages: result.Artifacts[0].SourcePages, Parents: result.Artifacts[0].Parents,
		Algorithm: result.Artifacts[0].Algorithm, AlgorithmVersion: result.Artifacts[0].AlgorithmVersion,
	}}
	missing := missingOutputs(fixture, artifacts)
	if len(missing) != 2 || missing[0] != RoleOCRText || missing[1] != RolePDF {
		t.Fatalf("generated content satisfied transcription or PDF: %v", missing)
	}
}

func TestEmptyOCROutputIsPresent(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "empty_text")
	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	})
	if !errors.Is(err, ErrUnavailable) || len(catalog.Artifacts) != 1 ||
		catalog.Artifacts[0].Role != RoleOCRText || catalog.Artifacts[0].Bytes != 0 {
		t.Fatalf("zero-byte OCR artifact was not preserved: catalog=%+v err=%v", catalog, err)
	}
}

func TestCandidateDeclaredHashMismatchFailsClosed(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "bad_hash")
	if _, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("candidate-declared hash mismatch accepted: %v", err)
	}
}

func TestOriginalRemainsImmutable(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "mutate")
	if _, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mutated original was accepted: %v", err)
	}
}

func TestRunRejectsInvalidPDFEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	catalogPath := filepath.Join(root, "scripts", "fixtures", "evaluation", "catalog.json")
	output := filepath.Join(t.TempDir(), "result")
	configPath := writeCandidateConfig(t, output, "invalid_pdf")
	catalog, err := RunCandidate(context.Background(), CandidateRunRequest{
		CatalogPath: catalogPath, CandidatePath: configPath,
		FixtureID: "fixture_a4_ja_en_001", OutputDir: output,
	})
	if !errors.Is(err, ErrUnavailable) || catalog.Status != "incomplete" ||
		catalog.Validation == nil || catalog.Validation.Status == "passed" ||
		catalog.Validation.FailureClass != "invalid-pdf" {
		t.Fatalf("invalid PDF evidence was not recorded as incomplete: catalog=%+v err=%v", catalog, err)
	}
}

func fixtureForArtifactTest() FixtureDefinition {
	return FixtureDefinition{
		ID: "fixture_artifact_001", MediaType: "image/png", PageIDs: []string{"page_artifact_001"},
		DocumentType: "a4-text", ExpectedOutputs: []ArtifactRole{RoleOCRText, RolePDF},
	}
}

func artifactForTest(role ArtifactRole, mediaType string) CandidateArtifact {
	id := "artifact_candidate_ocr_001"
	if role == RoleGenerated {
		id = "artifact_candidate_generated_001"
	}
	return CandidateArtifact{
		ID: id, Role: role, Path: "outputs/" + string(role), MediaType: mediaType,
		SourcePages: []string{"page_artifact_001"}, Parents: []string{"artifact_original_test"},
		Algorithm: "test", AlgorithmVersion: "1",
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
	candidateID := "candidate_helper_text_001"
	if mode == "complete" {
		candidateID = "candidate_helper_complete_001"
	}
	environment := []string{"EVAL_CANDIDATE_HELPER=1", "EVAL_CANDIDATE_ID=" + candidateID}
	if mode != "" {
		environment = append(environment, "EVAL_CANDIDATE_MODE="+mode)
	}
	config := map[string]any{
		"format": "scanner-eval-candidate", "version": 1,
		"candidate_id": candidateID, "executable": executable,
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
