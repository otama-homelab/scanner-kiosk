//go:build webui

package webui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuiltAstroScriptsAreEmbedded(t *testing.T) {
	assets, err := embeddedFS()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New("https://scanner.example.test")
	if err != nil {
		t.Fatal(err)
	}
	scripts := 0
	err = fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(name, "_astro/") && strings.HasSuffix(name, ".js") {
			scripts++
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "https://scanner.example.test/"+name, nil))
			if response.Code != 200 || response.Body.Len() == 0 {
				t.Fatal("built script not served", name)
			}
		}
		return nil
	})
	if err != nil || scripts == 0 {
		t.Fatal("Astro generated scripts missing", err)
	}
}
