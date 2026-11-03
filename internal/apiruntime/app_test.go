package apiruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/config"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	state, docs := filepath.Join(root, "state"), filepath.Join(root, "documents")
	for _, path := range []string{state, docs} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	values := map[string]string{"SCANNER_PUBLIC_URL": "https://scanner.example.test", "SCANNER_AUTH_MODE": "guest", "SCANNER_STATE_DIR": state, "SCANNER_DOCUMENT_DIR": docs}
	c, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestAPICompositionGuestAndStoreReadiness(t *testing.T) {
	c := fixture(t)
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, path := range []string{"/health/live", "/health/ready", "/api/session"} {
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test"+path, nil))
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{filepath.Join(c.StateDir, "state.sqlite3"), filepath.Join(c.DocumentDir, "documents.sqlite3")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private store missing", err)
		}
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://probe.invalid/health/ready", nil))
	if w.Code != 503 {
		t.Fatal("closed API advertised ready")
	}
}
func TestAPIFailsClosedWithoutOIDCOrPrivateRoots(t *testing.T) {
	for _, mode := range []string{"role", "root", "oidc"} {
		t.Run(mode, func(t *testing.T) {
			c := fixture(t)
			expected := ErrConfig
			switch mode {
			case "role":
				c.Role = config.Worker
			case "root":
				os.Chmod(c.StateDir, 0755)
				expected = ErrRoot
			case "oidc":
				c.AuthenticateType = "oidc"
				c.OIDC = config.OIDC{Issuer: "https://127.0.0.1:1", ClientID: "scanner", ClientAuth: "none", Scopes: []string{"openid"}}
				expected = ErrOIDC
			}
			app, err := New(context.Background(), c)
			if !errors.Is(err, expected) || app != nil {
				t.Fatal("invalid API configuration accepted/fell back", err)
			}
		})
	}
}
func TestRequestRateAndConcurrencyBounds(t *testing.T) {
	t.Run("rate", func(t *testing.T) {
		h := bounded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), 1, 1, 2, 5*time.Minute)
		for i := 0; i < 3; i++ {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
			expected := 200
			if i == 2 {
				expected = 429
			}
			if w.Code != expected {
				t.Fatal("rate admission failed", w.Code)
			}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://probe/health/live", nil))
		if w.Code != 200 {
			t.Fatal("user traffic starved health")
		}
	})
	t.Run("concurrency", func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		h := bounded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(started) })
			<-release
			w.WriteHeader(200)
		}), 1, 100, 10, 5*time.Minute)
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
		}()
		<-started
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
		if w.Code != 503 {
			t.Fatal("concurrent request not rejected")
		}
		close(release)
		<-done
	})
}

func TestBinaryTransferContextHasSeparateDeadline(t *testing.T) {
	for _, test := range []struct {
		path, method string
		want         time.Duration
	}{
		{"/api/documents/document_0000001/download", "GET", 5 * time.Minute},
		{"/api/documents/document_0000001/pages/page_000000001/preview", "HEAD", 5 * time.Minute},
		{"/api/documents", "GET", 30 * time.Second},
		{"/api/documents/document_0000001/download", "POST", 30 * time.Second},
	} {
		t.Run(test.path+test.method, func(t *testing.T) {
			h := bounded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deadline, ok := r.Context().Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > test.want || remaining < test.want-time.Second {
					t.Fatal("incorrect route duration", remaining)
				}
				w.WriteHeader(204)
			}), 1, 100, 10, 5*time.Minute)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(test.method, "https://scanner.example.test"+test.path, nil))
		})
	}
}

func TestStaticAssetsDoNotConsumeAPIRateBudget(t *testing.T) {
	h := bounded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), 1, 1, 1, 5*time.Minute)
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test/style.css", nil))
		if w.Code != 204 {
			t.Fatal("static asset throttled by API budget", w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
		expected := 204
		if i == 1 {
			expected = 429
		}
		if w.Code != expected {
			t.Fatal("API rate budget not preserved", w.Code)
		}
	}
}
