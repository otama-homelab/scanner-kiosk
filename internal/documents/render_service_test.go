package documents_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type renderServiceContextKey struct{}

type renderRecords struct {
	lookup func(context.Context, string, documents.RenderIntent) (documents.Admission, bool, error)
	apply  func(context.Context, string, documents.RenderCommand) (documents.Admission, error)
}

func (r renderRecords) LookupRender(ctx context.Context, owner string, i documents.RenderIntent) (documents.Admission, bool, error) {
	return r.lookup(ctx, owner, i)
}
func (r renderRecords) ApplyRender(ctx context.Context, owner string, c documents.RenderCommand) (documents.Admission, error) {
	return r.apply(ctx, owner, c)
}

func renderServiceLimits() documents.AdmissionLimits {
	return documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 10, JobTimeout: time.Minute, ReceiptRetention: time.Hour}
}
func renderServiceRequest() documents.RenderAdmission {
	return documents.RenderAdmission{DocumentID: "document_render_service_001", Revision: 7, IdempotencyKey: "request_render_service_001", Settings: documents.RenderSettings{Format: "pdf"}}
}

func TestRenderServiceValidatesBeforeBackend(t *testing.T) {
	if _, err := documents.NewRenderAdmissionService(nil); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal(err)
	}
	lookups, applies := 0, 0
	service, err := documents.NewRenderAdmissionService(renderRecords{
		lookup: func(context.Context, string, documents.RenderIntent) (documents.Admission, bool, error) {
			lookups++
			return documents.Admission{}, false, nil
		},
		apply: func(context.Context, string, documents.RenderCommand) (documents.Admission, error) {
			applies++
			return documents.Admission{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	valid := renderServiceRequest()
	if _, _, err = service.FindRender(ctx, "principal_render_service_001", valid); err != nil {
		t.Fatal(err)
	}
	if _, err = service.AdmitRender(ctx, "principal_render_service_001", valid, renderServiceLimits()); err != nil {
		t.Fatal(err)
	}
	if lookups != 1 || applies != 1 {
		t.Fatal("invalid valid control", lookups, applies)
	}
	lookups, applies = 0, 0
	for _, mutate := range []func(*documents.RenderAdmission){
		func(r *documents.RenderAdmission) { r.DocumentID = "" },
		func(r *documents.RenderAdmission) { r.Revision = 0 },
		func(r *documents.RenderAdmission) { r.IdempotencyKey = "" },
		func(r *documents.RenderAdmission) { r.Settings.Format = "zip" },
		func(r *documents.RenderAdmission) { r.Settings.PDFEncoding = "invalid" },
		func(r *documents.RenderAdmission) { r.Settings.OCR.Languages = []string{"eng"} },
	} {
		r := valid
		mutate(&r)
		if _, _, err = service.FindRender(ctx, "principal_render_service_001", r); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid lookup", r, err)
		}
		if _, err = service.AdmitRender(ctx, "principal_render_service_001", r, renderServiceLimits()); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid admission", r, err)
		}
	}
	if _, err = service.AdmitRender(ctx, "principal_render_service_001", valid, documents.AdmissionLimits{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("invalid policy", err)
	}
	if lookups != 0 || applies != 0 {
		t.Fatal("invalid input performed storage work", lookups, applies)
	}
}

func TestRenderServiceDetachesCommandsAndPreservesV1Identity(t *testing.T) {
	var intent documents.RenderIntent
	var command documents.RenderCommand
	backendErr := errors.New("backend unavailable")
	check := func(ctx context.Context, owner string) {
		t.Helper()
		if ctx.Value(renderServiceContextKey{}) != "caller" || owner != "principal_render_service_001" {
			t.Fatal("authority context changed")
		}
	}
	service, err := documents.NewRenderAdmissionService(renderRecords{
		lookup: func(ctx context.Context, owner string, i documents.RenderIntent) (documents.Admission, bool, error) {
			check(ctx, owner)
			intent = i
			return documents.Admission{}, false, backendErr
		},
		apply: func(ctx context.Context, owner string, c documents.RenderCommand) (documents.Admission, error) {
			check(ctx, owner)
			command = c
			return documents.Admission{}, backendErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), renderServiceContextKey{}, "caller")
	r := renderServiceRequest()
	r.Settings.OCR = documents.RenderOCR{Enabled: true, ProviderID: "plugin", ModelID: "model", Languages: []string{"eng"}}
	if _, _, err = service.FindRender(ctx, "principal_render_service_001", r); err != backendErr {
		t.Fatal(err)
	}
	r.Settings.OCR.Languages[0] = "caller_mutation"
	copy, err := intent.Request()
	if err != nil || copy.Settings.PDFEncoding != "jpeg" || copy.Settings.OCR.Languages[0] != "eng" {
		t.Fatal("intent changed", copy, err)
	}
	copy.Settings.OCR.Languages[0] = "accessor_mutation"
	copy, err = intent.Request()
	if err != nil || copy.Settings.OCR.Languages[0] != "eng" {
		t.Fatal("accessor changed intent", err)
	}
	r.Settings.OCR.Languages = []string{"eng"}
	r.Settings.OCR.ModelVersion = "server-v2"
	r.ResolvedQualityPreset = "standard"
	r.ReserveBytes = 100
	if _, err = service.AdmitRender(ctx, "principal_render_service_001", r, renderServiceLimits()); err != backendErr {
		t.Fatal(err)
	}
	r.Settings.OCR.Languages[0] = "later_mutation"
	copy, limits, err := command.Request()
	if err != nil || copy.Settings.OCR.Languages[0] != "eng" || copy.Settings.OCR.ModelVersion != "server-v2" || copy.ResolvedQualityPreset != "standard" || copy.ReserveBytes != 100 || limits != renderServiceLimits() {
		t.Fatal("prepared new work changed", copy, limits, err)
	}
	copy.Settings.OCR.Languages[0] = "returned_mutation"
	copy, _, err = command.Request()
	if err != nil || copy.Settings.OCR.Languages[0] != "eng" {
		t.Fatal("adapter changed command", err)
	}
	key, digest := intent.Hashes()
	newKey, newDigest := command.Hashes()
	wire := `{"format":"pdf","ocr":{"enabled":true,"languages":["eng"],"model_id":"model","provider_id":"plugin"},"pdf_encoding":"jpeg"}`
	expected := sha256.Sum256([]byte("render-v1\x00document_render_service_001\x007\x00" + wire))
	if key != sha256.Sum256([]byte("request_render_service_001")) || digest != expected || key != newKey || digest != newDigest {
		t.Fatal("existing client identity changed with server decision")
	}
}

func TestSQLiteRejectsUnpreparedRenderCommands(t *testing.T) {
	var store *documents.Store
	if _, _, err := store.LookupRender(context.Background(), "principal_render_service_001", documents.RenderIntent{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero intent reached SQL", err)
	}
	if _, err := store.ApplyRender(context.Background(), "principal_render_service_001", documents.RenderCommand{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero command reached SQL", err)
	}
}
