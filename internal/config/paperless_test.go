package config

import "testing"

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
}
