package renderadmission

import (
	"context"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/pkg/ocrplugin"
	"slices"
	"time"
)

type ModelDefault struct{ ProviderID, ModelID, QualityPreset string }

// CatalogSelection is the operator-configured preference, not an enabled job or version pin.
type CatalogSelection struct {
	ProviderID, ModelID, QualityPreset string
	Languages                          []string
}

// CatalogSnapshot carries worker-authored metadata only. Its source must also
// fence worker identity/generation; wall-clock freshness is not boot authority.
type CatalogSnapshot struct {
	PublishedAtMS, ExpiresAtMS int64
	Providers                  []ocrplugin.Capabilities
	Defaults                   []ModelDefault
	Preferred                  *CatalogSelection
}
type CatalogSource interface {
	ReadOCRCatalog(context.Context) (CatalogSnapshot, error)
}
type CatalogResolver struct {
	Source CatalogSource
	Now    func() time.Time
}

// ResolveOCR pins an exact advertised model. It neither probes providers nor
// reads credentials, performs OCR, chooses another model, or disables OCR.
func (r *CatalogResolver) ResolveOCR(ctx context.Context, request documents.RenderOCR, reOCR bool) (ResolvedOCR, error) {
	if r.Source == nil || r.Now == nil {
		return ResolvedOCR{}, ErrProviderUnavailable
	}
	snapshot, err := r.Source.ReadOCRCatalog(ctx)
	if err != nil {
		return ResolvedOCR{}, ErrProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ResolvedOCR{}, err
	}
	models, defaults, err := catalogContents(snapshot, r.Now().UnixMilli())
	if err != nil {
		return ResolvedOCR{}, err
	}
	if !request.Enabled || request.ModelVersion != "" || len(request.Languages) == 0 {
		return ResolvedOCR{}, ErrUnsupported
	}
	key := catalogIdentity{request.ProviderID, request.ModelID}
	model, exists := models[key]
	if !exists {
		return ResolvedOCR{}, ErrUnsupported
	}
	languages := map[string]bool{}
	for _, language := range request.Languages {
		if languages[language] || !hasString(model.Languages, language) {
			return ResolvedOCR{}, ErrUnsupported
		}
		languages[language] = true
	}
	preset := request.QualityPreset
	if preset == "" && len(model.QualityPresets) > 0 {
		var present bool
		preset, present = defaults[key]
		if !present {
			return ResolvedOCR{}, ErrProviderUnavailable
		}
	}
	if len(model.QualityPresets) == 0 && preset != "" || len(model.QualityPresets) > 0 && !hasString(model.QualityPresets, preset) {
		return ResolvedOCR{}, ErrUnsupported
	}
	// ReOCR's coordinator retains recognition artifacts before common rendering.
	outputs := []ocrplugin.OutputKind{ocrplugin.PDFTextLayer, ocrplugin.SearchablePDF}
	if reOCR {
		outputs = append([]ocrplugin.OutputKind{ocrplugin.PositionedText, ocrplugin.Text}, outputs...)
	}
	var selected ocrplugin.OutputKind
	for _, kind := range outputs {
		if slices.Contains(model.Outputs, kind) {
			selected = kind
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return ResolvedOCR{}, err
	}
	if r.Now().UnixMilli() >= snapshot.ExpiresAtMS {
		return ResolvedOCR{}, ErrProviderUnavailable
	}
	if selected == "" {
		return ResolvedOCR{}, ErrUnsupported
	}
	return ResolvedOCR{ModelVersion: model.Version, QualityPreset: preset, MaxPages: model.MaxPages, MaxPagePixels: model.MaxPagePixels, Output: selected}, nil
}
func hasString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type catalogIdentity struct{ provider, model string }

// ValidateCatalogSnapshot validates metadata without selecting or running a model.
func ValidateCatalogSnapshot(snapshot CatalogSnapshot, now time.Time) error {
	_, _, err := catalogContents(snapshot, now.UnixMilli())
	return err
}
func catalogContents(snapshot CatalogSnapshot, now int64) (map[catalogIdentity]ocrplugin.Model, map[catalogIdentity]string, error) {
	if snapshot.PublishedAtMS < 0 || snapshot.PublishedAtMS > now || snapshot.ExpiresAtMS <= now || snapshot.ExpiresAtMS <= snapshot.PublishedAtMS || snapshot.ExpiresAtMS-snapshot.PublishedAtMS > int64((5*time.Minute)/time.Millisecond) || len(snapshot.Providers) > 16 || len(snapshot.Defaults) > 256 {
		return nil, nil, ErrProviderUnavailable
	}
	models := map[catalogIdentity]ocrplugin.Model{}
	seen := map[string]bool{}
	for _, provider := range snapshot.Providers {
		if seen[provider.ID] || len(provider.Models) > 256 {
			return nil, nil, ErrProviderUnavailable
		}
		seen[provider.ID] = true
		for _, m := range provider.Models {
			if len(m.Languages) > 64 || len(m.QualityPresets) > 64 || len(m.Outputs) > 4 {
				return nil, nil, ErrProviderUnavailable
			}
		}
		detached, err := ocrplugin.CopyCapabilities(provider)
		if err != nil {
			return nil, nil, ErrProviderUnavailable
		}
		for _, model := range detached.Models {
			models[catalogIdentity{detached.ID, model.ID}] = model
		}
	}
	defaults := map[catalogIdentity]string{}
	for _, selected := range snapshot.Defaults {
		key := catalogIdentity{selected.ProviderID, selected.ModelID}
		model, exists := models[key]
		validPreset := len(model.QualityPresets) == 0 && selected.QualityPreset == "" || hasString(model.QualityPresets, selected.QualityPreset)
		if _, duplicate := defaults[key]; duplicate || !exists || !validPreset {
			return nil, nil, ErrProviderUnavailable
		}
		defaults[key] = selected.QualityPreset
	}

	if preferred := snapshot.Preferred; preferred != nil {
		model, exists := models[catalogIdentity{preferred.ProviderID, preferred.ModelID}]
		if !exists || len(preferred.Languages) == 0 || len(preferred.Languages) > 32 {
			return nil, nil, ErrProviderUnavailable
		}
		seen := map[string]bool{}
		for _, language := range preferred.Languages {
			if seen[language] || !hasString(model.Languages, language) {
				return nil, nil, ErrProviderUnavailable
			}
			seen[language] = true
		}
		if len(model.QualityPresets) == 0 && preferred.QualityPreset != "" || len(model.QualityPresets) > 0 && !hasString(model.QualityPresets, preferred.QualityPreset) {
			return nil, nil, ErrProviderUnavailable
		}
	}

	return models, defaults, nil
}
