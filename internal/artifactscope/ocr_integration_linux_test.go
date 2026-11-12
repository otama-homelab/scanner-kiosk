//go:build linux

package artifactscope_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/ocr/llm"
	"scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/pkg/ocrplugin"
)

func imageScope(t *testing.T, data []byte) (*artifactscope.Scope, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source"), data, 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.New(root, map[string]artifactscope.Input{"input_0000000001": {RelativePath: "source", Size: int64(len(data)), SHA256: sha256.Sum256(data)}}, artifactscope.Limits{MaxInputBytes: 20 << 20, MaxOutputBytes: 20 << 20, MaxTotalOutputBytes: 20 << 20, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scope.Close() })
	return scope, root
}
func TestLLMPortWritesOnlyScopedUnpublishedArtifacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(map[string]any{"text": "日本語 English"})
		json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(content)}}}})
	}))
	defer server.Close()
	profile := config.LLMProfile{ID: "fixture", Name: "Fixture", Protocol: "chat-completions", Endpoint: server.URL, AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"ja", "en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text}, MaxCompletionTokens: 1000}
	provider, err := llm.New(llm.Options{Settings: config.LLM{Profiles: []config.LLMProfile{profile}}, MaxInputBytes: 1 << 20, MaxResponseBytes: 1 << 20, MaxPagePixels: 10000, MaxPages: 1, MaxSpans: 100, MaxTextBytes: 1000, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	var buffer bytes.Buffer
	png.Encode(&buffer, image.NewGray(image.Rect(0, 0, 80, 100)))
	scope, root := imageScope(t, buffer.Bytes())
	caps, _ := provider.Capabilities(context.Background())
	req := ocrplugin.Request{JobID: "job_000000000001", InputRevision: 1, ModelID: "fixture", ModelVersion: caps.Models[0].Version, Languages: []string{"ja", "en"}, QualityPreset: "standard", Output: ocrplugin.Text, Pages: []ocrplugin.InputPage{{ID: "page_00000000001", ArtifactID: "input_0000000001", Width: 80, Height: 100, DPI: 300}}}
	result, err := provider.Run(context.Background(), req, scope)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := scope.Seal(context.Background())
	if err != nil || len(artifacts) != 1 || result.Pages[0].ArtifactID != artifacts[0].ID {
		t.Fatalf("scoped result not sealed: %v", err)
	}

	summaries := make([]ocrplugin.ResultArtifact, len(artifacts))
	for i, artifact := range artifacts {
		summaries[i] = ocrplugin.ResultArtifact{ID: artifact.ID, Kind: ocrplugin.OutputKind(artifact.Kind), Size: artifact.Size}
	}
	bound, err := ocrplugin.BindResult(req, result, summaries, 20<<20)
	if err != nil || len(bound) != 1 || bound[0].ArtifactID != artifacts[0].ID {
		t.Fatalf("LLM result binding: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, artifacts[0].RelativePath))
	if err != nil || string(data) != "日本語 English" {
		t.Fatal("invalid scoped transcription")
	}
	// Simulate a failed publication CAS: abort rather than release.
	scope.Close()
	if _, err = os.Stat(filepath.Join(root, artifacts[0].RelativePath)); !os.IsNotExist(err) {
		t.Fatal("unpublished artifact retained")
	}
	if _, err = os.Stat(filepath.Join(root, "source")); err != nil {
		t.Fatal("original removed")
	}
}

func TestInstalledTesseractScoped(t *testing.T) {
	binary := os.Getenv("SCANNER_TEST_TESSERACT_BINARY")
	if binary == "" {
		t.Skip("requires extracted Tesseract and synthetic fixture")
	}
	source, err := os.ReadFile(os.Getenv("SCANNER_TEST_OCR_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	geometry, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"SCANNER_OCR_TESSERACT_PATH": binary, "SCANNER_OCR_TESSERACT_DATA_DIR": os.Getenv("SCANNER_TEST_TESSDATA")}
	lookup := func(key string) (string, bool) { value, ok := values[key]; return value, ok }
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	native := t.TempDir()
	worker, err := runtime.NewWorker(context.Background(), core, lookup, native)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	defaults := worker.Defaults()
	for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PDFTextLayer} {
		t.Run(string(kind), func(t *testing.T) {
			scope, root := imageScope(t, source)
			req := ocrplugin.Request{JobID: "job_000000000001", InputRevision: 1, ModelID: defaults.ModelID, ModelVersion: defaults.ModelVersion, Languages: defaults.Languages, QualityPreset: defaults.QualityPreset, Output: kind, Pages: []ocrplugin.InputPage{{ID: "page_00000000001", ArtifactID: "input_0000000001", Width: geometry.Width, Height: geometry.Height, DPI: 300}}}
			provider, err := worker.Registry.Select(defaults.PluginID, req)
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.Run(context.Background(), req, scope)
			if err != nil {
				t.Fatal(err)
			}
			artifacts, err := scope.Seal(context.Background())
			if err != nil || len(artifacts) != 1 || len(result.Pages) != 1 || result.Pages[0].ArtifactID != artifacts[0].ID {
				t.Fatalf("sealed OCR mismatch: %v", err)
			}
			out := filepath.Join(root, artifacts[0].RelativePath)
			text, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if kind == ocrplugin.PDFTextLayer {
				text, err = exec.Command("pdftotext", "-layout", out, "-").Output()
				if err != nil {
					t.Fatal(err)
				}
			}
			compact := strings.Join(strings.Fields(string(text)), "")
			for _, expected := range []string{"日本語の書類", "ScannerOCR2026", "300dpi"} {
				if !strings.Contains(compact, expected) {
					t.Fatal("scoped synthetic OCR missing expected text")
				}
			}
			if err = scope.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("OCR staging retained after abort")
			}
		})
	}
}
