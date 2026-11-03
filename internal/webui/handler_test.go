package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticAssetBoundary(t *testing.T) {
	handler, err := fromFS("https://scanner.example.test", fstest.MapFS{"index.html": {Data: []byte("<h1>Scanner</h1>")}, "_astro/app.js": {Data: []byte("export {}")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, host, path string
		status             int
	}{{"GET", "scanner.example.test", "/", 200}, {"HEAD", "scanner.example.test", "/_astro/app.js", 200}, {"GET", "scanner.example.test", "/_astro/", 404}, {"GET", "scanner.example.test", "/../private", 404}, {"GET", "other.example.test", "/", 403}, {"POST", "scanner.example.test", "/", 405}} {
		request := httptest.NewRequest(test.method, "https://"+test.host+test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatal(test, response.Code)
		}
		if test.method == "HEAD" && response.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing static protections")
		}
	}
	if _, err = fromFS("https://scanner.example.test", fstest.MapFS{"index.html": {Data: []byte("ok")}, "secret.env": {Data: []byte("secret")}}); err == nil {
		t.Fatal("unreviewed public asset accepted")
	}
}
