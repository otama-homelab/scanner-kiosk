package capabilityhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/paperless"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
	"scanner-kiosk/pkg/ocrplugin"
)

type metadataProvider struct{ discoveries int }

func (p *metadataProvider) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	p.discoveries++
	return ocrplugin.Capabilities{APIVersion: ocrplugin.APIVersion, ID: "compiled", Name: "Compiled OCR", Version: "plugin-1", Models: []ocrplugin.Model{{ID: "model", Name: "Model", Version: "exact-1", Languages: []string{"ja"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PDFTextLayer}, MaxPages: 50, MaxPagePixels: 40000000}}}, nil
}

func (*metadataProvider) Run(context.Context, ocrplugin.Request, ocrplugin.Access) (ocrplugin.Result, error) {
	panic("metadata must not process documents")
}

type metadataSource struct {
	snapshot renderadmission.CatalogSnapshot
	err      error
	reads    int
}

func (s *metadataSource) ReadOCRCatalog(context.Context) (renderadmission.CatalogSnapshot, error) {
	s.reads++
	return s.snapshot, s.err
}

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) {
	b.t.Fatal("metadata read a document payload")
	return 0, errors.New("unexpected body read")
}
func (unreadBody) Close() error { return nil }

func metadataHTTP(t *testing.T, source *metadataSource, delivery ...Paperless) (http.Handler, *http.Cookie) {
	t.Helper()
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	sessions, err := websession.New(state, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "guest", Lifetime: time.Hour, IdleTimeout: time.Minute, ReceiptLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := metadataCall(sessions, "GET", "/api/session", nil)
	cookies := bootstrap.Result().Cookies()
	if bootstrap.Code != 200 || len(cookies) != 1 {
		t.Fatal("session bootstrap failed", bootstrap.Code)
	}
	login := metadataCall(sessions, "POST", "/auth/guest", cookies[0])
	cookies = login.Result().Cookies()
	if login.Code != 200 || len(cookies) != 1 {
		t.Fatal("guest login failed", login.Code, login.Body.String())
	}
	return New(source, nil, nil, sessions, nil, Limits{JobMaxPages: 10, MaxPagePixels: 1000000}, func() time.Time { return time.UnixMilli(2000) }, delivery...), cookies[0]
}

func TestPaperlessCapabilityProjection(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	source := &metadataSource{snapshot: renderadmission.CatalogSnapshot{PublishedAtMS: 1000, ExpiresAtMS: 10000}}
	h, cookie := metadataHTTP(t, source, Paperless{Enabled: true, Configured: true, ProcessingDestination: "Archive", Health: func(ctx context.Context) paperless.HealthResult {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return paperless.HealthResult{Status: paperless.HealthHealthy, CheckedAt: time.Now().UTC()}
	}})
	probe := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://scanner.example.test/api/capabilities/paperless/health", strings.NewReader("{}"))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- probe() }()
	<-started
	metadata := make(chan *httptest.ResponseRecorder, 1)
	go func() { metadata <- metadataCall(h, "GET", "/api/capabilities", cookie) }()
	select {
	case w := <-metadata:
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"unknown"`) {
			t.Error("probe changed public state prematurely", w.Code)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("slow health blocked ordinary metadata")
	}
	if w := probe(); w.Code != 409 {
		t.Error("overlapping health was not bounded", w.Code)
	}
	close(release)
	if w := <-done; w.Code != 200 {
		t.Fatal("health did not finish", w.Code)
	}
	if source.reads != 1 {
		t.Fatal("health consulted processing catalog", source.reads)
	}
}

func metadataCall(handler http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://scanner.example.test"+path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if method != "GET" {
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Idempotency-Key", "request_capability_login_001")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestCapabilityMetadataDoesNotProcess(t *testing.T) {
	p := &metadataProvider{}
	registry, err := ocrplugin.New(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	source := &metadataSource{snapshot: renderadmission.CatalogSnapshot{PublishedAtMS: 1000, ExpiresAtMS: 10000, Providers: registry.Capabilities()}}
	handler, cookie := metadataHTTP(t, source)
	var previous string
	for range 3 {
		r := httptest.NewRequest("GET", "https://scanner.example.test/api/capabilities", nil)
		r.AddCookie(cookie)
		r.Body = unreadBody{t}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
			t.Fatal("metadata response invalid", w.Code, w.Header())
		}
		var body response
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.OCRPlugins) != 1 || len(body.OCRPlugins[0].Models) != 1 || body.OCRPlugins[0].Models[0].Version != "exact-1" || body.OCRPlugins[0].Models[0].MaxPages != 10 || body.OCRPlugins[0].Models[0].MaxPagePixels != 1000000 || body.Defaults.OCR.Enabled {
			t.Fatal("metadata changed selection, enabled processing or exceeded policy", w.Body.String())
		}
		if previous != "" && previous != w.Body.String() {
			t.Fatal("unchanged metadata revision unstable")
		}
		previous = w.Body.String()
		for _, forbidden := range []string{"credential", "token", "endpoint", "relative_path", "artifact_id", "document_id", "transcript"} {
			if strings.Contains(previous, forbidden) {
				t.Fatal("non-metadata field exposed", forbidden)
			}
		}
	}
	if p.discoveries != 1 || source.reads != 3 {
		t.Fatal("metadata rediscovered provider or retried", p.discoveries, source.reads)
	}
}

func TestCapabilityEndpointAuthority(t *testing.T) {
	source := &metadataSource{}
	handler, cookie := metadataHTTP(t, source)
	if w := metadataCall(handler, "GET", "/api/capabilities", nil); w.Code != 401 || source.reads != 0 {
		t.Fatal("anonymous discovery reached catalog", w.Code, source.reads)
	}
	if w := metadataCall(handler, "POST", "/api/capabilities", cookie); w.Code != 405 || source.reads != 0 {
		t.Fatal("processing method accepted", w.Code, source.reads)
	}
	if w := metadataCall(handler, "GET", "/api/capabilities?document=private", cookie); w.Code != 400 || source.reads != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("query reached catalog", w.Code, source.reads)
	}
	var previous string
	for _, mode := range []string{"source error", "stale", "invalid metadata"} {
		source.err = nil
		source.snapshot = renderadmission.CatalogSnapshot{PublishedAtMS: 1000, ExpiresAtMS: 10000}
		switch mode {
		case "source error":
			source.err = errors.New("token=private-canary /private/document https://private.invalid")
		case "stale":
			source.snapshot.ExpiresAtMS = 2000
		case "invalid metadata":
			source.snapshot.Providers = []ocrplugin.Capabilities{{ID: "private-canary"}}
		}
		w := metadataCall(handler, "GET", "/api/capabilities", cookie)
		if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "{\"code\":\"provider_unavailable\",\"message\":\"Service Unavailable\"}\n" {
			t.Fatal("unstable or sensitive unavailable response", mode, w.Code, w.Body.String())
		}
		if previous != "" && previous != w.Body.String() {
			t.Fatal("failure details escaped")
		}
		previous = w.Body.String()
	}
	if source.reads != 3 {
		t.Fatal("unavailable catalog retried", source.reads)
	}
}

func TestDisabledCapabilityMetadata(t *testing.T) {
	p := &metadataProvider{}
	caps, err := p.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, providers := range [][]ocrplugin.Capabilities{nil, {caps}} {
		if len(providers) > 0 {
			providers[0].Models[0].Outputs = []ocrplugin.OutputKind{ocrplugin.Text}
		}
		source := &metadataSource{snapshot: renderadmission.CatalogSnapshot{PublishedAtMS: 1000, ExpiresAtMS: 10000, Providers: providers}}
		handler, cookie := metadataHTTP(t, source)
		w := metadataCall(handler, "GET", "/api/capabilities", cookie)
		var body response
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(body.OCRPlugins) != 0 || body.Defaults.OCR.Enabled || body.Defaults.OCR.ProviderID != "" {
			t.Fatal("disabled/incompatible provider exposed processing UI", w.Code, w.Body.String())
		}
		source.err = errors.New("optional OCR offline")
		if w := metadataCall(handler, "GET", "/api/capabilities", cookie); w.Code != 503 {
			t.Fatal("outage hidden", w.Code)
		}
		reads := source.reads
		if w := metadataCall(handler, "GET", "/api/scanners", cookie); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || source.reads != reads || strings.Contains(w.Body.String(), "ocr_plugins") {
			t.Fatal("baseline scanner metadata depends on OCR", w.Code, w.Body.String())
		}
	}
}
