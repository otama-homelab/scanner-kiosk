package config

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"scanner-kiosk/internal/paperless"
)

// Paperless holds one operator destination. Only public state is serializable.
type Paperless struct {
	Enabled           bool   `json:"enabled"`
	Configured        bool   `json:"configured"`
	DestinationLabel  string `json:"destination_label,omitempty"`
	BaseURL           string `json:"-"`
	Token             Secret `json:"-"`
	DestinationID     string `json:"-"`
	ConfigFingerprint string `json:"-"`
	AllowHTTP         bool   `json:"-"`
}

func LoadPaperless(lookup Lookup) (Paperless, error) {
	var p Paperless
	get := func(key string) string { v, _ := lookup(key); return v }
	invalid := func(key string) (Paperless, error) { return Paperless{}, fmt.Errorf("invalid %s", key) }
	switch get("SCANNER_PAPERLESS_ENABLED") {
	case "", "false":
		return p, nil
	case "true":
		p.Enabled = true
	default:
		return invalid("SCANNER_PAPERLESS_ENABLED")
	}
	p.DestinationLabel = get("SCANNER_PAPERLESS_DESTINATION")
	if !label(p.DestinationLabel) {
		return invalid("SCANNER_PAPERLESS_DESTINATION")
	}
	switch get("SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP") {
	case "", "false":
	case "true":
		p.AllowHTTP = true
	default:
		return invalid("SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP")
	}
	var err error
	p.BaseURL, err = paperless.CanonicalBaseURL(get("SCANNER_PAPERLESS_BASE_URL"), p.AllowHTTP)
	if err != nil {
		return invalid("SCANNER_PAPERLESS_BASE_URL")
	}
	name := get("SCANNER_PAPERLESS_TOKEN_ENV")
	const prefix = "SCANNER_PAPERLESS_TOKEN_"
	if len(name) <= len(prefix) || len(name) > 128 || !strings.HasPrefix(name, prefix) || name == "SCANNER_PAPERLESS_TOKEN_ENV" || strings.IndexFunc(name, func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') }) >= 0 {
		return invalid("SCANNER_PAPERLESS_TOKEN_ENV")
	}
	p.ConfigFingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte("paperless-api9\x00"+p.BaseURL)))
	p.DestinationID = "paperless-" + p.ConfigFingerprint
	value, ok := lookup(name)
	if !ok || value == "" {
		return p, nil
	}
	if len(value) > 4096 || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r >= 127 }) >= 0 {
		return invalid("SCANNER_PAPERLESS_TOKEN_ENV")
	}
	p.Token = Secret(value)
	p.Configured = true
	return p, nil
}

// NewClient does no I/O; disabled/unconfigured settings construct no remote client.
func (p Paperless) NewClient() (*paperless.Client, error) {
	if !p.Enabled || !p.Configured {
		return nil, nil
	}
	return paperless.New(paperless.Options{BaseURL: p.BaseURL, Token: string(p.Token), DestinationID: p.DestinationID, ConfigFingerprint: p.ConfigFingerprint, AllowHTTP: p.AllowHTTP})
}
