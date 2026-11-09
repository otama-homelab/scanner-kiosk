// Package paperless implements only the pinned Paperless-ngx v3.4.2 API 9 contract.
package paperless

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrUnavailable = errors.New("paperless unavailable")

const MaxPDFBytes int64 = 128 << 20
const maxResponseBytes = 64 << 10
const APIVersion = "application/json; version=9"

type Options struct {
	BaseURL, Token, DestinationID, ConfigFingerprint string
	AllowHTTP                                        bool
}
type Client struct {
	base                              *url.URL
	token, destinationID, fingerprint string
	client                            *http.Client
}
type Task struct {
	TaskID          string `json:"task_id"`
	Filename        string `json:"task_file_name"`
	Status          string `json:"status"`
	RelatedDocument *int64 `json:"related_document"`
}

type HealthStatus string

const (
	HealthHealthy      HealthStatus = "healthy"
	HealthUnauthorized HealthStatus = "unauthorized"
	HealthForbidden    HealthStatus = "forbidden"
	HealthTimeout      HealthStatus = "timeout"
	HealthRedirect     HealthStatus = "redirect"
	HealthIncompatible HealthStatus = "incompatible"
	HealthUnavailable  HealthStatus = "unavailable"
)

// HealthResult is only an observation; it carries no delivery or upload evidence.
type HealthResult struct {
	Status    HealthStatus `json:"status"`
	CheckedAt time.Time    `json:"checked_at"`
}

// Health reads only the API route map, discarding all remote URLs and content.
func (c *Client) Health(ctx context.Context) HealthResult {
	result := HealthResult{Status: HealthUnavailable, CheckedAt: time.Now().UTC()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(""), nil)
	if err != nil {
		return result
	}
	response, err := c.request(request)
	if err != nil {
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			result.Status = HealthTimeout
		}
		return result
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized:
		result.Status = HealthUnauthorized
	case response.StatusCode == http.StatusForbidden:
		result.Status = HealthForbidden
	case response.StatusCode >= 300 && response.StatusCode < 400:
		result.Status = HealthRedirect
	case response.StatusCode == http.StatusOK:
		result.Status = HealthIncompatible
		// v3.4.2 middleware reports the highest supported API, not the negotiated 9.
		if response.Header.Get("X-Version") != "3.4.2" || response.Header.Get("X-Api-Version") != "10" {
			return result
		}
		var routes struct {
			Documents string `json:"documents"`
			Tasks     string `json:"tasks"`
		}
		err = readJSON(response, &routes)
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			result.Status = HealthTimeout
		} else if err == nil && routes.Documents != "" && routes.Tasks != "" {
			result.Status = HealthHealthy
		}
	}
	return result
}

// CanonicalBaseURL is shared by operator configuration and the adapter boundary.
func CanonicalBaseURL(raw string, allowHTTP bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || len(raw) > 2048 || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "\x00\r\n\t\\?#") || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 || strings.HasSuffix(u.Host, ":") || u.Opaque != "" || u.RawPath != "" || u.Path != "" && u.Path != "/" && path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return "", ErrUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && allowHTTP && (strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback())) {
		return "", ErrUnavailable
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return "", ErrUnavailable
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Port() == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String(), nil
}

