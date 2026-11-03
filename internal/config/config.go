// Package config reads deployment settings exclusively from environment variables.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Lookup func(string) (string, bool)
type Secret string

func (Secret) String() string               { return "[redacted]" }
func (Secret) GoString() string             { return "[redacted]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

type Scanner struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Endpoint string `json:"endpoint"`
}
type OIDC struct {
	Issuer, ClientID, ClientAuth string
	ClientSecret                 Secret
	Scopes, AdminGroups          []string
}
type Role string

const (
	API    Role = "api"
	Worker Role = "worker"
)

type Config struct {
	Role                                                                     Role
	PublicURL, ListenAddr, AuthenticateType, StateDir, DocumentDir, LogLevel string
	OIDC                                                                     OIDC
	Scanners                                                                 []Scanner
	ScannerTools                                                             ScannerTools
	// Duration and limit keys use their full environment names. Only the listed,
	// validated values are exposed; plugins have independent configuration loaders.
	Durations map[string]time.Duration
	Limits    map[string]int64
}

func Load(lookup Lookup) (Config, error) { return load(lookup, API) }

// LoadWorker reads common settings without reading or requiring HTTP/OIDC secrets.
func LoadWorker(lookup Lookup) (Config, error) { return load(lookup, Worker) }

func load(lookup Lookup, role Role) (Config, error) {
	c := Config{Role: role}
	get := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return fallback
	}
	invalid := func(key string) (Config, error) { return Config{}, fmt.Errorf("invalid %s", key) }
	if role == API {

		c.PublicURL = get("SCANNER_PUBLIC_URL", "")
		if !validURL(c.PublicURL, true) {
			return invalid("SCANNER_PUBLIC_URL")
		}
		c.ListenAddr = get("SCANNER_LISTEN_ADDR", ":8080")
		if _, port, err := net.SplitHostPort(c.ListenAddr); err != nil || !validPort(port) {
			return invalid("SCANNER_LISTEN_ADDR")
		}
		authType, canonical := lookup("SCANNER_AUTHENTICATE_TYPE")
		legacy, old := lookup("SCANNER_AUTH_MODE")
		if canonical && old {
			return invalid("SCANNER_AUTHENTICATE_TYPE")
		}
		c.AuthenticateType = "oidc"
		if canonical {
			c.AuthenticateType = authType
		} else if old {
			c.AuthenticateType = legacy
		}
		if c.AuthenticateType != "oidc" && c.AuthenticateType != "simple" && !(old && c.AuthenticateType == "guest") {
			if canonical {
				return invalid("SCANNER_AUTHENTICATE_TYPE")
			}
			return invalid("SCANNER_AUTH_MODE")
		}
	}
	c.StateDir = get("SCANNER_STATE_DIR", "/var/lib/scanner/state")
	c.DocumentDir = get("SCANNER_DOCUMENT_DIR", "/var/lib/scanner/documents")
	for key, path := range map[string]string{"SCANNER_STATE_DIR": c.StateDir, "SCANNER_DOCUMENT_DIR": c.DocumentDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return invalid(key)
		}
	}
	rel, _ := filepath.Rel(c.StateDir, c.DocumentDir)
	reverse, _ := filepath.Rel(c.DocumentDir, c.StateDir)
	if rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." || !strings.HasPrefix(reverse, ".."+string(filepath.Separator)) && reverse != ".." {
		return invalid("SCANNER_DOCUMENT_DIR")
	}
	if role == API && c.AuthenticateType == "oidc" {
		c.OIDC = OIDC{Issuer: get("SCANNER_OIDC_ISSUER", ""), ClientID: get("SCANNER_OIDC_CLIENT_ID", ""), ClientAuth: get("SCANNER_OIDC_CLIENT_AUTH", "client_secret_basic"), ClientSecret: Secret(get("SCANNER_OIDC_CLIENT_SECRET", ""))}
		c.OIDC.Scopes = strings.Split(get("SCANNER_OIDC_SCOPES", "openid,profile"), ",")
		if c.AuthenticateType == "oidc" {
			if !validURL(c.OIDC.Issuer, false) {
				return invalid("SCANNER_OIDC_ISSUER")
			}
			if c.OIDC.ClientID == "" {
				return invalid("SCANNER_OIDC_CLIENT_ID")
			}
			if c.OIDC.ClientAuth != "client_secret_basic" && c.OIDC.ClientAuth != "client_secret_post" && c.OIDC.ClientAuth != "none" {
				return invalid("SCANNER_OIDC_CLIENT_AUTH")
			}
			if c.OIDC.ClientAuth != "none" && c.OIDC.ClientSecret == "" {
				return invalid("SCANNER_OIDC_CLIENT_SECRET")
			}
			openid := false
			seen := map[string]bool{}
			for _, s := range c.OIDC.Scopes {
				if s == "" || strings.TrimSpace(s) != s || seen[s] {
					return invalid("SCANNER_OIDC_SCOPES")
				}
				seen[s] = true
				openid = openid || s == "openid"
			}
			if !openid {
				return invalid("SCANNER_OIDC_SCOPES")
			}
		}
		if err := decode(get("SCANNER_OIDC_ADMIN_GROUPS", "[]"), &c.OIDC.AdminGroups); err != nil || c.OIDC.AdminGroups == nil {
			return invalid("SCANNER_OIDC_ADMIN_GROUPS")
		}
		groups := map[string]bool{}
		for _, group := range c.OIDC.AdminGroups {
			if group == "" || strings.TrimSpace(group) != group || groups[group] {
				return invalid("SCANNER_OIDC_ADMIN_GROUPS")
			}
			groups[group] = true
		}
	}
	if err := decode(get("SCANNER_SCANNERS_JSON", "[]"), &c.Scanners); err != nil || c.Scanners == nil || len(c.Scanners) > 32 {
		return invalid("SCANNER_SCANNERS_JSON")
	}

	if ValidateScannerEndpoints(c.Scanners) != nil {
		return invalid("SCANNER_SCANNERS_JSON")
	}
	seen := map[string]bool{}
	for _, s := range c.Scanners {
		if !validID(s.ID) || s.Name == "" || utf8.RuneCountInString(s.Name) > 256 || seen[s.ID] || s.Driver != "sane-airscan" || !scannerURL(s.Endpoint) {
			return invalid("SCANNER_SCANNERS_JSON")
		}
		seen[s.ID] = true
	}
	c.Durations = map[string]time.Duration{}
	for key, def := range map[string]int64{"SCANNER_SESSION_LIFETIME_SECONDS": 28800, "SCANNER_SESSION_IDLE_SECONDS": 1800, "SCANNER_RETENTION_SECONDS": 86400, "SCANNER_RECEIPT_RETENTION_SECONDS": 259200, "SCANNER_SCAN_TIMEOUT_SECONDS": 120, "SCANNER_OCR_TIMEOUT_SECONDS": 600, "SCANNER_PROCESS_STOP_GRACE_SECONDS": 15} {
		n, err := strconv.ParseInt(get(key, strconv.FormatInt(def, 10)), 10, 64)
		if err != nil || n < 1 || n > 31536000 {
			return invalid(key)
		}
		c.Durations[key] = time.Duration(n) * time.Second
	}
	if role == Worker {
		timeout, err := strconv.ParseInt(get("SCANNER_CLEANUP_TIMEOUT_SECONDS", "300"), 10, 64)
		if err != nil || timeout < 1 || timeout > 900 {
			return invalid("SCANNER_CLEANUP_TIMEOUT_SECONDS")
		}
		c.Durations["SCANNER_CLEANUP_TIMEOUT_SECONDS"] = time.Duration(timeout) * time.Second
		c.ScannerTools, err = LoadScannerTools(lookup)
		if err != nil {
			return Config{}, err
		}
		n, err := strconv.ParseInt(get("SCANNER_OCR_CATALOG_LEASE_SECONDS", "30"), 10, 64)
		if err != nil || n < 15 || n > 300 {
			return invalid("SCANNER_OCR_CATALOG_LEASE_SECONDS")
		}
		c.Durations["SCANNER_OCR_CATALOG_LEASE_SECONDS"] = time.Duration(n) * time.Second
	}

	if c.Durations["SCANNER_SESSION_IDLE_SECONDS"] > c.Durations["SCANNER_SESSION_LIFETIME_SECONDS"] {
		return invalid("SCANNER_SESSION_IDLE_SECONDS")
	}
	if c.Durations["SCANNER_RECEIPT_RETENTION_SECONDS"] < c.Durations["SCANNER_RETENTION_SECONDS"] {
		return invalid("SCANNER_RECEIPT_RETENTION_SECONDS")
	}
	c.Limits = map[string]int64{}
	if role == API {
		for key, def := range map[string]int64{"SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE": 10, "SCANNER_AUTH_LOGIN_BURST": 5} {
			n, err := strconv.ParseInt(get(key, strconv.FormatInt(def, 10)), 10, 64)
			upper := int64(600)
			if key == "SCANNER_AUTH_LOGIN_BURST" {
				upper = 20
			}
			if err != nil || n < 1 || n > upper {
				return invalid(key)
			}
			c.Limits[key] = n
		}
	}

	for key, def := range map[string]int64{"SCANNER_USER_MAX_DOCUMENTS": 20, "SCANNER_USER_MAX_BYTES": 524288000, "SCANNER_GLOBAL_MAX_BYTES": 1073741824, "SCANNER_MIN_FREE_BYTES": 67108864, "SCANNER_JOB_MAX_PAGES": 50, "SCANNER_JOB_MAX_BYTES": 209715200, "SCANNER_MAX_PAGE_PIXELS": 40000000, "SCANNER_PROCESSING_CONCURRENCY": 1, "SCANNER_OCR_MAX_RESULT_BYTES": 16777216} {
		n, err := strconv.ParseInt(get(key, strconv.FormatInt(def, 10)), 10, 64)
		if err != nil || n < 1 {
			return invalid(key)
		}
		c.Limits[key] = n
	}
	if c.Limits["SCANNER_PROCESSING_CONCURRENCY"] != 1 {
		return invalid("SCANNER_PROCESSING_CONCURRENCY")
	}
	if c.Limits["SCANNER_JOB_MAX_BYTES"] > c.Limits["SCANNER_GLOBAL_MAX_BYTES"] {
		return invalid("SCANNER_GLOBAL_MAX_BYTES")
	}
	if c.Limits["SCANNER_JOB_MAX_BYTES"] > c.Limits["SCANNER_USER_MAX_BYTES"] {
		return invalid("SCANNER_JOB_MAX_BYTES")
	}
	if role == API {
		n, err := strconv.ParseInt(get("SCANNER_TRANSFER_TIMEOUT_SECONDS", "300"), 10, 64)
		if err != nil || n < 1 || n > 900 {
			return invalid("SCANNER_TRANSFER_TIMEOUT_SECONDS")
		}
		c.Durations["SCANNER_TRANSFER_TIMEOUT_SECONDS"] = time.Duration(n) * time.Second
		for key, bounds := range map[string][2]int64{"SCANNER_API_MAX_INFLIGHT": {32, 512}, "SCANNER_API_REQUESTS_PER_MINUTE": {600, 60000}, "SCANNER_API_BURST": {32, 1024}} {
			n, err := strconv.ParseInt(get(key, strconv.FormatInt(bounds[0], 10)), 10, 64)
			if err != nil || n < 1 || n > bounds[1] {
				return invalid(key)
			}
			c.Limits[key] = n
		}
	}
	c.LogLevel = get("SCANNER_LOG_LEVEL", "info")
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return invalid("SCANNER_LOG_LEVEL")
	}
	return c, nil
}
func decode(raw string, out any) error {
	if len(raw) > 1048576 {
		return fmt.Errorf("configuration too large")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func validURL(raw string, origin bool) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && (u.Port() == "" || validPort(u.Port())) && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (!origin || u.Path == "" || u.Path == "/") && !strings.ContainsAny(raw, "\r\n")
}
func scannerURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && (u.Port() == "" || validPort(u.Port())) && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func validPort(s string) bool { n, err := strconv.Atoi(s); return err == nil && n >= 1 && n <= 65535 }
func validID(s string) bool {
	if len(s) < 16 || len(s) > 96 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
