package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
}
func base() map[string]string {
	return map[string]string{"SCANNER_PUBLIC_URL": "https://scanner.example.test", "SCANNER_OIDC_ISSUER": "https://id.example.test/realm", "SCANNER_OIDC_CLIENT_ID": "scanner", "SCANNER_OIDC_CLIENT_SECRET": "test-secret-never-log"}
}
func TestValidModes(t *testing.T) {
	for _, mode := range []string{"oidc", "guest"} {
		t.Run(mode, func(t *testing.T) {
			v := base()
			v["SCANNER_AUTH_MODE"] = mode
			if mode == "guest" {
				delete(v, "SCANNER_OIDC_CLIENT_SECRET")
				delete(v, "SCANNER_OIDC_ISSUER")
				delete(v, "SCANNER_OIDC_CLIENT_ID")
			}
			c, err := Load(env(v))
			if err != nil {
				t.Fatal(err)
			}
			if c.AuthenticateType != mode || c.Durations["SCANNER_RETENTION_SECONDS"] != 24*time.Hour || c.Limits["SCANNER_PROCESSING_CONCURRENCY"] != 1 {
				t.Fatal("incorrect configuration")
			}
		})
	}
}
func TestInvalidSettingsFailClosed(t *testing.T) {
	cases := []struct{ key, value string }{
		{"SCANNER_PUBLIC_URL", "http://scanner.example.test"},
		{"SCANNER_PUBLIC_URL", "https://scanner.example.test/private"},
		{"SCANNER_PUBLIC_URL", "https://secret@scanner.example.test"},
		{"SCANNER_AUTH_MODE", "auto"},
		{"SCANNER_LISTEN_ADDR", ":banana"},
		{"SCANNER_LISTEN_ADDR", ":65536"},
		{"SCANNER_OIDC_ADMIN_GROUPS", "null"},
		{"SCANNER_OIDC_ADMIN_GROUPS", `["admins","admins"]`},
		{"SCANNER_SCANNERS_JSON", "null"},
		{"SCANNER_OIDC_ISSUER", "http://id.example.test"},
		{"SCANNER_OIDC_CLIENT_SECRET", ""},
		{"SCANNER_OIDC_CLIENT_AUTH", "guess"},
		{"SCANNER_OIDC_SCOPES", "profile"},
		{"SCANNER_OIDC_SCOPES", "openid,openid"},
		{"SCANNER_OIDC_ADMIN_GROUPS", "[1]"},
		{"SCANNER_STATE_DIR", "relative"},
		{"SCANNER_DOCUMENT_DIR", "/var/lib/scanner/state/nested"},
		{"SCANNER_DOCUMENT_DIR", "/var/lib/scanner"},
		{"SCANNER_DOCUMENT_DIR", "/var/lib/scanner/state"},
		{"SCANNER_SESSION_IDLE_SECONDS", "28801"},
		{"SCANNER_RETENTION_SECONDS", "-1"},
		{"SCANNER_RETENTION_SECONDS", "9223372036854775807"},
		{"SCANNER_RECEIPT_RETENTION_SECONDS", "1"},
		{"SCANNER_JOB_MAX_BYTES", "999999999"},
		{"SCANNER_GLOBAL_MAX_BYTES", "1"},
		{"SCANNER_PROCESSING_CONCURRENCY", "2"},
		{"SCANNER_JOB_MAX_PAGES", "0"},
		{"SCANNER_LOG_LEVEL", "trace"},
		{"SCANNER_SCANNERS_JSON", `[{"id":"scanner_example_0001","name":"Scanner","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL","password":"unexpected"}]`},
		{"SCANNER_SCANNERS_JSON", `[] {}`},
	}
	for _, tc := range cases {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			v := base()
			v[tc.key] = tc.value
			_, err := Load(env(v))
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected key-only rejection for %s", tc.key)
			}
			if strings.Contains(err.Error(), "test-secret-never-log") {
				t.Fatal("secret in error")
			}
		})
	}
}
func TestScanners(t *testing.T) {
	v := base()
	scanner := `{"id":"scanner_example_0001","name":"Canon","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"}`
	v["SCANNER_SCANNERS_JSON"] = "[" + scanner + "]"
	c, err := Load(env(v))
	if err != nil || len(c.Scanners) != 1 {
		t.Fatalf("scanner not loaded: %v", err)
	}
	v["SCANNER_SCANNERS_JSON"] = "[" + scanner + "," + scanner + "]"
	if _, err := Load(env(v)); err == nil {
		t.Fatal("duplicate scanner accepted")
	}
}
func TestSecretRedaction(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{string(data), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(output, "test-secret-never-log") {
			t.Fatal("secret leaked")
		}
	}
	if string(c.OIDC.ClientSecret) != "test-secret-never-log" {
		t.Fatal("secret not available for explicit adapter use")
	}
}

func TestWorkerDoesNotReadAuthenticationConfiguration(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if strings.HasPrefix(key, "SCANNER_OIDC_") || key == "SCANNER_PUBLIC_URL" || key == "SCANNER_LISTEN_ADDR" || strings.HasPrefix(key, "SCANNER_AUTH_") || key == "SCANNER_AUTHENTICATE_TYPE" {
			t.Fatalf("worker accessed API-only setting %s", key)
		}
		return "", false
	}
	c, err := LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if c.Role != Worker || c.OIDC.ClientSecret != "" || c.AuthenticateType != "" || c.PublicURL != "" || c.Limits["SCANNER_JOB_MAX_PAGES"] != 50 {
		t.Fatal("unexpected worker configuration")
	}
}