func New(o Options) (*Client, error) {
	base, err := CanonicalBaseURL(o.BaseURL, o.AllowHTTP)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(base)
	if len(o.Token) < 1 || len(o.Token) > 4096 || strings.ContainsAny(o.Token, "\x00\r\n\t ") || len(o.DestinationID) < 1 || len(o.DestinationID) > 128 {
		return nil, ErrUnavailable
	}
	fingerprint, err := hex.DecodeString(o.ConfigFingerprint)
	if err != nil || len(fingerprint) != 32 || strings.ToLower(o.ConfigFingerprint) != o.ConfigFingerprint {
		return nil, ErrUnavailable
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 2, MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 16 << 10}
	return &Client{base: u, token: o.Token, destinationID: o.DestinationID, fingerprint: o.ConfigFingerprint, client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) DestinationID() string     { return c.destinationID }
func (c *Client) ConfigFingerprint() string { return c.fingerprint }
func (c *Client) Close()                    { c.client.CloseIdleConnections() }
func (c *Client) endpoint(suffix string) string {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/" + suffix
	return u.String()
}

// Upload deliberately has no replay body or idempotency headers, including on reused connections.
func (c *Client) Upload(ctx context.Context, filename string, size int64, pdf io.Reader) (string, error) {
	if !validFilename(filename) || size < 1 || size > MaxPDFBytes || pdf == nil {
		return "", ErrUnavailable
	}
	var envelope bytes.Buffer
	writer := multipart.NewWriter(&envelope)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="document"; filename="`+filename+`"`)
	header.Set("Content-Type", "application/pdf")
	if _, err := writer.CreatePart(header); err != nil {
		return "", ErrUnavailable
	}
	prefixLen := envelope.Len()
	if err := writer.Close(); err != nil {
		return "", ErrUnavailable
	}
	prefix := append([]byte(nil), envelope.Bytes()[:prefixLen]...)
	suffix := append([]byte(nil), envelope.Bytes()[prefixLen:]...)
	body := io.NopCloser(io.MultiReader(bytes.NewReader(prefix), io.LimitReader(pdf, size), bytes.NewReader(suffix)))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("documents/post_document/"), body)
	if err != nil {
		return "", ErrUnavailable
	}
	request.ContentLength = int64(len(prefix)) + size + int64(len(suffix))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	var taskID string
	if err = c.do(request, &taskID); err != nil || !ValidTaskID(taskID) {
		return "", ErrUnavailable
	}
	return taskID, nil
}

func (c *Client) Task(ctx context.Context, taskID, filename string) (Task, error) {
	if !ValidTaskID(taskID) || !validFilename(filename) {
		return Task{}, ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("tasks/")+"?task_id="+url.QueryEscape(taskID), nil)
	if err != nil {
		return Task{}, ErrUnavailable
	}
	var tasks []Task
	if err = c.do(request, &tasks); err != nil || len(tasks) != 1 {
		return Task{}, ErrUnavailable
	}
	task := tasks[0]
	if task.TaskID != taskID || task.Filename != filename {
		return Task{}, ErrUnavailable
	}
	switch task.Status {
	case "PENDING", "STARTED", "FAILURE", "REVOKED":
	case "SUCCESS":
		if task.RelatedDocument == nil || *task.RelatedDocument < 1 {
			return Task{}, ErrUnavailable
		}
	default:
		return Task{}, ErrUnavailable
	}
	if task.RelatedDocument != nil && *task.RelatedDocument < 1 {
		return Task{}, ErrUnavailable
	}
	return task, nil
}
func (c *Client) Metadata(ctx context.Context, documentID int64, expected [32]byte) error {
	if documentID < 1 {
		return ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("documents/"+strconv.FormatInt(documentID, 10)+"/metadata/"), nil)
	if err != nil {
		return ErrUnavailable
	}
	var metadata struct {
		Checksum string `json:"original_checksum"`
	}
	if err = c.do(request, &metadata); err != nil {
		return ErrUnavailable
	}
	digest, err := hex.DecodeString(metadata.Checksum)
	if err != nil || len(digest) != sha256.Size || !bytes.Equal(digest, expected[:]) {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) do(request *http.Request, out any) error {
	response, err := c.request(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if readJSON(response, out) != nil {
		return ErrUnavailable
	}
	return nil
}

func (c *Client) request(request *http.Request) (*http.Response, error) {
	request.Header.Set("Accept", APIVersion)
	request.Header.Set("Authorization", "Token "+c.token)
	return c.client.Do(request)
}

func readJSON(response *http.Response, out any) error {
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err = decoder.Decode(out); err != nil {
		return ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrUnavailable
	}
	return nil
}
func ValidTaskID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != "00000000-0000-0000-0000-000000000000"
}
func validFilename(s string) bool {
	if !strings.HasPrefix(s, "delivery_") || !strings.HasSuffix(s, ".pdf") || len(s) != 61 {
		return false
	}
	b, err := hex.DecodeString(s[9:57])
	return err == nil && len(b) == 24 && strings.ToLower(s) == s
}
