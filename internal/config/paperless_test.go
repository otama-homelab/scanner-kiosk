package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func paperlessEnv() map[string]string {
	v := base()
	v["SCANNER_PAPERLESS_ENABLED"] = "true"
	v["SCANNER_PAPERLESS_DESTINATION"] = "Document archive"
	v["SCANNER_PAPERLESS_BASE_URL"] = "https://archive.example.invalid/paperless/"
	v["SCANNER_PAPERLESS_TOKEN_ENV"] = "SCANNER_PAPERLESS_TOKEN_ARCHIVE"
	v["SCANNER_PAPERLESS_TOKEN_ARCHIVE"] = "synthetic-token"
	return v
}

func TestPaperlessConfigurationBoundary(t *testing.T) {
	v := base()
	v["SCANNER_PAPERLESS_ENABLED"] = "true"
	v["SCANNER_PAPERLESS_DESTINATION"] = "Document archive"
	v["SCANNER_PAPERLESS_BASE_URL"] = "https://user:password@example.invalid/"
	v["SCANNER_PAPERLESS_TOKEN_ENV"] = "SCANNER_PAPERLESS_TOKEN_ARCHIVE"
	v["SCANNER_PAPERLESS_TOKEN_ARCHIVE"] = "synthetic-token"
	if _, err := Load(env(v)); err == nil {
		t.Fatal("enabled Paperless accepted a credential-bearing destination URL")
	}
	t.Run("disabled", func(t *testing.T) {
		p, err := LoadPaperless(func(k string) (string, bool) {
			if k != "SCANNER_PAPERLESS_ENABLED" {
				t.Fatal("disabled reads other settings", k)
			}
			return "false", true
		})
		client, clientErr := p.NewClient()
		if err != nil || p.Enabled || p.Configured || client != nil || clientErr != nil {
			t.Fatal("disabled acquired remote authority")
		}
	})
	t.Run("missing secret", func(t *testing.T) {
		v := paperlessEnv()
		delete(v, "SCANNER_PAPERLESS_TOKEN_ARCHIVE")
		p, err := LoadPaperless(env(v))
		client, clientErr := p.NewClient()
		if err != nil || !p.Enabled || p.Configured || client != nil || clientErr != nil {
			t.Fatal("unconfigured acquired remote authority")
		}
		assertPaperlessPrivate(t, p)
	})
	t.Run("roles and redaction", func(t *testing.T) {
		v := paperlessEnv()
		api, err := Load(env(v))
		if err != nil {
			t.Fatal(err)
		}
		worker, err := LoadWorker(env(v))
		if err != nil {
			t.Fatal(err)
		}
		if api.Paperless != worker.Paperless || !api.Paperless.Configured {
			t.Fatal("roles disagree")
		}
		client, err := api.Paperless.NewClient()
		if err != nil || client == nil {
			t.Fatal("configured client absent", err)
		}
		client.Close()
		assertPaperlessPrivate(t, api.Paperless)
	})
	for _, tc := range []struct{ key, value string }{
		{"ENABLED", "yes"}, {"DESTINATION", ""}, {"DESTINATION", "bad\nlabel"}, {"DESTINATION", strings.Repeat("x", 257)},
		{"BASE_URL", ""}, {"BASE_URL", "https://user:pass@example.invalid"}, {"BASE_URL", "https://example.invalid/?"}, {"BASE_URL", "https://example.invalid/#"},
		{"BASE_URL", "https://example.invalid/a/../b"}, {"BASE_URL", "https://example.invalid/%2Fother"}, {"BASE_URL", "https://example.invalid/%00"},
		{"BASE_URL", "https://example.invalid:0"}, {"BASE_URL", "https://example.invalid:65536"}, {"BASE_URL", "https://example.invalid:"},
		{"BASE_URL", "https://example.invalid/\rtest"}, {"BASE_URL", "file:///tmp/test"}, {"BASE_URL", "//example.invalid"}, {"BASE_URL", "http://127.0.0.1:8080"},
		{"TOKEN_ENV", "OIDC_CLIENT_SECRET"}, {"TOKEN_ENV", "SCANNER_PAPERLESS_TOKEN_ENV"}, {"TOKEN_ENV", "SCANNER_PAPERLESS_TOKEN_"}, {"TOKEN_ENV", "SCANNER_PAPERLESS_TOKEN_lower"},
		{"TOKEN_ARCHIVE", "bad\rsecret"}, {"TOKEN_ARCHIVE", "bad secret"}, {"TOKEN_ARCHIVE", strings.Repeat("x", 4097)}, {"ALLOW_INSECURE_HTTP", "yes"},
	} {
		t.Run(tc.key+fmt.Sprint(len(tc.value))+tc.value[:min(len(tc.value), 20)], func(t *testing.T) {
			v := paperlessEnv()
			v["SCANNER_PAPERLESS_"+tc.key] = tc.value
			_, err := LoadPaperless(env(v))
			if err == nil || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "archive.example.invalid") {
				t.Fatal("unsafe config or error", err)
			}
		})
	}
	t.Run("explicit loopback only", func(t *testing.T) {
		for _, host := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "192.0.2.10:8080", "example.invalid:8080"} {
			v := paperlessEnv()
			v["SCANNER_PAPERLESS_BASE_URL"] = "http://" + host
			v["SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP"] = "true"
			_, err := LoadPaperless(env(v))
			want := host == "127.0.0.1:8080" || host == "[::1]:8080" || host == "localhost:8080"
			if (err == nil) != want {
				t.Fatal("loopback boundary", host, err)
			}
		}
	})
}

func assertPaperlessPrivate(t *testing.T, p Paperless) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"synthetic-token", "SCANNER_PAPERLESS_TOKEN_ARCHIVE", "archive.example.invalid", p.ConfigFingerprint} {
		if private != "" && strings.Contains(string(data), private) {
			t.Fatal("private authority in JSON")
		}
	}
	if strings.Contains(fmt.Sprint(p), "synthetic-token") {
		t.Fatal("secret in formatting")
	}
}

func TestPaperlessDestinationIdentity(t *testing.T) {
	v := paperlessEnv()
	p, err := LoadPaperless(env(v))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://archive.example.invalid/paperless", "https://ARCHIVE.example.invalid:443/paperless/"} {
		v["SCANNER_PAPERLESS_BASE_URL"] = raw
		v["SCANNER_PAPERLESS_TOKEN_ARCHIVE"] = "rotated-token"
		q, err := LoadPaperless(env(v))
		if err != nil || q.DestinationID != p.DestinationID || q.ConfigFingerprint != p.ConfigFingerprint {
			t.Fatal("rotation/canonical URL changed identity", err)
		}
	}
	for _, raw := range []string{"https://other.example.invalid/paperless", "https://archive.example.invalid/other", "https://archive.example.invalid:8443/paperless"} {
		v["SCANNER_PAPERLESS_BASE_URL"] = raw
		q, err := LoadPaperless(env(v))
		if err != nil || q.DestinationID == p.DestinationID || q.ConfigFingerprint == p.ConfigFingerprint {
			t.Fatal("destination drift retained identity", err)
		}
	}
}
