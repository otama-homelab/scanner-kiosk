package config

import (
	"encoding/json"
	"strings"
	"testing"

	"scanner-kiosk/pkg/ocrplugin"
)

func llmProfile() LLMProfile {
	return LLMProfile{ID: "vision-local", Name: "Local vision", Protocol: "chat-completions", Endpoint: "https://llm.example.test/v1/chat/completions", APIKeyEnv: "SCANNER_OCR_LLM_KEY_LOCAL", Model: "vision-request", ResponseModel: "vision-pinned", Version: "v1", Languages: []string{"ja", "en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PositionedText}, MaxCompletionTokens: 8192}
}
func TestLLMProfileMetadataAndWorkerCredentials(t *testing.T) {
	p := llmProfile()
	raw, _ := json.Marshal([]LLMProfile{p})
	settings, err := LoadLLM(env(map[string]string{"SCANNER_OCR_LLM_PROFILES_JSON": string(raw)}))
	if err != nil {
		t.Fatal(err)
	}
	c := settings.(LLM)
	if c.Profiles[0].TokenParameter != "max_completion_tokens" {
		t.Fatal("missing default token parameter")
	}
	if _, err = ResolveLLMCredentials(c, env(nil)); err == nil {
		t.Fatal("missing worker key accepted")
	}
	keys, err := ResolveLLMCredentials(c, env(map[string]string{p.APIKeyEnv: "secret-canary"}))
	if err != nil || string(keys[p.ID]) != "secret-canary" {
		t.Fatal("worker key resolution failed")
	}
}
func TestLLMInvalidProfiles(t *testing.T) {
	for _, mode := range []string{"http", "protocol", "secret-reference", "model", "response-model", "output", "duplicate-language", "tokens", "token-parameter", "query"} {
		t.Run(mode, func(t *testing.T) {
			p := llmProfile()
			switch mode {
			case "http":
				p.Endpoint = "http://localhost:8000/v1/chat/completions"
			case "protocol":
				p.Protocol = "auto"
			case "secret-reference":
				p.APIKeyEnv = "SCANNER_OIDC_CLIENT_SECRET"
			case "model":
				p.Model = ""
			case "response-model":
				p.ResponseModel = ""
			case "output":
				p.Outputs = []ocrplugin.OutputKind{ocrplugin.SearchablePDF}
			case "duplicate-language":
				p.Languages = []string{"en", "en"}
			case "tokens":
				p.MaxCompletionTokens = 0
			case "token-parameter":
				p.TokenParameter = "auto"
			case "query":
				p.Endpoint += "?api_key=secret-canary"
			}
			raw, _ := json.Marshal([]LLMProfile{p})
			_, err := LoadLLM(env(map[string]string{"SCANNER_OCR_LLM_PROFILES_JSON": string(raw)}))
			if err == nil || strings.Contains(err.Error(), "secret-canary") {
				t.Fatal("invalid profile accepted or leaked")
			}
		})
	}
}
func TestLLMExplicitLocalHTTP(t *testing.T) {
	p := llmProfile()
	p.Endpoint = "http://llm.internal:8000/v1/chat/completions"
	p.AllowInsecureHTTP = true
	p.APIKeyEnv = ""
	raw, _ := json.Marshal([]LLMProfile{p})
	if _, err := LoadLLM(env(map[string]string{"SCANNER_OCR_LLM_PROFILES_JSON": string(raw)})); err != nil {
		t.Fatal(err)
	}
}

func TestLLMTextLayerFontPathIsStaticMetadata(t *testing.T) {
	raw, _ := json.Marshal([]LLMProfile{llmProfile()})
	for _, path := range []string{"", "/nonexistent/approved.ttf", "relative.ttf", "/fonts/../font.ttf", "/font\n.ttf"} {
		settings, err := LoadLLM(env(map[string]string{"SCANNER_OCR_LLM_PROFILES_JSON": string(raw), "SCANNER_OCR_LLM_TEXT_LAYER_FONT": path}))
		valid := path == "" || path == "/nonexistent/approved.ttf"
		if valid {
			if err != nil || settings.(LLM).TextLayerFontPath != path {
				t.Fatalf("valid metadata: %v", err)
			}
		} else if err == nil || strings.Contains(err.Error(), path) {
			t.Fatal("invalid font path accepted or echoed")
		}
	}
}
