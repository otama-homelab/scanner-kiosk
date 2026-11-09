package documents

import (
	"context"
	"crypto/sha256"
	"slices"
	"strconv"
)

// RenderRecords reconciles receipts and admits render work as backend-owned
// atomic commands, rechecking owner/expiry/revision/capacity before commit.
// Model/space decisions for new work never precede exact receipt replay.
type RenderRecords interface {
	LookupRender(context.Context, string, RenderIntent) (Admission, bool, error)
	ApplyRender(context.Context, string, RenderCommand) (Admission, error)
}

type RenderAdmissionService struct{ records RenderRecords }

func NewRenderAdmissionService(records RenderRecords) (*RenderAdmissionService, error) {
	if records == nil {
		return nil, ErrInvalid
	}
	return &RenderAdmissionService{records: records}, nil
}

// RenderIntent is a normalized, detached request and its established render-v1
// identity. Server-resolved model version is deliberately excluded from the hash.
type RenderIntent struct {
	request     RenderAdmission
	key, digest [32]byte
	valid       bool
}

func (i RenderIntent) Request() (RenderAdmission, error) {
	if !i.valid {
		return RenderAdmission{}, ErrInvalid
	}
	request := i.request
	request.Settings.OCR.Languages = slices.Clone(request.Settings.OCR.Languages)
	return request, nil
}
func (i RenderIntent) Hashes() ([32]byte, [32]byte) { return i.key, i.digest }

// RenderCommand adds immutable deployment limits to a validated intent. New-work
// model/reservation checks remain after the transactional receipt replay check.
type RenderCommand struct {
	intent RenderIntent
	limits AdmissionLimits
}

func (c RenderCommand) Request() (RenderAdmission, AdmissionLimits, error) {
	request, err := c.intent.Request()
	return request, c.limits, err
}
func (c RenderCommand) Hashes() ([32]byte, [32]byte) { return c.intent.Hashes() }

func prepareRender(request RenderAdmission) (RenderIntent, error) {
	settings, key, digest, err := renderIdentity(request)
	if err != nil {
		return RenderIntent{}, err
	}
	request.Settings = settings
	return RenderIntent{request: request, key: key, digest: digest, valid: true}, nil
}
func (s *RenderAdmissionService) FindRender(ctx context.Context, owner string, request RenderAdmission) (Admission, bool, error) {
	intent, err := prepareRender(request)
	if err != nil {
		return Admission{}, false, err
	}
	return s.records.LookupRender(ctx, owner, intent)
}
func (s *RenderAdmissionService) AdmitRender(ctx context.Context, owner string, request RenderAdmission, limits AdmissionLimits) (Admission, error) {
	if !validAdmissionLimits(limits) {
		return Admission{}, ErrInvalid
	}
	intent, err := prepareRender(request)
	if err != nil {
		return Admission{}, err
	}
	return s.records.ApplyRender(ctx, owner, RenderCommand{intent: intent, limits: limits})
}

func renderIdentity(request RenderAdmission) (RenderSettings, [32]byte, [32]byte, error) {
	if !id(request.DocumentID) || request.Revision < 1 || !id(request.IdempotencyKey) {
		return RenderSettings{}, [32]byte{}, [32]byte{}, ErrInvalid
	}
	settings, err := normalizeRender(request.Settings)
	if err != nil {
		return RenderSettings{}, [32]byte{}, [32]byte{}, err
	}
	if request.ReOCR && (!settings.OCR.Enabled || settings.Format != "pdf") {
		return RenderSettings{}, [32]byte{}, [32]byte{}, ErrInvalid
	}
	command := settings
	command.OCR.ModelVersion = ""
	canonical, err := canonicalCommand(command)
	if err != nil {
		return RenderSettings{}, [32]byte{}, [32]byte{}, ErrInvalid
	}
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	separator := string(rune(0))
	operation := "render-v1"
	if request.ReOCR {
		operation = "reocr-v1"
	}
	digest := sha256.Sum256([]byte(operation + separator + request.DocumentID + separator + strconv.FormatInt(request.Revision, 10) + separator + string(canonical)))
	return settings, key, digest, nil
}

func normalizeRender(in RenderSettings) (RenderSettings, error) {
	out := in
	out.OCR.Languages = append([]string(nil), in.OCR.Languages...)
	switch out.Format {
	case "pdf":
		if out.PDFEncoding == "" {
			out.PDFEncoding = "jpeg"
		}
		if out.PDFEncoding != "jpeg" && out.PDFEncoding != "lossless" {
			return RenderSettings{}, ErrInvalid
		}
	case "jpeg", "png":
		if out.PDFEncoding != "" {
			return RenderSettings{}, ErrInvalid
		}
	default:
		return RenderSettings{}, ErrInvalid
	}
	o := out.OCR
	if !o.Enabled {
		if o.ProviderID != "" || o.ModelID != "" || o.ModelVersion != "" || len(o.Languages) > 0 || o.QualityPreset != "" {
			return RenderSettings{}, ErrInvalid
		}
		return out, nil
	}
	if !label(o.ProviderID) || !label(o.ModelID) || len(o.Languages) < 1 || len(o.Languages) > 32 || (o.QualityPreset != "" && !label(o.QualityPreset)) || (o.ModelVersion != "" && !label(o.ModelVersion)) {
		return RenderSettings{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, lang := range o.Languages {
		if !label(lang) || seen[lang] {
			return RenderSettings{}, ErrInvalid
		}
		seen[lang] = true
	}
	return out, nil
}
