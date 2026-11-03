package oidcprovider

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
	"unicode"
)

type boundedTransport struct {
	base    *http.Transport
	mu      sync.RWMutex
	allowed map[string]string
	max     int64
}

func (t *boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.RLock()
	method := t.allowed[request.URL.String()]
	t.mu.RUnlock()
	if request.Method != method || method == "" {
		return nil, ErrUnavailable
	}
	copy := request.Clone(request.Context())
	copy.Header.Set("Accept-Encoding", "identity")
	if copy.Method == http.MethodGet {
		copy.Header.Del("Authorization")
		copy.Header.Del("Cookie")
	}
	response, err := t.base.RoundTrip(copy)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.ContentLength > t.max || (response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, t.max+1))
	if err != nil || int64(len(body)) > t.max {
		return nil, ErrUnavailable
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}
func (t *boundedTransport) allow(uri, method string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.allowed[uri] = method
}
func newClient(timeout time.Duration, maxBytes int64, roots *x509.CertPool) (*http.Client, *boundedTransport) {
	if roots != nil {
		roots = roots.Clone()
	}
	transport := &boundedTransport{base: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: timeout}).DialContext, DisableKeepAlives: true, DisableCompression: true, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, MaxResponseHeaderBytes: 16 << 10}, allowed: map[string]string{}, max: maxBytes}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}, transport
}
func secureURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 4096 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
func secretString(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
