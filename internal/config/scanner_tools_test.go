package config

import (
	"strings"
	"testing"
)

func TestNativeScannerSettingsAreWorkerOnly(t *testing.T) {
	values := base()
	if _, err := Load(func(key string) (string, bool) {
		if key == "SSL_CERT_FILE" || key == "SCANNER_SCAN_CONCURRENCY" || key == "SCANNER_ACQUISITION_ENABLED" || key == "SCANNER_SCANIMAGE_PATH" || strings.HasPrefix(key, "SCANNER_SANE_") {
			t.Fatal("API read native scanner setting", key)
		}
		value, ok := values[key]
		return value, ok
	}); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWorker(env(nil))
	if err != nil || c.ScannerTools.ScanimagePath != "/usr/bin/scanimage" || c.ScannerTools.ParametersPath != "/usr/libexec/scanner/sane-parameters" || c.ScannerTools.LibraryDirs == nil {
		t.Fatal("invalid worker native defaults", err)
	}
}
func TestInvalidNativeScannerSettingsFailWithoutEchoingValues(t *testing.T) {
	for key, values := range map[string][]string{"SCANNER_SCANIMAGE_PATH": {"relative-secret", "/tmp/../private", "/tmp/private\n"}, "SCANNER_SANE_PARAMETERS_PATH": {"", "relative-private"}, "SCANNER_SANE_LIBRARY_DIRS_JSON": {"null", `["relative-private"]`, `["/a","/a"]`, `["/a:/private"]`, `["/a;/private"]`}} {
		for _, value := range values {
			if _, err := LoadWorker(env(map[string]string{key: value})); err == nil || value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("invalid scanner setting accepted or exposed", key)
			}
		}
	}
}

func TestScannerIDMatchesPublicAndDriverBounds(t *testing.T) {
	for _, length := range []int{96, 97} {
		raw := `[{"id":"` + strings.Repeat("s", length) + `","name":"Scanner","driver":"sane-airscan","endpoint":"http://192.0.2.10/eSCL"}]`
		_, err := LoadWorker(env(map[string]string{"SCANNER_SCANNERS_JSON": raw}))
		if (err == nil) != (length == 96) {
			t.Fatal("scanner ID bounds differ", length, err)
		}
	}
}

func TestScannerAcquisitionRequiresExplicitWorkerOptIn(t *testing.T) {
	for _, value := range []string{"true", "false", "", "1", "TRUE", "yes"} {
		c, err := LoadWorker(env(map[string]string{"SCANNER_ACQUISITION_ENABLED": value}))
		valid := value == "true" || value == "false"
		if (err == nil) != valid {
			t.Fatalf("unexpected flag validation for %q: %v", value, err)
		}
		if valid && c.ScannerTools.AcquisitionEnabled != (value == "true") {
			t.Fatal("wrong acquisition setting")
		}
	}
	c, err := LoadWorker(env(nil))
	if err != nil || c.ScannerTools.AcquisitionEnabled {
		t.Fatal("acquisition enabled by default", err)
	}
}

func TestScanConcurrencyBoundsAndEffectiveCapacity(t *testing.T) {
	for _, value := range []string{"0", "33", "-1", "", "bad"} {
		if _, err := LoadWorker(env(map[string]string{"SCANNER_SCAN_CONCURRENCY": value})); err == nil {
			t.Fatal("invalid scan slots accepted")
		}
	}
	c, err := LoadWorker(env(map[string]string{"SCANNER_SCAN_CONCURRENCY": "32"}))
	if err != nil {
		t.Fatal(err)
	}
	c.ScannerDiscovery = false
	c.Scanners = make([]Scanner, 3)
	if c.EffectiveScanConcurrency() != 3 {
		t.Fatal("device cap ignored")
	}
	c.Limits["SCANNER_GLOBAL_MAX_BYTES"] = c.Limits["SCANNER_JOB_MAX_BYTES"] * 2
	if c.EffectiveScanConcurrency() != 2 {
		t.Fatal("shared reservation cap ignored")
	}
	c.ScannerDiscovery = true
	c.Scanners = nil
	if c.EffectiveScanConcurrency() != 2 {
		t.Fatal("discovered devices cannot use bounded configured capacity")
	}
	c.ScannerTools.Concurrency = 1
	if c.EffectiveScanConcurrency() != 1 {
		t.Fatal("configured scan cap ignored")
	}
}

func TestNativeScannerCAPathValidation(t *testing.T) {
	for _, value := range []string{"", "relative-private", "/tmp/../private", "/private\n", strings.Repeat("/a", 2050)} {
		if _, err := LoadWorker(env(map[string]string{"SSL_CERT_FILE": value})); err == nil || value != "" && strings.Contains(err.Error(), value) {
			t.Fatal("invalid explicit native CA path accepted or exposed")
		}
	}
	if c, err := LoadWorker(env(map[string]string{"SSL_CERT_FILE": "/private/not-installed/ca.pem"})); err != nil || c.ScannerTools.CAFile != "/private/not-installed/ca.pem" {
		t.Fatal("configuration must not probe trust files", err)
	}
}
