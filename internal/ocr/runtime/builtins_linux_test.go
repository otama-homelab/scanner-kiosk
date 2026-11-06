//go:build linux

package runtime

import (
	"context"
	"encoding/json"
	"golang.org/x/image/font/gofont/goregular"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestWorkerComposesLLMWithoutOIDCOrRemoteProbe(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	profiles := []config.LLMProfile{{ID: "local", Name: "Local", ProcessingDestination: "Fixture OCR service", Protocol: "chat-completions", Endpoint: server.URL + "/v1/chat/completions", AllowInsecureHTTP: true, APIKeyEnv: "SCANNER_OCR_LLM_KEY_LOCAL", Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"ja", "en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text}, MaxCompletionTokens: 8192}}
	raw, _ := json.Marshal(profiles)
	values := map[string]string{"SCANNER_OCR_PLUGINS": "llm", "SCANNER_OCR_LLM_PROFILES_JSON": string(raw), "SCANNER_OCR_LLM_KEY_LOCAL": "worker-only-secret"}
	lookup := func(key string) (string, bool) {
		if strings.HasPrefix(key, "SCANNER_OIDC_") {
			t.Fatal("worker read OIDC")
		}
		v, ok := values[key]
		return v, ok
	}
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWorker(context.Background(), core, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.Defaults().PluginID != "llm" || calls.Load() != 0 {
		t.Fatal("startup called remote endpoint or changed default")
	}
	caps, _ := json.Marshal(runtime.Registry.Capabilities())
	if strings.Contains(string(caps), "worker-only-secret") {
		t.Fatal("credential in capabilities")
	}
	delete(values, "SCANNER_OCR_LLM_KEY_LOCAL")
	if _, err = NewWorker(context.Background(), core, lookup, t.TempDir()); err == nil || strings.Contains(err.Error(), "worker-only-secret") {
		t.Fatal("missing key accepted or leaked")
	}
}

func TestWorkerComposesRemoteTesseractWithoutLocalEngineOrRecognizing(t *testing.T) {
	const key = "fixture-tesseract-key-00000000000000000000"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/v1/capabilities" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("unexpected recognition or credential")
		}
		json.NewEncoder(w).Encode(ocrplugin.Capabilities{APIVersion: 1, ID: "tesseract", Name: "Tesseract", Version: "1", Models: []ocrplugin.Model{{ID: "installed", Name: "Installed", Version: "sha256-" + strings.Repeat("a", 64), Languages: []string{"ja", "en"}, QualityPresets: []string{"auto"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 10000, MaxPagePixels: 40000000}}})
	}))
	defer server.Close()
	values := map[string]string{"SCANNER_OCR_TESSERACT_ENDPOINT": server.URL, "SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP": "true", "SCANNER_OCR_TESSERACT_API_KEY": key, "SCANNER_OCR_TESSERACT_PATH": "/not-installed/tesseract"}
	lookup := func(k string) (string, bool) {
		if strings.HasPrefix(k, "SCANNER_OIDC_") {
			t.Fatal("OCR worker read OIDC")
		}
		v, ok := values[k]
		return v, ok
	}
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWorker(context.Background(), core, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	caps, _ := json.Marshal(runtime.Registry.Capabilities())
	if calls.Load() != 1 || runtime.Defaults().PluginID != "tesseract" || strings.Contains(string(caps), key) || strings.Contains(string(caps), server.URL) {
		t.Fatal("incorrect optional remote composition")
	}
	values["SCANNER_OCR_PLUGINS"] = ""
	disabled, err := NewWorker(context.Background(), core, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if calls.Load() != 1 || len(disabled.Registry.Capabilities()) != 0 {
		t.Fatal("disabled plugin contacted endpoint")
	}
}
func TestWorkerUsesInstalledDictionaryDefaults(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "tesseract")
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("not executed during startup"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"eng", "jpn", "osd"} {
		if err := os.WriteFile(filepath.Join(data, language+".traineddata"), []byte(language), 0600); err != nil {
			t.Fatal(err)
		}
	}
	values := map[string]string{"SCANNER_OCR_TESSERACT_PATH": binary, "SCANNER_OCR_TESSERACT_DATA_DIR": data}
	lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewWorker(context.Background(), core, lookup, root)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Defaults().PluginID != "tesseract" || runtime.Defaults().ModelID != "installed" {
		t.Fatal("missing installed default")
	}
	runtime.Close()
	if err = os.Remove(filepath.Join(data, "jpn.traineddata")); err != nil {
		t.Fatal(err)
	}
	if _, err = NewWorker(context.Background(), core, lookup, root); err == nil {
		t.Fatal("unsupported default language accepted")
	}
	values["SCANNER_OCR_DEFAULT_LANGUAGES"] = "en"
	runtime, err = NewWorker(context.Background(), core, lookup, root)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	core.Role = config.API
	if _, err = NewWorker(context.Background(), core, lookup, root); err == nil {
		t.Fatal("API configuration accepted at worker boundary")
	}
}

func TestWorkerLoadsOptionalLLMTextLayerFont(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	profile := config.LLMProfile{ID: "local", Name: "Local", ProcessingDestination: "Fixture OCR service", Protocol: "chat-completions", Endpoint: server.URL, AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PositionedText}, MaxCompletionTokens: 1000}
	raw, _ := json.Marshal([]config.LLMProfile{profile})
	root := t.TempDir()
	path := filepath.Join(root, "font.ttf")
	if err := os.WriteFile(path, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"SCANNER_OCR_PLUGINS": "llm", "SCANNER_OCR_DEFAULT_LANGUAGES": "en", "SCANNER_OCR_LLM_PROFILES_JSON": string(raw), "SCANNER_OCR_LLM_TEXT_LAYER_FONT": path}
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	core, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(context.Background(), core, lookup, root)
	if err != nil {
		t.Fatal(err)
	}
	caps := worker.Registry.Capabilities()
	version := caps[0].Models[0].Version
	hasPDF := false
	for _, kind := range caps[0].Models[0].Outputs {
		if kind == ocrplugin.PDFTextLayer {
			hasPDF = true
		}
	}
	worker.Close()
	if !hasPDF || calls.Load() != 0 {
		t.Fatal("configured font missing capability or startup contacted endpoint")
	}
	values["SCANNER_OCR_LLM_TEXT_LAYER_FONT"] = ""
	worker, err = NewWorker(context.Background(), core, lookup, root)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if worker.Registry.Capabilities()[0].Models[0].Version == version {
		t.Fatal("removing renderer did not change version")
	}
	fifo := filepath.Join(root, "fifo")
	if err = syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{filepath.Join(root, "missing"), root, fifo} {
		values["SCANNER_OCR_LLM_TEXT_LAYER_FONT"] = invalid
		if runtime, err := NewWorker(context.Background(), core, lookup, root); err == nil {
			runtime.Close()
			t.Fatal("invalid font accepted")
		} else if strings.Contains(err.Error(), root) {
			t.Fatal("font path leaked")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("font validation contacted endpoint")
	}
}
