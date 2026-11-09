package apiruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
)

func paperlessApp(t *testing.T, enabled, configured bool, status *atomic.Int32, gets, posts *atomic.Int32) (*App, *http.Cookie, config.Paperless) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
			w.WriteHeader(500)
			return
		}
		gets.Add(1)
		if r.URL.Path != "/api/" || r.URL.RawQuery != "" || r.Header.Get("Accept") != "application/json; version=9" || r.Header.Get("Authorization") != "Token synthetic-paperless-secret" {
			t.Error("health protocol escaped fixed read-only root")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Version", "3.4.2")
		w.Header().Set("X-Api-Version", "10")
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(`{"documents":"https://do-not-follow.invalid/private/"}`))
	}))
	t.Cleanup(server.Close)
	values := map[string]string{"SCANNER_PAPERLESS_ENABLED": "true", "SCANNER_PAPERLESS_DESTINATION": "Household archive", "SCANNER_PAPERLESS_BASE_URL": server.URL, "SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP": "true", "SCANNER_PAPERLESS_TOKEN_ENV": "SCANNER_PAPERLESS_TOKEN_TEST"}
	if !enabled {
		values["SCANNER_PAPERLESS_ENABLED"] = "false"
	}
	if configured {
		values["SCANNER_PAPERLESS_TOKEN_TEST"] = "synthetic-paperless-secret"
	}
	p, err := config.LoadPaperless(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	c := fixture(t)
	c.Paperless = p
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close() })
	guard, err := workeridentity.Acquire(c.DocumentDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guard.Close() })
	catalog, err := workercatalog.New(app.docs, time.Now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := catalog.Begin(context.Background(), guard)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.Publish(context.Background(), lease, renderadmission.CatalogSnapshot{}); err != nil {
		t.Fatal(err)
	}
	anonymous := previewRequest(app, "GET", "/api/session", nil).Result().Cookies()[0]
	guest := previewRequest(app, "POST", "/auth/guest", anonymous).Result().Cookies()[0]
	return app, guest, p
}

func TestPaperlessCapabilityProjection(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfigured", true: "configured"}[configured], func(t *testing.T) {
			var gets, posts, status atomic.Int32
			status.Store(200)
			app, cookie, p := paperlessApp(t, true, configured, &status, &gets, &posts)
			w := previewRequest(app, "GET", "/api/capabilities", cookie)
			var body struct {
				Paperless *struct {
					Enabled      bool   `json:"enabled"`
					Configured   bool   `json:"configured"`
					Destination  string `json:"processing_destination"`
					Confirmation bool   `json:"confirmation_required"`
					Health       struct {
						Status    string     `json:"status"`
						CheckedAt *time.Time `json:"checked_at"`
					} `json:"health"`
				} `json:"paperless"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || body.Paperless == nil {
				t.Fatal("enabled Paperless must project distinct public capability state", w.Code, w.Body.String())
			}
			v := body.Paperless
			if !v.Enabled || v.Configured != configured || v.Destination != "Household archive" || !v.Confirmation || v.Health.Status != "unknown" || v.Health.CheckedAt != nil {
				t.Fatal("capability collapsed distinct states", w.Body.String())
			}
			for _, secret := range []string{p.BaseURL, "synthetic-paperless-secret", p.ConfigFingerprint, p.DestinationID, "do-not-follow"} {
				if secret != "" && strings.Contains(w.Body.String(), secret) {
					t.Fatal("private metadata exposed")
				}
			}
			if gets.Load() != 0 || posts.Load() != 0 {
				t.Fatal("metadata contacted remote")
			}
		})
	}
}

func TestDisabledPaperlessCapabilityMetadata(t *testing.T) {
	var gets, posts, status atomic.Int32
	status.Store(200)
	app, cookie, _ := paperlessApp(t, false, true, &status, &gets, &posts)
	for _, path := range []string{"/api/capabilities", "/api/scanners"} {
		w := previewRequest(app, "GET", path, cookie)
		if w.Code != 200 || strings.Contains(w.Body.String(), "paperless") {
			t.Fatal("disabled capability exposed", w.Code, w.Body.String())
		}
	}
	if w := previewRequest(app, "POST", "/api/capabilities/paperless/health", cookie); w.Code != 404 {
		t.Fatal("disabled health route available", w.Code)
	}
	if gets.Load() != 0 || posts.Load() != 0 {
		t.Fatal("disabled capability contacted remote")
	}
}
