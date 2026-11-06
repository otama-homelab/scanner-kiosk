package tesseractapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
	"unicode/utf8"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

type ClientOptions struct {
	Settings config.Tesseract
	APIKey   config.Secret
	Limits   Limits
}
type Client struct {
	options       ClientOptions
	http          *http.Client
	caps          ocrplugin.Capabilities
	registry      *ocrplugin.Registry
	nativeVersion string
}

// NewClient is called only by lazy optional-OCR initialization, never by API startup.
// Failed discovery is retried by the existing catalog initialization loop.
func NewClient(ctx context.Context, options ClientOptions) (*Client, error) {
	if !options.Limits.valid() || !validKey(string(options.APIKey)) {
		return nil, errService
	}
	settings := options.Settings
	validated, err := config.LoadTesseract(func(key string) (string, bool) {
		values := map[string]string{"SCANNER_OCR_TESSERACT_ENDPOINT": settings.Endpoint, "SCANNER_OCR_TESSERACT_DESTINATION": settings.ProcessingDestination, "SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP": "false"}
		if settings.AllowInsecureHTTP {
			values["SCANNER_OCR_TESSERACT_ALLOW_INSECURE_HTTP"] = "true"
		}
		v, ok := values[key]
		return v, ok
	})
	if err != nil || settings.Endpoint == "" {
		return nil, errService
	}
	options.Settings = validated.(config.Tesseract)
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: options.Limits.Timeout, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true}
	c := &Client{options: options, http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	ctx, cancel := context.WithTimeout(ctx, min(options.Limits.Timeout, 5*time.Second))
	defer cancel()
	req, err := c.request(ctx, "GET", "/v1/capabilities", nil)
	if err != nil {
		c.Close()
		return nil, errService
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.Close()
		return nil, errService
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || res.StatusCode != 200 || len(raw) > 64<<10 || decode(raw, &c.caps) != nil || !nativeCapabilities(c.caps) {
		c.Close()
		return nil, errService
	}
	c.caps.ExternalProcessing = true
	m := &c.caps.Models[0]
	c.nativeVersion = m.Version
	// A queued selection must not move to a different endpoint or disclosure label.
	fingerprint := sha256.Sum256([]byte("tesseract-api-v1\x00" + c.nativeVersion + "\x00" + options.Settings.Endpoint + "\x00" + options.Settings.ProcessingDestination))
	m.Version = "sha256-" + hex.EncodeToString(fingerprint[:])
	m.MaxPages = min(m.MaxPages, options.Limits.MaxPages)
	m.MaxPagePixels = min(m.MaxPagePixels, options.Limits.MaxPagePixels)
	m.ProcessingDestination = options.Settings.ProcessingDestination
	c.registry, err = ocrplugin.New(ctx, c)
	if err != nil {
		c.Close()
		return nil, errService
	}
	return c, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	return clone(c.caps), nil
}
func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, c.options.Settings.Endpoint+path, body)
	if err != nil {
		return nil, errService
	}
	r.Header.Set("Authorization", "Bearer "+string(c.options.APIKey))
	return r, nil
}
func (c *Client) Run(ctx context.Context, r ocrplugin.Request, a ocrplugin.Access) (ocrplugin.Result, error) {
	if _, err := c.registry.Select("tesseract", r); err != nil {
		return ocrplugin.Result{}, errService
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.Limits.Timeout)
	defer cancel()
	result := ocrplugin.Result{ModelVersion: r.ModelVersion}
	for _, page := range r.Pages {
		meta, err := json.Marshal(wireRequest{ModelVersion: c.nativeVersion, Languages: r.Languages, QualityPreset: r.QualityPreset, Output: r.Output, Width: page.Width, Height: page.Height, DPI: page.DPI})
		if err != nil || len(meta) > 8192 {
			return ocrplugin.Result{}, errService
		}
		in, err := a.OpenInput(ctx, page.ArtifactID)
		if err != nil {
			return ocrplugin.Result{}, errService
		}
		body := struct {
			io.Reader
			io.Closer
		}{io.LimitReader(in, c.options.Limits.MaxInputBytes+1), in}
		req, err := c.request(ctx, "POST", "/v1/recognize", body)
		if err != nil {
			in.Close()
			return ocrplugin.Result{}, errService
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-OCR-Request", string(meta))
		res, err := c.http.Do(req)
		_ = in.Close()
		if err != nil {
			return ocrplugin.Result{}, errService
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, c.options.Limits.MaxResultBytes+1))
		closeErr := res.Body.Close()
		if readErr != nil || closeErr != nil || res.StatusCode != 200 || res.Header.Get("X-OCR-Model-Version") != c.nativeVersion || res.Header.Get("Content-Type") != contentType(r.Output) || int64(len(data)) > c.options.Limits.MaxResultBytes || (r.Output == ocrplugin.Text && !utf8.Valid(data)) || (r.Output == ocrplugin.PDFTextLayer && !bytes.HasPrefix(data, []byte("%PDF-"))) || ctx.Err() != nil {
			return ocrplugin.Result{}, errService
		}
		out, err := a.CreateOutput(ctx, r.Output)
		if err != nil {
			return ocrplugin.Result{}, errService
		}
		_, writeErr := out.Write(data)
		closeErr = out.Close()
		if writeErr != nil || closeErr != nil || ctx.Err() != nil {
			return ocrplugin.Result{}, errService
		}
		result.Pages = append(result.Pages, ocrplugin.PageResult{PageID: page.ID, ArtifactID: out.Reference(), Kind: r.Output})
	}
	return result, nil
}
