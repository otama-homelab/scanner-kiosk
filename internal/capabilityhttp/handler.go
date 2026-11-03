// Package capabilityhttp exposes worker metadata without provider clients or device endpoints.
package capabilityhttp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/scannerreadiness"
	"scanner-kiosk/internal/websession"
	"scanner-kiosk/pkg/ocrplugin"
)

type Scanner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Limits struct {
	RetentionSeconds int64 `json:"retention_seconds"`
	MaxDocuments     int64 `json:"max_documents"`
	UserMaxBytes     int64 `json:"user_max_bytes"`
	JobMaxPages      int   `json:"job_max_pages"`
	JobMaxBytes      int64 `json:"job_max_bytes"`
	MaxPagePixels    int64 `json:"-"`
}
type scannerView struct {
	Scanner
	CaptureOptions []scanner.Capture  `json:"capture_options"`
	State          string             `json:"state"`
	Sources        []string           `json:"sources"`
	Sizes          []string           `json:"sizes"`
	Colors         []string           `json:"colors"`
	DPIOptions     []int              `json:"dpi_options"`
	DefaultCapture *documents.Capture `json:"default_capture"`
}
type modelView struct {
	ProcessingDestination string                 `json:"processing_destination,omitempty"`
	ID                    string                 `json:"id"`
	Version               string                 `json:"version"`
	Languages             []string               `json:"languages"`
	QualityPresets        []string               `json:"quality_presets"`
	Name                  string                 `json:"name"`
	NativeOutputs         []ocrplugin.OutputKind `json:"native_outputs"`
	Outputs               []string               `json:"outputs"`
	MaxPages              int                    `json:"max_pages"`
	MaxPagePixels         int64                  `json:"max_page_pixels"`
	Available             bool                   `json:"available"`
	DefaultQualityPreset  *string                `json:"default_quality_preset"`
	UnavailableReason     *string                `json:"unavailable_reason"`
}
type pluginView struct {
	Name               string      `json:"name"`
	ID                 string      `json:"id"`
	Version            string      `json:"version"`
	Models             []modelView `json:"models"`
	Outputs            []string    `json:"outputs"`
	ExternalProcessing bool        `json:"external_processing"`
}
type defaultsView struct {
	Capture documents.Capture   `json:"capture"`
	OCR     documents.RenderOCR `json:"ocr"`
}
type response struct {
	Revision   string        `json:"revision"`
	Scanners   []scannerView `json:"scanners"`
	OCRPlugins []pluginView  `json:"ocr_plugins"`
	Defaults   defaultsView  `json:"defaults"`
	Limits     Limits        `json:"limits"`
}

type scannerResponse struct {
	Revision       string            `json:"revision"`
	Scanners       []scannerView     `json:"scanners"`
	DefaultCapture documents.Capture `json:"default_capture"`
	Limits         Limits            `json:"limits"`
}

