package tesseractapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/pkg/ocrplugin"
)

type HandlerOptions struct {
	Provider ocrplugin.Provider
	APIKey   config.Secret
	WorkRoot string
	Limits   Limits
}
type Handler struct {
	options  HandlerOptions
	registry *ocrplugin.Registry
	caps     ocrplugin.Capabilities
	key      [32]byte
	busy     chan struct{}
	poisoned atomic.Bool
	mux      *http.ServeMux
}

func NewHandler(options HandlerOptions) (*Handler, error) {
	if options.Provider == nil || !options.Limits.valid() || !validKey(string(options.APIKey)) || !filepath.IsAbs(options.WorkRoot) || filepath.Clean(options.WorkRoot) != options.WorkRoot {
		return nil, errService
	}
	info, err := os.Lstat(options.WorkRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errService
	}
	entries, err := os.ReadDir(options.WorkRoot)
	if err != nil || len(entries) != 0 {
		return nil, errService
	}
	registry, err := ocrplugin.New(context.Background(), options.Provider)
	if err != nil {
		return nil, errService
	}
	caps := registry.Capabilities()[0]
	if !nativeCapabilities(caps) {
		return nil, errService
	}
	h := &Handler{options: options, registry: registry, caps: caps, key: sha256.Sum256([]byte(options.APIKey)), busy: make(chan struct{}, 1), mux: http.NewServeMux()}
	h.options.APIKey = ""
	h.mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if h.poisoned.Load() {
			http.Error(w, "OCR service unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.caps)
	})
	h.mux.HandleFunc("POST /v1/recognize", h.recognize)
	return h, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "GET" && (r.URL.Path == "/health/ready" || r.URL.Path == "/health/live") {
		if h.poisoned.Load() {
			http.Error(w, "OCR service unavailable", 503)
		} else {
			w.WriteHeader(200)
		}
		return
	}
	values := r.Header.Values("Authorization")
	value := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hash := sha256.Sum256([]byte(value))
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || subtle.ConstantTimeCompare(hash[:], h.key[:]) != 1 {
		http.Error(w, "Unauthorized", 401)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Invalid OCR request", 422)
		return
	}
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) recognize(w http.ResponseWriter, r *http.Request) {
	fail := func(status int) { http.Error(w, "OCR request unavailable or invalid", status) }
	if h.poisoned.Load() {
		fail(503)
		return
	}
	select {
	case h.busy <- struct{}{}:
	default:
		fail(429)
		return
	}
	defer func() {
		if !h.poisoned.Load() {
			<-h.busy
		}
	}()
	if r.ContentLength > h.options.Limits.MaxInputBytes {
		fail(413)
		return
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Content-Encoding") != "" || len(r.Header.Values("X-OCR-Request")) != 1 {
		fail(422)
		return
	}
	meta := r.Header.Get("X-OCR-Request")
	var settings wireRequest
	if len(meta) > 8192 || decode([]byte(meta), &settings) != nil {
		fail(422)
		return
	}
	request := settings.request()
	if settings.ModelVersion != h.caps.Models[0].Version {
		fail(409)
		return
	}
	if _, err := h.registry.Select("tesseract", request); err != nil {
		fail(422)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.options.Limits.Timeout)
	defer cancel()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(h.options.Limits.Timeout))
	root, err := os.MkdirTemp(h.options.WorkRoot, "request-")
	if err != nil {
		fail(503)
		return
	}
	clean := true
	cleanup := func() error {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		dir, err := os.Open(h.options.WorkRoot)
		if err != nil {
			return err
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	defer func() {
		if clean && cleanup() != nil {
			h.poisoned.Store(true)
		}
	}()
	input := filepath.Join(root, "input")
	target, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fail(503)
		return
	}
	n, copyErr := io.Copy(target, io.LimitReader(r.Body, h.options.Limits.MaxInputBytes+1))
	closeErr := target.Close()
	if n > h.options.Limits.MaxInputBytes {
		fail(413)
		return
	}
	var pathErr *os.PathError
	if closeErr != nil || errors.As(copyErr, &pathErr) {
		fail(503)
		return
	}
	if copyErr != nil || closeErr != nil || ctx.Err() != nil {
		fail(422)
		return
	}
	source, err := os.Open(input)
	if err != nil {
		fail(503)
		return
	}
	valid := validImage(source, settings.Width, settings.Height, h.options.Limits.MaxPagePixels)
	closeErr = source.Close()
	if !valid || closeErr != nil {
		fail(422)
		return
	}
	if ctx.Err() != nil {
		fail(503)
		return
	}
	a := &fileAccess{root: root, limit: h.options.Limits.MaxResultBytes}
	result, err := h.options.Provider.Run(ctx, request, a)
	if a.output != nil && !a.closed {
		_ = a.output.File.Close()
	}
	if errors.Is(err, ocrplugin.ErrReconciliation) {
		clean = false
		h.poisoned.Store(true)
		fail(503)
		return
	}
	if err != nil || ctx.Err() != nil || !a.closed || result.ModelVersion != settings.ModelVersion || len(result.Pages) != 1 || result.Pages[0].PageID != "page" || result.Pages[0].ArtifactID != "result" || result.Pages[0].Kind != settings.Output {
		fail(503)
		return
	}
	f, err := os.OpenInRoot(root, "result")
	if err != nil {
		fail(503)
		return
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > h.options.Limits.MaxResultBytes {
		f.Close()
		fail(503)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, h.options.Limits.MaxResultBytes+1))
	closeErr = f.Close()
	if err != nil || closeErr != nil || int64(len(data)) > h.options.Limits.MaxResultBytes {
		fail(503)
		return
	}
	if settings.Output == ocrplugin.PositionedText {
		if _, err := positionedResult(data, request.Pages[0]); err != nil {
			fail(503)
			return
		}
	}
	// Complete local process stop and workspace cleanup before a success response.
	if err = cleanup(); err != nil {
		clean = false
		h.poisoned.Store(true)
		fail(503)
		return
	}
	clean = false
	w.Header().Set("Content-Type", contentType(settings.Output))
	w.Header().Set("X-OCR-Model-Version", result.ModelVersion)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// Only server-assigned paths are reachable. The native adapter receives no client paths.
type fileAccess struct {
	root            string
	limit           int64
	created, closed bool
	output          *fileOutput
}

func (a *fileAccess) OpenInput(ctx context.Context, id string) (io.ReadCloser, error) {
	if ctx.Err() != nil || id != "input" {
		return nil, errService
	}
	return os.Open(filepath.Join(a.root, "input"))
}
func (a *fileAccess) CreateOutput(ctx context.Context, kind ocrplugin.OutputKind) (ocrplugin.ArtifactWriter, error) {
	if ctx.Err() != nil || a.created || (kind != ocrplugin.Text && kind != ocrplugin.PDFTextLayer && kind != ocrplugin.PositionedText) {
		return nil, errService
	}
	f, err := os.OpenFile(filepath.Join(a.root, "result"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errService
	}
	a.created = true
	a.output = &fileOutput{File: f, access: a}
	return a.output, nil
}

type fileOutput struct {
	*os.File
	access *fileAccess
	bytes  int64
}

func (o *fileOutput) Reference() string { return "result" }
func (o *fileOutput) Write(raw []byte) (int, error) {
	if int64(len(raw)) > o.access.limit-o.bytes {
		return 0, errService
	}
	n, err := o.File.Write(raw)
	o.bytes += int64(n)
	return n, err
}
func (o *fileOutput) Close() error {
	syncErr := o.File.Sync()
	closeErr := o.File.Close()
	o.access.closed = syncErr == nil && closeErr == nil
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
