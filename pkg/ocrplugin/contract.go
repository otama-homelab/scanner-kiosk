// Package ocrplugin defines an engine-independent OCR boundary.
// Document ownership, quotas, retention and atomic publication stay with the application.
package ocrplugin

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const APIVersion = 1

type OutputKind string

const (
	Text           OutputKind = "text"
	PositionedText OutputKind = "positioned_text"
	SearchablePDF  OutputKind = "searchable_pdf"
	PDFTextLayer   OutputKind = "pdf_text_layer"
)

type Model struct {
	ProcessingDestination string       `json:"processing_destination,omitempty"`
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	Version               string       `json:"version"`
	Languages             []string     `json:"languages"`
	QualityPresets        []string     `json:"quality_presets"`
	Outputs               []OutputKind `json:"outputs"`
	MaxPages              int          `json:"max_pages"`
	MaxPagePixels         int64        `json:"max_page_pixels"`
}
type Capabilities struct {
	APIVersion         int     `json:"api_version"`
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Version            string  `json:"version"`
	ExternalProcessing bool    `json:"external_processing"`
	Models             []Model `json:"models"`
}
type InputPage struct {
	ID         string
	ArtifactID string
	Width      int
	Height     int
	DPI        int
}
type Request struct {
	JobID         string
	InputRevision int64
	ModelID       string
	ModelVersion  string
	Languages     []string
	QualityPreset string
	Output        OutputKind
	Pages         []InputPage
}

