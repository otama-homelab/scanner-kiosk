package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PluginLoader validates static provider settings without network I/O or spawning
// tools. Runtime probing and model/default capability validation are separate.
// Adding a compiled plugin registers its loader at the composition root.
type PluginLoader func(Lookup) (any, error)
type PluginSettings struct {
	ID       string
	Settings any
}
type OCR struct {
	Plugins          []PluginSettings
	DefaultPlugin    string
	DefaultLanguages []string
}
type Tesseract struct{ Path, DataDir string }

// LoadOCR retains explicit configuration order and never silently substitutes a
// provider. Only loaders for enabled plugins are called. Underlying loader errors
// are suppressed because they may contain endpoint credentials or raw settings.
func LoadOCR(lookup Lookup, loaders map[string]PluginLoader) (OCR, error) {
	var out OCR
	get := func(key, def string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return def
	}
	fail := func(key string) (OCR, error) { return OCR{}, fmt.Errorf("invalid %s", key) }
	raw := get("SCANNER_OCR_PLUGINS", "tesseract")
	ids := []string{}
	if raw != "" {
		ids = strings.Split(raw, ",")
	}
	if len(ids) > 32 {
		return fail("SCANNER_OCR_PLUGINS")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !pluginID(id) || seen[id] || loaders[id] == nil {
			return fail("SCANNER_OCR_PLUGINS")
		}
		seen[id] = true
	}
	defaultID := ""
	if len(ids) > 0 {
		defaultID = ids[0]
	}
	out.DefaultPlugin = get("SCANNER_OCR_DEFAULT_PLUGIN", defaultID)
	if len(ids) == 0 {
		if out.DefaultPlugin != "" {
			return fail("SCANNER_OCR_DEFAULT_PLUGIN")
		}
		return out, nil
	}
	if !seen[out.DefaultPlugin] {
		return fail("SCANNER_OCR_DEFAULT_PLUGIN")
	}
	out.DefaultLanguages = strings.Split(get("SCANNER_OCR_DEFAULT_LANGUAGES", "ja,en"), ",")
	langs := map[string]bool{}
	if len(out.DefaultLanguages) > 32 {
		return fail("SCANNER_OCR_DEFAULT_LANGUAGES")
	}
	for _, lang := range out.DefaultLanguages {
		if !languageID(lang) || langs[lang] {
			return fail("SCANNER_OCR_DEFAULT_LANGUAGES")
		}
		langs[lang] = true
	}
	for _, id := range ids {
		settings, err := loaders[id](lookup)
		if err != nil || settings == nil {
			return fail("SCANNER_OCR_PLUGINS (" + id + ")")
		}
		out.Plugins = append(out.Plugins, PluginSettings{ID: id, Settings: settings})
	}
	return out, nil
}

func LoadTesseract(lookup Lookup) (any, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return def
	}
	out := Tesseract{Path: get("SCANNER_OCR_TESSERACT_PATH", "/usr/bin/tesseract"), DataDir: get("SCANNER_OCR_TESSERACT_DATA_DIR", "/usr/share/tesseract-ocr/5/tessdata")}
	for key, path := range map[string]string{"SCANNER_OCR_TESSERACT_PATH": out.Path, "SCANNER_OCR_TESSERACT_DATA_DIR": out.DataDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
			return nil, fmt.Errorf("invalid %s", key)
		}
	}
	return out, nil
}
func pluginID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func languageID(id string) bool {
	if len(id) < 2 || len(id) > 35 {
		return false
	}
	for _, part := range strings.Split(id, "-") {
		if len(part) == 0 || len(part) > 8 {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}
