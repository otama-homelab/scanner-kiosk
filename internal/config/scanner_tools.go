package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ScannerTools is worker-only deployment metadata; paths are not probed here.
type ScannerTools struct {
	AcquisitionEnabled            bool
	Concurrency                   int
	ScanimagePath, ParametersPath string
	CAFile                        string
	LibraryDirs                   []string
}

func LoadScannerTools(lookup Lookup) (ScannerTools, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	out := ScannerTools{ScanimagePath: get("SCANNER_SCANIMAGE_PATH", "/usr/bin/scanimage"), ParametersPath: get("SCANNER_SANE_PARAMETERS_PATH", "/usr/libexec/scanner/sane-parameters")}
	slots, err := strconv.Atoi(get("SCANNER_SCAN_CONCURRENCY", "1"))
	if err != nil || slots < 1 || slots > 32 {
		return ScannerTools{}, fmt.Errorf("invalid SCANNER_SCAN_CONCURRENCY")
	}
	out.Concurrency = slots

	switch get("SCANNER_ACQUISITION_ENABLED", "false") {
	case "true":
		out.AcquisitionEnabled = true
	case "false":
	default:
		return ScannerTools{}, fmt.Errorf("invalid SCANNER_ACQUISITION_ENABLED")
	}
	paths := map[string]string{"SCANNER_SCANIMAGE_PATH": out.ScanimagePath, "SCANNER_SANE_PARAMETERS_PATH": out.ParametersPath}
	if value, ok := lookup("SSL_CERT_FILE"); ok {
		out.CAFile = value
		paths["SSL_CERT_FILE"] = value
	}
	for key, value := range paths {
		if len(value) > 4096 || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return ScannerTools{}, fmt.Errorf("invalid %s", key)
		}
	}
	key := "SCANNER_SANE_LIBRARY_DIRS_JSON"
	if decode(get(key, "[]"), &out.LibraryDirs) != nil || out.LibraryDirs == nil || len(out.LibraryDirs) > 16 {
		return ScannerTools{}, fmt.Errorf("invalid %s", key)
	}
	seen := map[string]bool{}
	for _, path := range out.LibraryDirs {
		if len(path) > 4096 || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n:;") || seen[path] {
			return ScannerTools{}, fmt.Errorf("invalid %s", key)
		}
		seen[path] = true
	}
	return out, nil
}

// EffectiveScanConcurrency caps native tasks by the device bound and the
// shared logical reservation budget. Discovery may add devices after startup. It is not an RSS/headroom measurement.
func (c Config) EffectiveScanConcurrency() int {
	slots := c.ScannerTools.Concurrency
	if slots == 0 {
		slots = 1
	}
	if slots < 1 || slots > 32 {
		return 0
	}
	devices := max(1, len(c.Scanners))
	if c.ScannerDiscovery {
		devices = 32
	}
	slots = min(slots, devices)
	if job := c.Limits["SCANNER_JOB_MAX_BYTES"]; job > 0 {
		slots = min(slots, int(c.Limits["SCANNER_GLOBAL_MAX_BYTES"]/job))
	}
	return slots
}
