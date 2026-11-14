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

func TestPDFPreviewAssetsKeepStaticBoundary(t *testing.T) {
	for _, name := range []string{"_astro/pdf.worker.mjs", "pdfjs/cmaps/Japan.bcmap", "pdfjs/standard_fonts/Foxit.pfb", "pdfjs/standard_fonts/font.ttf", "pdfjs/wasm/openjpeg.wasm", "pdfjs/NOTICE.txt", "ui/NOTICE.txt"} {
		handler, err := fromFS("https://scanner.example.test", fstest.MapFS{"index.html": {Data: []byte("ok")}, name: {Data: []byte("asset")}})
		if err != nil {
			t.Fatal(name, err)
		}
		request := httptest.NewRequest("GET", "https://scanner.example.test/"+name, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 || response.Header().Get("Content-Type") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(name, response.Code, response.Header())
		}
	}
	for _, name := range []string{"private.ttf", "pdfjs/secret.env", "pdfjs/arbitrary.txt", "ui/arbitrary.txt"} {
		if _, err := fromFS("https://scanner.example.test", fstest.MapFS{"index.html": {Data: []byte("ok")}, name: {Data: []byte("private")}}); err == nil {
			t.Fatal("unreviewed asset type accepted", name)
		}
	}
}
