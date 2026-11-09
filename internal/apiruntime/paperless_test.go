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
		_, _ = w.Write([]byte(`{"documents":"https://do-not-follow.invalid/private/","tasks":"https://do-not-follow.invalid/tasks/"}`))
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

func TestPaperlessHealthRouteAuthority(t *testing.T) {
	var gets, posts, status atomic.Int32
	status.Store(200)
	app, cookie, _ := paperlessApp(t, true, true, &status, &gets, &posts)
	call := func(method, path, body, origin string, auth bool, mutate func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://scanner.example.test"+path, strings.NewReader(body))
		if auth {
			r.AddCookie(cookie)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	const path = "/api/capabilities/paperless/health"
	for _, test := range []struct {
		name, method, path, body, origin string
		auth                             bool
		want                             int
		mutate                           func(*http.Request)
	}{
		{"anonymous", "POST", path, "{}", "https://scanner.example.test", false, 401, nil},
		{"query", "POST", path + "?destination=evil", "{}", "https://scanner.example.test", true, 400, nil},
		{"empty-query", "POST", path + "?", "{}", "https://scanner.example.test", true, 400, nil},
		{"method", "GET", path, "{}", "https://scanner.example.test", true, 405, nil},
		{"origin", "POST", path, "{}", "https://evil.invalid", true, 403, nil},
		{"missing-origin", "POST", path, "{}", "", true, 403, nil},
		{"cross-site", "POST", path, "{}", "https://scanner.example.test", true, 403, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{"host", "POST", path, "{}", "https://scanner.example.test", true, 403, func(r *http.Request) { r.Host = "evil.invalid" }},
		{"duplicate-origin", "POST", path, "{}", "https://scanner.example.test", true, 403, func(r *http.Request) { r.Header.Add("Origin", "https://scanner.example.test") }},
		{"payload", "POST", path, `{"url":"https://evil.invalid"}`, "https://scanner.example.test", true, 400, nil},
		{"malformed", "POST", path, "{", "https://scanner.example.test", true, 400, nil},
		{"null", "POST", path, "null", "https://scanner.example.test", true, 400, nil},
		{"trailing", "POST", path, "{}{}", "https://scanner.example.test", true, 400, nil},
		{"oversized", "POST", path, "{" + strings.Repeat(" ", 300) + "}", "https://scanner.example.test", true, 400, nil},
		{"media", "POST", path, "{}", "https://scanner.example.test", true, 400, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := call(test.method, test.path, test.body, test.origin, test.auth, test.mutate)
			if w.Code != test.want || gets.Load() != 0 || posts.Load() != 0 {
				t.Fatal("rejected command reached remote", w.Code, w.Body.String(), gets.Load(), posts.Load())
			}
		})
	}
	for i, code := range []int32{200, 401, 403, 503} {
		status.Store(code)
		w := call("POST", path, "{}", "https://scanner.example.test", true, nil)
		want := []string{"healthy", "unauthorized", "forbidden", "unavailable"}[i]
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"status":"`+want+`"`) || !strings.Contains(w.Body.String(), `"checked_at"`) {
			t.Fatal("health projection missing", w.Code, w.Body.String())
		}
		metadata := previewRequest(app, "GET", "/api/capabilities", cookie)
		if !strings.Contains(metadata.Body.String(), `"status":"`+want+`"`) || !strings.Contains(metadata.Body.String(), `"confirmation_required":true`) || gets.Load() != int32(i+1) || posts.Load() != 0 {
			t.Fatal("health state or authority leaked", metadata.Body.String(), gets.Load(), posts.Load())
		}
	}
	t.Logf("health root GET=%d upload POST=%d; metadata remote GET=0", gets.Load(), posts.Load())
	var g, p, s atomic.Int32
	s.Store(200)
	unconfigured, guest, _ := paperlessApp(t, true, false, &s, &g, &p)
	r := httptest.NewRequest("POST", "https://scanner.example.test"+path, strings.NewReader("{}"))
	r.AddCookie(guest)
	r.Header.Set("Origin", "https://scanner.example.test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	unconfigured.Handler().ServeHTTP(w, r)
	if w.Code != 503 || g.Load() != 0 || p.Load() != 0 {
		t.Fatal("unconfigured health constructed remote authority", w.Code)
	}
}
