//go:build linux

package workerruntime

import (
	"context"
	"os"
	"path/filepath"
	"scanner-kiosk/internal/renderadmission"
	"sync"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	ocrruntime "scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/internal/renderjob"
	"scanner-kiosk/internal/thumbnail"
	"scanner-kiosk/pkg/ocrplugin"
)

// Optional OCR initialization is shared by background capability publication and
// execution. Initialization failure does not block plain PDF/image processing.
type renderExecutionRecords interface {
	renderjob.Records
	renderjob.PulseRecords
	renderjob.StopRecords
	renderjob.OCRResultRecords
	PublishExportWithThumbnails(context.Context, string, string, string, int64, documents.Export, []documents.ThumbnailRecord) error
}

func (*processingRecords) OCRTextLayerFont(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ocrruntime.ReadTextLayerFont(os.Getenv("SCANNER_OCR_LLM_TEXT_LAYER_FONT"))
}

type processingRecords struct {
	renderExecutionRecords
	config    config.Config
	providers *ocrruntime.Runtime
	mu        sync.Mutex
}

func (*processingRecords) PDFRasterCommand() renderjob.PDFRasterCommand {
	path := os.Getenv("SCANNER_PDF_RASTER_PATH")
	if path == "" {
		path = "/usr/bin/pdftoppm"
	}
	return renderjob.PDFRasterCommand{Path: path}
}

func (r *processingRecords) initOCR(ctx context.Context) error {
	if r.providers == nil {
		root := filepath.Join(r.config.DocumentDir, ".native")
		if err := os.Mkdir(root, 0700); err != nil && !os.IsExist(err) {
			return ErrRoot
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return ErrRoot
		}
		r.providers, err = ocrruntime.NewWorker(ctx, r.config, os.LookupEnv, root)
		if err != nil {
			return err
		}
	}
	return nil
}
func (r *processingRecords) SelectOCR(ctx context.Context, id string, request ocrplugin.Request) (ocrplugin.Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.initOCR(ctx); err != nil {
		return nil, err
	}
	return r.providers.Registry.Select(id, request)
}
func (r *processingRecords) catalog(ctx context.Context) (renderadmission.CatalogSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.initOCR(ctx); err != nil {
		return renderadmission.CatalogSnapshot{}, err
	}
	out := renderadmission.CatalogSnapshot{Providers: r.providers.Registry.Capabilities()}
	defaults := r.providers.Defaults()
	if defaults.PluginID != "" {
		out.Preferred = &renderadmission.CatalogSelection{ProviderID: defaults.PluginID, ModelID: defaults.ModelID, QualityPreset: defaults.QualityPreset, Languages: append([]string(nil), defaults.Languages...)}
		out.Defaults = []renderadmission.ModelDefault{{ProviderID: defaults.PluginID, ModelID: defaults.ModelID, QualityPreset: defaults.QualityPreset}}
	}
	return out, nil
}
func (r *processingRecords) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers != nil {
		r.providers.Close()
	}
}

func (*processingRecords) ThumbnailOptions() thumbnail.Options {
	return thumbnail.Options{MaxEdge: 512, MaxOutputBytes: 1 << 20, Quality: 82}
}
