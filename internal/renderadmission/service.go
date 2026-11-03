// Package renderadmission joins HTTP-neutral export intent and server policy.
package renderadmission

import (
	"context"
	"errors"
	"scanner-kiosk/internal/documents"
	"slices"
)

var ErrProviderUnavailable = errors.New("OCR capability unavailable")
var ErrUnsupported = errors.New("unsupported OCR selection")

type Records interface {
	FindRender(context.Context, string, documents.RenderAdmission) (documents.Admission, bool, error)
	AdmitRender(context.Context, string, documents.RenderAdmission, documents.AdmissionLimits) (documents.Admission, error)
}
type ResolvedOCR struct {
	ModelVersion, QualityPreset string
	MaxPages                    int
	MaxPagePixels               int64
}

// Resolver consumes trusted worker capability projections, never browser secrets
// or provider constructors. A version is pinned only for new work.
type Resolver interface {
	ResolveOCR(context.Context, documents.RenderOCR) (ResolvedOCR, error)
}
type Service struct {
	Records  Records
	Resolver Resolver
	Limits   documents.AdmissionLimits
}

func (s *Service) Admit(ctx context.Context, owner string, request documents.RenderAdmission) (documents.Admission, error) {
	if request.Settings.OCR.ModelVersion != "" || request.ResolvedQualityPreset != "" || request.ReserveBytes != 0 {
		return documents.Admission{}, documents.ErrInvalid
	}
	// Keep one client intent across caller, receipt-store and resolver mutation.
	request.Settings.OCR.Languages = slices.Clone(request.Settings.OCR.Languages)
	lookup := request
	lookup.Settings.OCR.Languages = slices.Clone(request.Settings.OCR.Languages)
	previous, found, err := s.Records.FindRender(ctx, owner, lookup)
	if err != nil {
		return documents.Admission{}, err
	}
	if found {
		return previous, nil
	}
	limits := s.Limits
	if request.Settings.OCR.Enabled {
		if s.Resolver == nil {
			return documents.Admission{}, ErrProviderUnavailable
		}
		selection := request.Settings.OCR
		selection.Languages = slices.Clone(selection.Languages)
		resolved, err := s.Resolver.ResolveOCR(ctx, selection)
		if errors.Is(err, ErrUnsupported) {
			return documents.Admission{}, ErrUnsupported
		}
		if err != nil || resolved.ModelVersion == "" || resolved.MaxPages < 1 || resolved.MaxPagePixels < 1 || request.Settings.OCR.QualityPreset != "" && resolved.QualityPreset != request.Settings.OCR.QualityPreset {
			return documents.Admission{}, ErrProviderUnavailable
		}
		limits.OCRMaxPages, limits.OCRMaxPagePixels = resolved.MaxPages, resolved.MaxPagePixels
		if s.Limits.OCRMaxPagePixels > 0 {
			limits.OCRMaxPagePixels = min(limits.OCRMaxPagePixels, s.Limits.OCRMaxPagePixels)
		}
		request.Settings.OCR.ModelVersion = resolved.ModelVersion
		request.ResolvedQualityPreset = resolved.QualityPreset
	}
	// Conservative reservation: worker execution cannot exceed the configured
	// whole-job ceiling. Capacity and disk headroom are enforced in the transaction.
	request.ReserveBytes = s.Limits.JobMaxBytes
	return s.Records.AdmitRender(ctx, owner, request, limits)
}