func TestAPILimitsValidatedAndWorkerDoesNotReadThem(t *testing.T) {
	for key, value := range map[string]string{"SCANNER_API_MAX_INFLIGHT": "513", "SCANNER_API_REQUESTS_PER_MINUTE": "0", "SCANNER_API_BURST": "1025"} {
		v := base()
		v[key] = value
		if _, err := Load(env(v)); err == nil {
			t.Fatal("invalid API limit accepted", key)
		}
	}
	c, err := LoadWorker(func(key string) (string, bool) {
		if strings.HasPrefix(key, "SCANNER_API_") {
			t.Fatal("worker read API settings")
		}
		return "", false
	})
	if err != nil || c.Role != Worker {
		t.Fatal(err)
	}
}

func TestTransferTimeoutIsBoundedAndAPIOnly(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil || c.Durations["SCANNER_TRANSFER_TIMEOUT_SECONDS"] != 5*time.Minute {
		t.Fatal("transfer default", err)
	}
	for _, raw := range []string{"0", "-1", "901", "invalid"} {
		values := base()
		values["SCANNER_TRANSFER_TIMEOUT_SECONDS"] = raw
		if _, err := Load(env(values)); err == nil {
			t.Fatal("invalid transfer duration", raw)
		}
	}
	if _, err := LoadWorker(func(key string) (string, bool) {
		if key == "SCANNER_TRANSFER_TIMEOUT_SECONDS" {
			t.Fatal("worker accessed API-only transfer timeout")
		}
		return "", false
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogLeaseIsWorkerOnlyAndBounded(t *testing.T) {
	for _, value := range []string{"0", "14", "301", "bad"} {
		if _, err := LoadWorker(env(map[string]string{"SCANNER_OCR_CATALOG_LEASE_SECONDS": value})); err == nil {
			t.Fatal("invalid catalog lease", value)
		}
	}
	values := base()
	if _, err := Load(func(key string) (string, bool) {
		if key == "SCANNER_OCR_CATALOG_LEASE_SECONDS" {
			t.Fatal("API read worker-only catalog lease")
		}
		value, ok := values[key]
		return value, ok
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := LoadWorker(env(nil))
	if err != nil || worker.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"] != 30*time.Second {
		t.Fatal("catalog default", err)
	}
}

func TestCanonicalAuthenticateTypeAndLegacyConflict(t *testing.T) {
	for _, kind := range []string{"oidc", "simple"} {
		values := base()
		delete(values, "SCANNER_AUTH_MODE")
		values["SCANNER_AUTHENTICATE_TYPE"] = kind
		c, err := Load(func(key string) (string, bool) {
			if kind == "simple" && strings.HasPrefix(key, "SCANNER_OIDC_") {
				t.Fatal("simple mode accessed OIDC setting", key)
			}
			value, ok := values[key]
			return value, ok
		})
		if err != nil || c.AuthenticateType != kind {
			t.Fatal("canonical authenticate type failed", kind, err)
		}
	}
	values := base()
	values["SCANNER_AUTH_MODE"] = "oidc"
	values["SCANNER_AUTHENTICATE_TYPE"] = "oidc"
	if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), "SCANNER_AUTHENTICATE_TYPE") {
		t.Fatal("ambiguous auth keys accepted", err)
	}
	for _, kind := range []string{"none", "guest", "automatic", ""} {
		values := base()
		delete(values, "SCANNER_AUTH_MODE")
		values["SCANNER_AUTHENTICATE_TYPE"] = kind
		if _, err := Load(env(values)); err == nil {
			t.Fatal("unsupported canonical type accepted", kind)
		}
	}
	for key, value := range map[string]string{"SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE": "601", "SCANNER_AUTH_LOGIN_BURST": "21"} {
		values := base()
		values[key] = value
		if _, err := Load(env(values)); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatal("unbounded login policy accepted", key, err)
		}
	}
}

func TestCleanupTimeoutIsWorkerOnlyAndBounded(t *testing.T) {
	for _, raw := range []string{"", "0", "901", "-1", "overflow"} {
		if _, err := LoadWorker(env(map[string]string{"SCANNER_CLEANUP_TIMEOUT_SECONDS": raw})); err == nil {
			t.Fatal(raw)
		}
	}
	for _, seconds := range []int{1, 300, 900} {
		c, err := LoadWorker(env(map[string]string{"SCANNER_CLEANUP_TIMEOUT_SECONDS": fmt.Sprint(seconds)}))
		if err != nil || c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"] != time.Duration(seconds)*time.Second {
			t.Fatal(c, err)
		}
	}
	v := base()
	v["SCANNER_CLEANUP_TIMEOUT_SECONDS"] = "invalid-worker-setting"
	c, err := Load(env(v))
	if err != nil || c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"] != 0 {
		t.Fatal("API consumed worker GC setting", err)
	}
}
