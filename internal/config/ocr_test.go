package config

import (
	"errors"
	"strings"
	"testing"
)

func TestOCRDefaults(t *testing.T) {
	c, err := LoadOCR(env(nil), map[string]PluginLoader{"tesseract": LoadTesseract})
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultPlugin != "tesseract" || strings.Join(c.DefaultLanguages, ",") != "ja,en" || len(c.Plugins) != 1 {
		t.Fatal("incorrect defaults")
	}
	settings, ok := c.Plugins[0].Settings.(Tesseract)
	if !ok || settings.Path != "/usr/bin/tesseract" {
		t.Fatal("missing typed settings")
	}
}
func TestOCRExtensionAndDisabledIsolation(t *testing.T) {
	calls := 0
	custom := func(lookup Lookup) (any, error) { calls++; v, _ := lookup("CUSTOM_PLUGIN_OPTION"); return v, nil }
	loaders := map[string]PluginLoader{"tesseract": func(Lookup) (any, error) { t.Fatal("disabled loader called"); return nil, nil }, "future-plugin": custom}
	values := map[string]string{"SCANNER_OCR_PLUGINS": "future-plugin", "CUSTOM_PLUGIN_OPTION": "configured"}
	c, err := LoadOCR(env(values), loaders)
	if err != nil || calls != 1 || c.DefaultPlugin != "future-plugin" || c.Plugins[0].Settings != "configured" {
		t.Fatalf("extension failed: %v", err)
	}
	values["SCANNER_OCR_PLUGINS"] = ""
	c, err = LoadOCR(env(values), loaders)
	if err != nil || len(c.Plugins) != 0 || c.DefaultPlugin != "" || calls != 1 {
		t.Fatal("disabled OCR invoked a plugin")
	}
}
func TestOCRPreservesOrder(t *testing.T) {
	loader := func(Lookup) (any, error) { return struct{}{}, nil }
	c, err := LoadOCR(env(map[string]string{"SCANNER_OCR_PLUGINS": "second,first", "SCANNER_OCR_DEFAULT_PLUGIN": "first"}), map[string]PluginLoader{"first": loader, "second": loader})
	if err != nil || c.Plugins[0].ID != "second" || c.Plugins[1].ID != "first" || c.DefaultPlugin != "first" {
		t.Fatal("configuration order/default changed")
	}
}
func TestOCRInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"SCANNER_OCR_PLUGINS", "unknown"}, {"SCANNER_OCR_PLUGINS", "tesseract,tesseract"}, {"SCANNER_OCR_PLUGINS", " tesseract"},
		{"SCANNER_OCR_DEFAULT_PLUGIN", "disabled"}, {"SCANNER_OCR_DEFAULT_LANGUAGES", ""}, {"SCANNER_OCR_DEFAULT_LANGUAGES", "ja,ja"}, {"SCANNER_OCR_DEFAULT_LANGUAGES", "ja, en"},
		{"SCANNER_OCR_TESSERACT_PATH", "tesseract"}, {"SCANNER_OCR_TESSERACT_DATA_DIR", "/tmp/../data"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			_, err := LoadOCR(env(map[string]string{tc.key: tc.value}), map[string]PluginLoader{"tesseract": LoadTesseract})
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
func TestOCRLoaderErrorRedaction(t *testing.T) {
	_, err := LoadOCR(env(nil), map[string]PluginLoader{"tesseract": func(Lookup) (any, error) { return nil, errors.New("https://secret-token@endpoint/private") }})
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("loader error leaked")
	}
}

func TestTesseractRemoteConfigurationIsExplicitAndNonSecret(t *testing.T) {
	values := map[string]string{"SCANNER_OCR_TESSERACT_ENDPOINT": "https://ocr.example", "SCANNER_OCR_TESSERACT_API_KEY": "must-not-be-read"}
	lookup := func(key string) (string, bool) {
		if key == "SCANNER_OCR_TESSERACT_API_KEY" {
			t.Fatal("static loader read a credential")
		}
		v, ok := values[key]
		return v, ok
	}
	v, err := LoadTesseract(lookup)
	if err != nil {
		t.Fatal(err)
	}
	c := v.(Tesseract)
	if c.Endpoint != "https://ocr.example" || c.ProcessingDestination == "" || c.AllowInsecureHTTP {
		t.Fatal("incorrect remote metadata")
	}
	for _, endpoint := range []string{"http://ocr.example", "https://user:secret@ocr.example", "https://ocr.example?key=secret", "https://ocr.example/#fragment", "https://ocr.example/private", "file:///tmp/socket"} {
		values["SCANNER_OCR_TESSERACT_ENDPOINT"] = endpoint
		if _, err := LoadTesseract(lookup); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	values["SCANNER_OCR_TESSERACT_ENDPOINT"] = "http://ocr.example:8080"
	values["SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP"] = "true"
	if _, err := LoadTesseract(lookup); err != nil {
		t.Fatal("explicit private HTTP rejected")
	}
}
