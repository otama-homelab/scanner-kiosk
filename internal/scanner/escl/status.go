// Package escl implements bounded read-only scanner status observation. Capture
// remains in the upstream SANE backend; this package never creates scan jobs.
package escl

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrInvalid     = errors.New("invalid scanner status configuration")
	ErrUnavailable = errors.New("scanner status unavailable or not idle")
)

type Device struct{ ID, Endpoint string }
type Options struct {
	Timeout          time.Duration
	MaxResponseBytes int64
}
type Observer struct {
	mu       sync.RWMutex
	devices  map[string]string
	client   *http.Client
	maxBytes int64
}

// New pins administrator-registered endpoints. Browser URLs, proxies, cookies,
// redirects, implicit retries, TLS bypass and inherited credentials are absent.
// A status response never fences another scanner writer or proves local stop.
func New(devices []Device, options Options) (*Observer, error) {
	if len(devices) > 32 || options.Timeout < time.Millisecond || options.Timeout > 30*time.Second || options.MaxResponseBytes < 1024 || options.MaxResponseBytes > 1<<20 {
		return nil, ErrInvalid
	}
	endpoints := make(map[string]string, len(devices))
	for _, device := range devices {
		if !opaque(device.ID) || endpoints[device.ID] != "" || !endpoint(device.Endpoint) {
			return nil, ErrInvalid
		}
		endpoints[device.ID] = strings.TrimRight(device.Endpoint, "/") + "/ScannerStatus"
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: options.Timeout}).DialContext, DisableKeepAlives: true, DisableCompression: true, TLSHandshakeTimeout: options.Timeout, ResponseHeaderTimeout: options.Timeout, MaxResponseHeaderBytes: 16 << 10}
	client := &http.Client{Transport: transport, Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}
	return &Observer{devices: endpoints, client: client, maxBytes: options.MaxResponseBytes}, nil
}

// ObserveIdle makes one fresh GET only after a known local stop. Unknown,
// malformed, busy or contradictory device/job states return no idle timestamp.
// Successful observations are bound to the requested registered device ID and
// this call's time; callers must still hold the exact lease and exclusive writer.
func (o *Observer) ObserveIdle(ctx context.Context, deviceID string, stoppedAtMS int64) (int64, error) {
	endpoint, ok := o.lookup(deviceID)
	began := time.Now().UnixMilli()
	if !ok || stoppedAtMS <= 0 || stoppedAtMS > began {
		return 0, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, ErrUnavailable
	}
	request.Header.Set("Accept", "text/xml, application/xml")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache, no-store")
	request.Header.Set("Pragma", "no-cache")
	response, err := o.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > o.maxBytes || (response.Header.Get("Age") != "" && response.Header.Get("Age") != "0") || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return 0, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, o.maxBytes+1))
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err != nil || int64(len(data)) > o.maxBytes || !idleStatus(data) {
		return 0, ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	observed := time.Now().UnixMilli()
	if observed < began || observed < stoppedAtMS {
		return 0, ErrUnavailable
	}
	return observed, nil
}

func opaque(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func endpoint(raw string) bool {
	if len(raw) > 4096 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (o *Observer) lookup(id string) (string, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	endpoint, ok := o.devices[id]
	return endpoint, ok
}

// Register publishes a validated endpoint once; existing identity bindings never move.
func (o *Observer) Register(device Device) error {
	if !opaque(device.ID) || !endpoint(device.Endpoint) {
		return ErrInvalid
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.devices) >= 32 || o.devices[device.ID] != "" {
		return ErrInvalid
	}
	o.devices[device.ID] = strings.TrimRight(device.Endpoint, "/") + "/ScannerStatus"
	return nil
}
