package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScannerNameBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		valid       bool
	}{
		{"empty", "", false},
		{"one", "J", true},
		{"ascii_limit", strings.Repeat("a", 256), true},
		{"ascii_over", strings.Repeat("a", 257), false},
		{"japanese_limit", strings.Repeat("書", 256), true},
		{"japanese_over", strings.Repeat("書", 257), false},
		{"emoji_limit", strings.Repeat("📚", 256), true},
		{"emoji_over", strings.Repeat("📚", 257), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := base()
			encoded, err := json.Marshal([]Scanner{{ID: "scanner_example_0001", Name: tc.label, Driver: "sane-airscan", Endpoint: "http://192.0.2.10/eSCL"}})
			if err != nil {
				t.Fatal(err)
			}
			values["SCANNER_SCANNERS_JSON"] = string(encoded)
			c, err := Load(env(values))
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if len(c.Scanners) != 1 || c.Scanners[0].Name != tc.label {
					t.Fatal("scanner name changed")
				}
			} else if err == nil || err.Error() != "invalid SCANNER_SCANNERS_JSON" {
				t.Fatalf("want key-only configuration error, got %v", err)
			}
		})
	}
}
