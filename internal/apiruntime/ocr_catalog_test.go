package apiruntime

import (
	"context"
	"encoding/json"
	"golang.org/x/image/font/gofont/goregular"
	"net/http/httptest"
	"path/filepath"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/ocr/llm"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"scanner-kiosk/pkg/ocrplugin"
	"strings"
	"testing"
	"time"
)

func TestPublishedOCRCatalogControlsHTTPAdmissionAndReplay(t *testing.T) {
	app, guest, _, _, original := previewFixture(t, time.Hour)
	guard, err := workeridentity.Acquire(filepath.Dir(original))
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	catalog, err := workercatalog.New(app.docs, time.Now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := catalog.Begin(context.Background(), guard)
	if err != nil {
		t.Fatal(err)
	}
	options := llm.Options{Settings: config.LLM{Profiles: []config.LLMProfile{{ID: "model", Name: "Model", ProcessingDestination: "Fixture OCR service", Protocol: "chat-completions", Endpoint: "https://llm.example.test/v1/chat/completions", Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"jpn", "eng"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PositionedText}, MaxCompletionTokens: 1000}}}, TextLayerFont: goregular.TTF, MaxTextLayerBytes: 1 << 20, MaxInputBytes: 1 << 20, MaxResponseBytes: 1 << 20, MaxPagePixels: 40000000, MaxPages: 50, MaxSpans: 100, MaxTextBytes: 10000, Timeout: time.Second}
	provider, err := llm.New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	capabilities, err := provider.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pinnedVersion := capabilities.Models[0].Version
	snapshot := renderadmission.CatalogSnapshot{Providers: []ocrplugin.Capabilities{capabilities}, Defaults: []renderadmission.ModelDefault{{ProviderID: "llm", ModelID: "model", QualityPreset: "standard"}}}

	call := func(languages, key string) *httptest.ResponseRecorder {
		body := `{"format":"pdf","ocr":{"enabled":true,"provider_id":"llm","model_id":"model","languages":` + languages + `}}`
		r := httptest.NewRequest("POST", "https://scanner.example.test/api/documents/document_preview_01/render", strings.NewReader(body))
		r.AddCookie(guest)
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"1"`)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call(`["jpn","eng"]`, "request_ocr_00001"); w.Code != 503 {
		t.Fatal("unpublished catalog accepted", w.Code)
	}
	if err = catalog.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	capabilityResponse := previewRequest(app, "GET", "/api/capabilities", guest)
	var public struct {
		Plugins []struct {
			ID     string `json:"id"`
			Models []struct {
				ID                    string `json:"id"`
				Version               string `json:"version"`
				ProcessingDestination string `json:"processing_destination"`
			} `json:"models"`
		} `json:"ocr_plugins"`
	}
	if err = json.Unmarshal(capabilityResponse.Body.Bytes(), &public); err != nil || capabilityResponse.Code != 200 || len(public.Plugins) != 1 || len(public.Plugins[0].Models) != 1 || public.Plugins[0].Models[0].Version != pinnedVersion {
		t.Fatal("font-bound model missing from HTTP capabilities", capabilityResponse.Code, capabilityResponse.Body.String())
	}
	if !strings.Contains(capabilityResponse.Body.String(), "Fixture OCR service") {
		t.Fatal("public processing destination missing")
	}
	if strings.Contains(capabilityResponse.Body.String(), "llm.example.test") {
		t.Fatal("private LLM endpoint exposed")
	}
	if w := call(`["fra"]`, "request_ocr_00002"); w.Code != 422 {
		t.Fatal("unsupported languages not rejected", w.Code, w.Body.String())
	}
	snapshot.Providers[0].Models[0].MaxPagePixels = 511
	if err = catalog.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	if w := call(`["jpn","eng"]`, "request_ocr_00001"); w.Code != 422 {
		t.Fatal("model pixel limit bypassed", w.Code, w.Body.String())
	}
	var revision int64
	if err := app.docs.QueryRow("SELECT revision FROM documents WHERE id='document_preview_01'").Scan(&revision); err != nil || revision != 1 {
		t.Fatal("rejection mutated document", err)
	}
	snapshot.Providers[0].Models[0].MaxPagePixels = 512
	if err = catalog.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	w := call(`["jpn","eng"]`, "request_ocr_00001")
	var job documents.JobProjection
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil || w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var configuration string
	if err := app.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", job.ID).Scan(&configuration); err != nil {
		t.Fatal(err)
	}
	var frozen documents.RenderSnapshot
	if err := json.Unmarshal([]byte(configuration), &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen.Settings.OCR.ModelVersion != pinnedVersion || frozen.Settings.OCR.QualityPreset != "standard" {
		t.Fatal("model/default not pinned", frozen.Settings.OCR)
	}
	// Removing the renderer changes live capability, but cannot rewrite a
	// receipt or the font-bound model identity already frozen in a queued job.
	options.TextLayerFont = nil
	options.MaxTextLayerBytes = 0
	withoutFont, err := llm.New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer withoutFont.Close()
	noPDF, err := withoutFont.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if noPDF.Models[0].Version == pinnedVersion {
		t.Fatal("font removal did not change model identity")
	}
	snapshot.Providers = []ocrplugin.Capabilities{noPDF}
	if err = catalog.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	if w := call(`["jpn","eng"]`, "request_ocr_00001"); w.Code != 202 {
		t.Fatal("replay depended on renderer availability", w.Code, w.Body.String())
	}
	capabilityResponse = previewRequest(app, "GET", "/api/capabilities", guest)
	if err = json.Unmarshal(capabilityResponse.Body.Bytes(), &public); err != nil || capabilityResponse.Code != 200 || len(public.Plugins) != 0 {
		t.Fatal("fontless model advertised searchable PDF", capabilityResponse.Code, capabilityResponse.Body.String())
	}
	if w := call(`["jpn","eng"]`, "request_no_font_001"); w.Code != 422 {
		t.Fatal("fontless LLM accepted new PDF work", w.Code, w.Body.String())
	}
	if err := catalog.Stop(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	w = call(`["jpn","eng"]`, "request_ocr_00001")
	var replay documents.JobProjection
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || w.Code != 202 || replay.ID != job.ID {
		t.Fatal("replay depended on live metadata", w.Code, w.Body.String())
	}
	if w := call(`["jpn","eng"]`, "request_ocr_00003"); w.Code != 503 {
		t.Fatal("stopped catalog admitted new work", w.Code, w.Body.String())
	}
}