// Text geometry uses pixels, with origin at the top-left of the supplied oriented bitmap.
// It is an artifact payload: never put it in session/job metadata or logs.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Span struct {
	Text       string   `json:"text"`
	Box        Box      `json:"box"`
	Confidence *float64 `json:"confidence,omitempty"`
}
type PositionedPage struct {
	PageID string `json:"page_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Spans  []Span `json:"spans"`
}
type PageResult struct {
	PageID     string
	ArtifactID string
	Kind       OutputKind
}
type Result struct {
	ModelVersion string
	Pages        []PageResult
}

// Access is constructed for one job. It must reject unlisted input IDs, bound every
// output write, assign opaque output IDs, and create only inside the job workspace.
type Access interface {
	OpenInput(context.Context, string) (io.ReadCloser, error)
	CreateOutput(context.Context, OutputKind) (ArtifactWriter, error)
}
type ArtifactWriter interface {
	io.WriteCloser
	Reference() string
}

// Capabilities returns installed/configured metadata, without requiring a live remote endpoint.
// Runtime health is separate so an optional OCR outage cannot disable basic scanning.
type Provider interface {
	Capabilities(context.Context) (Capabilities, error)
	Run(context.Context, Request, Access) (Result, error)
}
type entry struct {
	provider     Provider
	capabilities Capabilities
}
type Registry struct {
	entries map[string]entry
	order   []string
}

// New snapshots capabilities once. Reconfiguration constructs a new registry;
// an active job keeps its selected plugin/model/configuration revision.
func New(ctx context.Context, providers ...Provider) (*Registry, error) {
	r := &Registry{entries: map[string]entry{}}
	for _, provider := range providers {
		if provider == nil {
			return nil, errors.New("nil OCR provider")
		}
		c, err := provider.Capabilities(ctx)
		if err != nil {
			return nil, errors.New("OCR capability discovery failed")
		}
		if err = validateCapabilities(c); err != nil {
			return nil, err
		}
		if _, ok := r.entries[c.ID]; ok {
			return nil, errors.New("duplicate OCR provider")
		}
		r.entries[c.ID] = entry{provider: provider, capabilities: clone(c)}
		r.order = append(r.order, c.ID)
	}
	return r, nil
}
func (r *Registry) Capabilities() []Capabilities {
	out := make([]Capabilities, 0, len(r.entries))
	for _, id := range r.order {
		out = append(out, clone(r.entries[id].capabilities))
	}
	return out
}

// Select rejects unsupported input; it never changes model, language, preset or output.
// The caller must still intersect model limits with the job's application limits.
func (r *Registry) Select(providerID string, request Request) (Provider, error) {
	entry, ok := r.entries[providerID]
	if !ok {
		return nil, errors.New("OCR provider unavailable")
	}
	var model *Model
	for i := range entry.capabilities.Models {
		if entry.capabilities.Models[i].ID == request.ModelID {
			model = &entry.capabilities.Models[i]
			break
		}
	}
	if model == nil || model.Version != request.ModelVersion {
		return nil, errors.New("OCR model revision unavailable")
	}
	if !nonempty(request.JobID) || request.InputRevision < 1 || len(request.Languages) == 0 || len(request.Pages) == 0 || len(request.Pages) > model.MaxPages {
		return nil, errors.New("invalid OCR request")
	}
	languages := map[string]bool{}
	for _, language := range request.Languages {
		if languages[language] || !contains(model.Languages, language) {
			return nil, errors.New("unsupported OCR languages")
		}
		languages[language] = true
	}
	if (len(model.QualityPresets) == 0 && request.QualityPreset != "") || (len(model.QualityPresets) > 0 && !contains(model.QualityPresets, request.QualityPreset)) {
		return nil, errors.New("unsupported OCR preset")
	}
	supported := false
	for _, kind := range model.Outputs {
		if kind == request.Output {
			supported = true
		}
	}
	if !supported {
		return nil, errors.New("unsupported OCR output")
	}
	pages := map[string]bool{}
	for _, page := range request.Pages {
		if !nonempty(page.ID) || !nonempty(page.ArtifactID) || pages[page.ID] || page.Width <= 0 || page.Height <= 0 || page.DPI <= 0 || int64(page.Width) > model.MaxPagePixels/int64(page.Height) {
			return nil, errors.New("invalid OCR page")
		}
		pages[page.ID] = true
	}
	return entry.provider, nil
}
func nonempty(value string) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func validateCapabilities(c Capabilities) error {
	if c.APIVersion != APIVersion || !nonempty(c.ID) || !nonempty(c.Name) || !nonempty(c.Version) || len(c.Models) == 0 {
		return errors.New("invalid OCR capabilities")
	}
	models := map[string]bool{}
	for _, m := range c.Models {
		if !nonempty(m.ID) || !nonempty(m.Name) || !nonempty(m.Version) || models[m.ID] || len(m.Languages) == 0 || len(m.Outputs) == 0 || m.MaxPages <= 0 || m.MaxPagePixels <= 0 {
			return errors.New("invalid OCR model capabilities")
		}
		if m.ProcessingDestination != "" && !nonempty(m.ProcessingDestination) {
			return errors.New("invalid OCR processing destination")
		}
		models[m.ID] = true
		languages := map[string]bool{}
		for _, v := range m.Languages {
			if !nonempty(v) || languages[v] {
				return errors.New("invalid OCR language capabilities")
			}
			languages[v] = true
		}
		presets := map[string]bool{}
		for _, v := range m.QualityPresets {
			if !nonempty(v) || presets[v] {
				return errors.New("invalid OCR preset capabilities")
			}
			presets[v] = true
		}
		outputs := map[OutputKind]bool{}
		for _, v := range m.Outputs {
			if (v != Text && v != PositionedText && v != SearchablePDF && v != PDFTextLayer) || outputs[v] {
				return errors.New("invalid OCR output capabilities")
			}
			outputs[v] = true
		}
		if c.ExternalProcessing && (outputs[SearchablePDF] || outputs[PDFTextLayer]) && !nonempty(m.ProcessingDestination) {
			return errors.New("OCR processing destination required")
		}
	}
	return nil
}
func clone(c Capabilities) Capabilities {
	c.Models = append([]Model(nil), c.Models...)
	for i := range c.Models {
		c.Models[i].Languages = append([]string(nil), c.Models[i].Languages...)
		c.Models[i].QualityPresets = append([]string(nil), c.Models[i].QualityPresets...)
		c.Models[i].Outputs = append([]OutputKind(nil), c.Models[i].Outputs...)
	}
	return c
}

// ValidatePositionedPage checks output shape and finite in-bounds geometry. It does
// not establish recognition accuracy or reading order quality. The artifact reader
// must additionally enforce byte/span/text limits while decoding JSON.
func ValidatePositionedPage(page PositionedPage, input InputPage) error {
	if page.PageID != input.ID || page.Width != input.Width || page.Height != input.Height || page.Width <= 0 || page.Height <= 0 {
		return errors.New("OCR geometry does not match input")
	}
	for _, span := range page.Spans {
		if span.Text == "" || !utf8.ValidString(span.Text) {
			return errors.New("invalid OCR text")
		}
		box := span.Box
		for _, value := range []float64{box.X, box.Y, box.Width, box.Height} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("non-finite OCR geometry")
			}
		}
		if box.X < 0 || box.Y < 0 || box.Width <= 0 || box.Height <= 0 || box.X+box.Width > float64(page.Width) || box.Y+box.Height > float64(page.Height) {
			return errors.New("OCR geometry is outside input")
		}
		if span.Confidence != nil {
			v := *span.Confidence
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
				return errors.New("invalid OCR confidence")
			}
		}
	}
	return nil
}

// CopyCapabilities validates public metadata and returns a detached copy. This
// lets capability publishers/readers share the plugin contract without creating
// a provider or obtaining execution credentials.
func CopyCapabilities(capabilities Capabilities) (Capabilities, error) {
	if err := validateCapabilities(capabilities); err != nil {
		return Capabilities{}, err
	}
	return clone(capabilities), nil
}
