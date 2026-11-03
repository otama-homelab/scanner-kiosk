package apiruntime

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
	"scanner-kiosk/pkg/ocrplugin"
	"strings"
	"testing"
	"time"
)

func TestCapabilitiesExposeOnlyCurrentSelectableMetadata(t *testing.T) {
	c := fixture(t)
	c.Scanners = []config.Scanner{{ID: "scanner_catalog_01", Name: "Office scanner", Driver: "sane-airscan", Endpoint: "http://192.0.2.10/private-endpoint"}}
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	guest := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	if w := previewRequest(app, "GET", "/api/capabilities", nil); w.Code != 401 {
		t.Fatal("anonymous capabilities", w.Code)
	}
	if w := previewRequest(app, "GET", "/api/capabilities", guest); w.Code != 503 {
		t.Fatal("missing catalog", w.Code)
	}
	guard, err := workeridentity.Acquire(c.DocumentDir)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	store, err := workercatalog.New(app.docs, time.Now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Begin(context.Background(), guard)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := renderadmission.CatalogSnapshot{Preferred: &renderadmission.CatalogSelection{ProviderID: "local", ModelID: "pdf", Languages: []string{"ja", "en"}, QualityPreset: "standard"}, Defaults: []renderadmission.ModelDefault{{ProviderID: "local", ModelID: "pdf", QualityPreset: "standard"}}, Providers: []ocrplugin.Capabilities{{APIVersion: 1, ID: "local", Name: "Local OCR", Version: "v1", Models: []ocrplugin.Model{
		{ID: "pdf", Name: "PDF model", Version: "model-v1", Languages: []string{"ja", "en"}, QualityPresets: []string{"standard"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 100, MaxPagePixels: 80000000},
		{ID: "text-only", Name: "No renderer", Version: "model-v2", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text}, MaxPages: 1, MaxPagePixels: 1000},
	}}}}
	if err = store.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	w := previewRequest(app, "GET", "/api/capabilities", guest)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, private := range []string{"private-endpoint", "192.0.2.10", guard.ID(), "text-only", ".sqlite3", "sane-airscan"} {
		if strings.Contains(body, private) {
			t.Fatal("private or unselectable metadata exposed", private)
		}
	}
	var value struct {
		Revision string `json:"revision"`
		Scanners []struct {
			State string   `json:"state"`
			Sizes []string `json:"sizes"`
			DPI   []any    `json:"dpi_options"`
		} `json:"scanners"`
		Plugins []struct {
			Models []struct {
				ID        string   `json:"id"`
				MaxPages  int      `json:"max_pages"`
				MaxPixels int64    `json:"max_page_pixels"`
				Outputs   []string `json:"outputs"`
			} `json:"models"`
		} `json:"ocr_plugins"`
		Defaults struct {
			OCR struct {
				Enabled   bool     `json:"enabled"`
				ModelID   string   `json:"model_id"`
				Languages []string `json:"languages"`
			} `json:"ocr"`
		} `json:"defaults"`
		Limits struct {
			Retention int64 `json:"retention_seconds"`
		} `json:"limits"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Revision) != 64 || len(value.Scanners) != 1 || value.Scanners[0].State != "unknown" || len(value.Scanners[0].Sizes) != 0 || len(value.Scanners[0].DPI) != 0 || value.Defaults.OCR.Enabled || value.Defaults.OCR.ModelID != "pdf" || len(value.Defaults.OCR.Languages) != 2 || value.Limits.Retention != 86400 {
		t.Fatal("invalid defaults or fabricated device capability", body)
	}
	if len(value.Plugins) != 1 || len(value.Plugins[0].Models) != 1 {
		t.Fatal("incompatible models included", body)
	}
	model := value.Plugins[0].Models[0]
	if model.ID != "pdf" || model.MaxPages != 50 || model.MaxPixels != 40000000 || len(model.Outputs) != 1 || model.Outputs[0] != "searchable_pdf" {
		t.Fatal("incorrect effective capability", body)
	}
	if err = store.Publish(context.Background(), lease, snapshot); err != nil {
		t.Fatal(err)
	}
	if next := previewRequest(app, "GET", "/api/capabilities", guest); next.Body.String() != body {
		t.Fatal("heartbeat changed content revision")
	}
	if w := previewRequest(app, "GET", "/api/capabilities?endpoint=private", guest); w.Code != 400 {
		t.Fatal("query accepted", w.Code)
	}
	if err = store.Stop(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if w := previewRequest(app, "GET", "/api/capabilities", guest); w.Code != 503 {
		t.Fatal("withdrawn catalog exposed", w.Code)
	}
	// The session guard checks the public Host before accessing catalog data.
	r := httptest.NewRequest("GET", "https://attacker.invalid/api/capabilities", nil)
	r.AddCookie(guest)
	denied := httptest.NewRecorder()
	app.Handler().ServeHTTP(denied, r)
	if denied.Code != 403 {
		t.Fatal("foreign host accepted", denied.Code)
	}
}
