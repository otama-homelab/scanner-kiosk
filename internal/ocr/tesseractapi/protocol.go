// Package tesseractapi connects the application OCR port to an optional Tesseract service.
package tesseractapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"scanner-kiosk/pkg/ocrplugin"
)

var errService = errors.New("Tesseract OCR service unavailable or invalid")

type Limits struct {
	MaxInputBytes, MaxResultBytes, MaxPagePixels int64
	MaxPages                                     int
	Timeout                                      time.Duration
}

func (l Limits) valid() bool {
	return l.MaxInputBytes > 0 && l.MaxInputBytes <= 1<<40 && l.MaxResultBytes > 0 && l.MaxResultBytes <= 64<<20 && l.MaxPagePixels > 0 && l.MaxPages > 0 && l.MaxPages <= 10000 && l.Timeout > 0 && l.Timeout <= time.Hour
}
func validKey(key string) bool {
	if len(key) < 32 || len(key) > 256 {
		return false
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// One bitmap per call. Identifiers, paths and owner/session data stay in the application.
type wireRequest struct {
	ModelVersion  string               `json:"model_version"`
	Languages     []string             `json:"languages"`
	QualityPreset string               `json:"quality_preset"`
	Output        ocrplugin.OutputKind `json:"output"`
	Width         int                  `json:"width"`
	Height        int                  `json:"height"`
	DPI           int                  `json:"dpi"`
}

func (r wireRequest) request() ocrplugin.Request {
	return ocrplugin.Request{JobID: "request", InputRevision: 1, ModelID: "installed", ModelVersion: r.ModelVersion, Languages: r.Languages, QualityPreset: r.QualityPreset, Output: r.Output, Pages: []ocrplugin.InputPage{{ID: "page", ArtifactID: "input", Width: r.Width, Height: r.Height, DPI: r.DPI}}}
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errService
	}
	if d.Decode(new(any)) != io.EOF {
		return errService
	}
	return nil
}
func validImage(input io.Reader, width, height int, pixels int64) bool {
	if width <= 0 || height <= 0 || int64(width) > pixels/int64(height) {
		return false
	}
	c, format, err := image.DecodeConfig(input)
	return err == nil && (format == "png" || format == "jpeg") && c.Width == width && c.Height == height
}
func contentType(kind ocrplugin.OutputKind) string {
	if kind == ocrplugin.PositionedText {
		return "application/json"
	}
	if kind == ocrplugin.Text {
		return "text/plain; charset=utf-8"
	}
	return "application/pdf"
}
func clone(c ocrplugin.Capabilities) ocrplugin.Capabilities {
	c.Models = append([]ocrplugin.Model(nil), c.Models...)
	for i := range c.Models {
		c.Models[i].Languages = append([]string(nil), c.Models[i].Languages...)
		c.Models[i].QualityPresets = append([]string(nil), c.Models[i].QualityPresets...)
		c.Models[i].Outputs = append([]ocrplugin.OutputKind(nil), c.Models[i].Outputs...)
	}
	return c
}
func nativeCapabilities(c ocrplugin.Capabilities) bool {
	if c.ID != "tesseract" || c.Version != "1" || len(c.Models) != 1 || c.Models[0].ID != "installed" {
		return false
	}
	m := c.Models[0]
	version := strings.TrimPrefix(m.Version, "sha256-")
	if len(version) != 64 || version == m.Version {
		return false
	}
	for _, c := range version {
		if !(c >= 'a' && c <= 'f') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	for _, output := range m.Outputs {
		if output != ocrplugin.Text && output != ocrplugin.PDFTextLayer && output != ocrplugin.PositionedText {
			return false
		}
	}
	return true
}

// The service uses only its fixed page identity; private application IDs never cross the wire.
func positionedResult(raw []byte, input ocrplugin.InputPage) (ocrplugin.PositionedPage, error) {
	var page ocrplugin.PositionedPage
	if !utf8.Valid(raw) || decode(raw, &page) != nil || page.Spans == nil || len(page.Spans) > 100000 || ocrplugin.ValidatePositionedPage(page, input) != nil {
		return ocrplugin.PositionedPage{}, errService
	}
	return page, nil
}
