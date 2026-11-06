//go:build linux

package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/ocr/llm"
	"scanner-kiosk/internal/ocr/tesseract"
	"scanner-kiosk/internal/ocr/tesseractapi"
)

// NewWorker is the initial composition root. The API role must not call it or
// receive LLM credentials: API capability snapshots are a separate projection.
// workRoot must be a private directory on the temporary document volume.
func NewWorker(ctx context.Context, core config.Config, lookup config.Lookup, workRoot string) (*Runtime, error) {
	if core.Role != config.Worker {
		return nil, errors.New("worker configuration required")
	}
	settings, err := config.LoadOCR(lookup, map[string]config.PluginLoader{"tesseract": config.LoadTesseract, "llm": config.LoadLLM})
	if err != nil {
		return nil, err
	}
	limits := core.Limits
	durations := core.Durations
	maxPages := int(limits["SCANNER_JOB_MAX_PAGES"])
	maxPixels := limits["SCANNER_MAX_PAGE_PIXELS"]
	maxInput := limits["SCANNER_JOB_MAX_BYTES"]
	maxResult := limits["SCANNER_OCR_MAX_RESULT_BYTES"]
	factories := map[string]Factory{
		"tesseract": func(ctx context.Context, value any) (Instance, error) {
			cfg, ok := value.(config.Tesseract)
			if !ok {
				return Instance{}, errors.New("invalid Tesseract configuration")
			}
			if cfg.Endpoint != "" {
				key, _ := lookup("SCANNER_OCR_TESSERACT_API_KEY")
				provider, err := tesseractapi.NewClient(ctx, tesseractapi.ClientOptions{Settings: cfg, APIKey: config.Secret(key), Limits: tesseractapi.Limits{MaxInputBytes: maxInput, MaxResultBytes: min(maxResult, 64<<20), MaxPagePixels: maxPixels, MaxPages: maxPages, Timeout: durations["SCANNER_OCR_TIMEOUT_SECONDS"]}})
				if err != nil {
					return Instance{}, err
				}
				return Instance{Provider: provider, Close: provider.Close}, nil
			}
			provider, err := tesseract.New(tesseract.Options{Binary: cfg.Path, DataDir: cfg.DataDir, WorkRoot: workRoot, MaxInputBytes: maxInput, MaxResultBytes: maxResult, MaxPagePixels: maxPixels, MaxPages: maxPages, Timeout: durations["SCANNER_OCR_TIMEOUT_SECONDS"], StopGrace: durations["SCANNER_PROCESS_STOP_GRACE_SECONDS"]})
			return Instance{Provider: provider}, err
		},
		"llm": func(ctx context.Context, value any) (Instance, error) {
			cfg, ok := value.(config.LLM)
			if !ok {
				return Instance{}, errors.New("invalid LLM configuration")
			}
			credentials, err := config.ResolveLLMCredentials(cfg, lookup)
			if err != nil {
				return Instance{}, err
			}
			var fontData []byte
			if cfg.TextLayerFontPath != "" {
				fontData, err = readTextLayerFont(cfg.TextLayerFontPath)
				if err != nil {
					return Instance{}, err
				}
			}
			layerLimit := int64(0)
			if len(fontData) > 0 {
				layerLimit = maxResult
			}
			// The wire parser has a stricter 64MiB bound than whole-job storage. Span
			// count is an internal decoder safety ceiling, not a model quality setting.
			provider, err := llm.New(llm.Options{TextLayerFont: fontData, MaxTextLayerBytes: layerLimit, Settings: cfg, Credentials: credentials, MaxInputBytes: min(maxInput, 64<<20), MaxResponseBytes: min(maxResult, 64<<20), MaxPagePixels: maxPixels, MaxPages: maxPages, MaxSpans: 100000, MaxTextBytes: int(min(maxResult, 64<<20)), Timeout: durations["SCANNER_OCR_TIMEOUT_SECONDS"]})
			if err != nil {
				return Instance{}, err
			}
			return Instance{Provider: provider, Close: provider.Close}, nil
		},
	}
	return Build(ctx, settings, factories)
}

// Nonblocking open prevents a configured FIFO/device from hanging worker startup.
func readTextLayerFont(path string) ([]byte, error) {
	fail := func() ([]byte, error) { return nil, errors.New("LLM text-layer font unavailable or invalid") }
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fail()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 12 || info.Size() > 32<<20 {
		return fail()
	}
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil || len(data) < 12 || len(data) > 32<<20 {
		return fail()
	}
	return data, nil
}
