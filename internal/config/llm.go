package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"scanner-kiosk/pkg/ocrplugin"
)

// LLMProfile contains non-secret deployment metadata. Credentials are referenced
// by environment variable name and resolved only in the worker composition root.
type LLMProfile struct {
	ProcessingDestination string                 `json:"processing_destination,omitempty"`
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Protocol              string                 `json:"protocol"`
	Endpoint              string                 `json:"endpoint"`
	APIKeyEnv             string                 `json:"api_key_env"`
	AllowInsecureHTTP     bool                   `json:"allow_insecure_http"`
	Model                 string                 `json:"model"`
	ResponseModel         string                 `json:"response_model"`
	Version               string                 `json:"version"`
	Languages             []string               `json:"languages"`
	Outputs               []ocrplugin.OutputKind `json:"outputs"`
	MaxCompletionTokens   int                    `json:"max_completion_tokens"`
	TokenParameter        string                 `json:"token_parameter"`
}
type LLM struct {
	Profiles          []LLMProfile
	TextLayerFontPath string
}

func LoadLLM(lookup Lookup) (any, error) {
	raw, ok := lookup("SCANNER_OCR_LLM_PROFILES_JSON")
	if !ok {
		raw = "[]"
	}
	var profiles []LLMProfile
	fail := func() (any, error) { return nil, fmt.Errorf("invalid SCANNER_OCR_LLM_PROFILES_JSON") }
	if err := decode(raw, &profiles); err != nil || len(profiles) < 1 || len(profiles) > 32 {
		return fail()
	}
	seen := map[string]bool{}
	for i := range profiles {
		p := &profiles[i]
		if !pluginID(p.ID) || seen[p.ID] || !label(p.Name) || p.Protocol != "chat-completions" || !label(p.Model) || !label(p.ResponseModel) || !label(p.Version) || len(p.Version) > 128 {
			return fail()
		}
		if p.ProcessingDestination != "" && !label(p.ProcessingDestination) {
			return fail()
		}
		seen[p.ID] = true
		u, err := url.Parse(p.Endpoint)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Port() != "" && !validPort(u.Port())) || (u.Scheme != "https" && !(u.Scheme == "http" && p.AllowInsecureHTTP)) || strings.ContainsAny(p.Endpoint, "\x00\r\n") {
			return fail()
		}
		if p.APIKeyEnv != "" && !credentialName(p.APIKeyEnv) {
			return fail()
		}
		if p.MaxCompletionTokens < 1 || p.MaxCompletionTokens > 1000000 {
			return fail()
		}
		if p.TokenParameter == "" {
			p.TokenParameter = "max_completion_tokens"
		}
		if p.TokenParameter != "max_completion_tokens" && p.TokenParameter != "max_tokens" {
			return fail()
		}
		langs := map[string]bool{}
		if len(p.Languages) < 1 || len(p.Languages) > 32 {
			return fail()
		}
		for _, lang := range p.Languages {
			if !languageID(lang) || langs[lang] {
				return fail()
			}
			langs[lang] = true
		}
		outputs := map[ocrplugin.OutputKind]bool{}
		if len(p.Outputs) < 1 || len(p.Outputs) > 2 {
			return fail()
		}
		for _, kind := range p.Outputs {
			if (kind != ocrplugin.Text && kind != ocrplugin.PositionedText) || outputs[kind] {
				return fail()
			}
			outputs[kind] = true
		}
	}
	fontPath := ""
	if value, ok := lookup("SCANNER_OCR_LLM_TEXT_LAYER_FONT"); ok {
		fontPath = value
	}
	if fontPath != "" && (!filepath.IsAbs(fontPath) || filepath.Clean(fontPath) != fontPath || strings.ContainsAny(fontPath, "\x00\r\n")) {
		return nil, fmt.Errorf("invalid SCANNER_OCR_LLM_TEXT_LAYER_FONT")
	}
	return LLM{Profiles: profiles, TextLayerFontPath: fontPath}, nil
}
func label(s string) bool {
	if len(s) == 0 || len(s) > 256 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func credentialName(s string) bool {
	if !strings.HasPrefix(s, "SCANNER_OCR_LLM_KEY_") || len(s) > 128 || len(s) <= len("SCANNER_OCR_LLM_KEY_") {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// ResolveLLMCredentials is worker-only. The API can load metadata without keys.
// It never substitutes OIDC or unrelated environment secrets.
func ResolveLLMCredentials(settings LLM, lookup Lookup) (map[string]Secret, error) {
	keys := map[string]Secret{}
	for _, profile := range settings.Profiles {
		if profile.APIKeyEnv == "" {
			continue
		}
		if !credentialName(profile.APIKeyEnv) {
			return nil, fmt.Errorf("invalid LLM credential reference")
		}
		value, ok := lookup(profile.APIKeyEnv)
		if !ok || value == "" || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("LLM credential unavailable")
		}
		keys[profile.ID] = Secret(value)
	}
	return keys, nil
}