// New accepts sanitized labels and independently validated scanner metadata.
// Verified tuples alone do not establish readiness; status uses a separate port.
// OCR is initially off; this endpoint never guesses a model or language selection.
func New(source renderadmission.CatalogSource, scannerSource scannermetadata.Source, readinessSource scannerreadiness.Source, sessions *websession.Handler, scanners []Scanner, limits Limits, now func() time.Time) http.Handler {
	configured := append([]Scanner(nil), scanners...)
	mux := http.NewServeMux()
	handle := func(withOCR bool) http.Handler {
		return sessions.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, principal websession.Principal) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			fail := func(status int, code string) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": http.StatusText(status)})
			}
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				fail(400, "invalid_request")
				return
			}
			snapshot := renderadmission.CatalogSnapshot{}
			if withOCR {
				value, err := source.ReadOCRCatalog(r.Context())
				if err != nil || renderadmission.ValidateCatalogSnapshot(value, now()) != nil {
					fail(503, "provider_unavailable")
					return
				}
				snapshot = value
			}

			capture := documents.Capture{Size: "a4", Color: "color", DPI: 300}
			out := response{Scanners: []scannerView{}, OCRPlugins: []pluginView{}, Defaults: defaultsView{Capture: capture}, Limits: limits}
			metadata := scannermetadata.Snapshot{}
			status := scannerreadiness.Snapshot{}
			if readinessSource != nil {
				ids := make([]string, 0, len(configured))
				for _, device := range configured {
					ids = append(ids, device.ID)
				}
				if value, err := readinessSource.ReadScannerReadiness(r.Context(), principal.ID, ids, limits.MaxPagePixels); err == nil && scannermetadata.Validate(value.Metadata, now()) == nil {
					status = value
					metadata = value.Metadata
				}
			}
			if metadata.Devices == nil && scannerSource != nil {
				if value, err := scannerSource.ReadScannerMetadata(r.Context()); err == nil && scannermetadata.Validate(value, now()) == nil {
					metadata = value
				}
			}

			for _, device := range configured {
				view := scannerView{Scanner: device, State: "unknown", Sources: []string{}, Sizes: []string{}, Colors: []string{}, DPIOptions: []int{}, CaptureOptions: []scanner.Capture{}}
				for _, entry := range metadata.Devices {
					if entry.ID == device.ID {
						for _, profile := range entry.Profiles {
							if int64(profile.Width) > limits.MaxPagePixels/int64(profile.Height) {
								continue
							}
							view.CaptureOptions = append(view.CaptureOptions, profile.Capture)
							if profile.Size == capture.Size && profile.Color == capture.Color && profile.DPI == capture.DPI {
								view.DefaultCapture = &capture
							}
							if !contains(view.Sizes, profile.Size) {
								view.Sizes = append(view.Sizes, profile.Size)
							}
							if !contains(view.Colors, profile.Color) {
								view.Colors = append(view.Colors, profile.Color)
							}
							if !contains(view.DPIOptions, profile.DPI) {
								view.DPIOptions = append(view.DPIOptions, profile.DPI)
							}
						}
					}
				}
				if len(view.CaptureOptions) > 0 {
					for _, state := range status.Devices {
						if state.ID == device.ID && state.ExpiresAtMS > now().UnixMilli() && status.ExpiresAtMS > now().UnixMilli() {
							switch state.State {
							case scannerreadiness.Idle, scannerreadiness.Busy, scannerreadiness.Unavailable:
								view.State = string(state.State)
							}
						}
					}
					view.Sources = []string{"flatbed"}
				}
				out.Scanners = append(out.Scanners, view)
			}
			for _, provider := range snapshot.Providers {
				plugin := pluginView{Name: provider.Name, ID: provider.ID, Version: provider.Version, Models: []modelView{}, Outputs: []string{"searchable_pdf"}, ExternalProcessing: provider.ExternalProcessing}
				for _, model := range provider.Models {
					compatible := false
					for _, kind := range model.Outputs {
						if kind == ocrplugin.SearchablePDF || kind == ocrplugin.PDFTextLayer {
							compatible = true
						}
					}
					if !compatible {
						continue
					}
					var defaultPreset *string
					for _, entry := range snapshot.Defaults {
						if entry.ProviderID == provider.ID && entry.ModelID == model.ID {
							value := entry.QualityPreset
							defaultPreset = &value
						}
					}
					if preferred := snapshot.Preferred; preferred != nil && preferred.ProviderID == provider.ID && preferred.ModelID == model.ID {
						out.Defaults.OCR = documents.RenderOCR{ProviderID: preferred.ProviderID, ModelID: preferred.ModelID, Languages: append([]string(nil), preferred.Languages...), QualityPreset: preferred.QualityPreset}
					}
					presets := append([]string{}, model.QualityPresets...)
					plugin.Models = append(plugin.Models, modelView{ProcessingDestination: model.ProcessingDestination, ID: model.ID, Name: model.Name, Version: model.Version, Languages: append([]string{}, model.Languages...), QualityPresets: presets, NativeOutputs: append([]ocrplugin.OutputKind{}, model.Outputs...), Outputs: []string{"searchable_pdf"}, MaxPages: min(model.MaxPages, limits.JobMaxPages), MaxPagePixels: min(model.MaxPagePixels, limits.MaxPagePixels), Available: true, DefaultQualityPreset: defaultPreset})
				}
				if len(plugin.Models) > 0 {
					out.OCRPlugins = append(out.OCRPlugins, plugin)
				}
			}
			if r.Context().Err() != nil || withOCR && now().UnixMilli() >= snapshot.ExpiresAtMS {
				fail(503, "provider_unavailable")
				return
			}
			current := now().UnixMilli()
			for i := range out.Scanners {
				view := &out.Scanners[i]
				if metadata.ExpiresAtMS <= current {
					view.CaptureOptions = []scanner.Capture{}
					view.Sources = []string{}
					view.Sizes = []string{}
					view.Colors = []string{}
					view.DPIOptions = []int{}
					view.DefaultCapture = nil
					view.State = "unknown"
					continue
				}
				if status.ExpiresAtMS <= current {
					view.State = "unknown"
					continue
				}
				for _, state := range status.Devices {
					if state.ID == view.ID && state.ExpiresAtMS <= current {
						view.State = "unknown"
					}
				}
			}

			// A stable content revision excludes private worker identity and lease times.
			var projection any = &out
			if !withOCR {
				projection = &scannerResponse{Scanners: out.Scanners, DefaultCapture: capture, Limits: limits}
			}
			encoded, err := json.Marshal(projection)
			if err != nil {
				fail(503, "provider_unavailable")
				return
			}
			digest := sha256.Sum256(encoded)
			revision := hex.EncodeToString(digest[:])
			switch value := projection.(type) {
			case *response:
				value.Revision = revision
			case *scannerResponse:
				value.Revision = revision
			}
			_ = json.NewEncoder(w).Encode(projection)
		})
	}
	mux.Handle("GET /api/capabilities", handle(true))
	mux.Handle("GET /api/scanners", handle(false))
	return mux
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
