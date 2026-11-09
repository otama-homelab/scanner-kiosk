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

func New(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || u == nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(o.BaseURL, "\x00\r\n\t\\?#") || u.Opaque != "" || u.RawPath != "" || u.Path != "" && path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return nil, ErrUnavailable
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && o.AllowHTTP) {
		return nil, ErrUnavailable
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return nil, ErrUnavailable
		}
	}
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
	request.Header.Set("Accept", APIVersion)
	request.Header.Set("Authorization", "Token "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
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
