package documents_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type readyScanRecordsFunc func(context.Context, string, documents.ReadyScanCommand) (documents.ScanAdmission, error)

func (f readyScanRecordsFunc) ApplyReadyScan(ctx context.Context, o string, c documents.ReadyScanCommand) (documents.ScanAdmission, error) {
	return f(ctx, o, c)
}

func readyServiceLimits() documents.ScanLimits {
	return documents.ScanLimits{AdmissionLimits: renderServiceLimits(), MaxDocuments: 10, MaxPagePixels: 1000000, DocumentLifetime: time.Hour}
}
func readyServiceRequest() documents.ScanRequest {
	return documents.ScanRequest{Purpose: "photo", DeviceID: "scanner_service_001", Capture: documents.Capture{Size: "a4", Color: "gray", DPI: 300}, IdempotencyKey: "request_service_001", ReserveBytes: 100}
}

func TestReadyScanServiceValidatesBeforeBackend(t *testing.T) {
	if _, err := documents.NewReadyScanService(nil); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal(err)
	}
	calls := 0
	service, err := documents.NewReadyScanService(readyScanRecordsFunc(func(context.Context, string, documents.ReadyScanCommand) (documents.ScanAdmission, error) {
		calls++
		return documents.ScanAdmission{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	valid := readyServiceRequest()
	if _, err = service.AdmitReadyScan(ctx, "principal_service_001", valid, readyServiceLimits()); err != nil || calls != 1 {
		t.Fatal("invalid valid control", err, calls)
	}
	calls = 0
	for _, mutate := range []func(*documents.ScanRequest){func(r *documents.ScanRequest) { r.WorkerBootID = "worker_client_0001" }, func(r *documents.ScanRequest) { r.IdempotencyKey = "" }, func(r *documents.ScanRequest) { r.DeviceID = "" }, func(r *documents.ScanRequest) { r.Revision = 1 }, func(r *documents.ScanRequest) { r.Capture.DPI = 123 }, func(r *documents.ScanRequest) { r.PhotoFormat = "gif" }, func(r *documents.ScanRequest) { r.DocumentID = "document_service_001" }} {
		r := valid
		mutate(&r)
		if _, err = service.AdmitReadyScan(ctx, "principal_service_001", r, readyServiceLimits()); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid request", r, err)
		}
	}
	for _, mutate := range []func(*documents.ScanLimits){func(l *documents.ScanLimits) { l.MaxPagePixels = 0 }, func(l *documents.ScanLimits) { l.MaxConcurrentScans = 33 }, func(l *documents.ScanLimits) { l.MaxDocuments = 0 }} {
		l := readyServiceLimits()
		mutate(&l)
		if _, err = service.AdmitReadyScan(ctx, "principal_service_001", valid, l); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("invalid limits", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached backend", calls)
	}
}

func TestReadyScanCommandDetachesPolicyAndPreservesV1Identity(t *testing.T) {
	var command documents.ReadyScanCommand
	backendErr := errors.New("backend uncertain")
	service, err := documents.NewReadyScanService(readyScanRecordsFunc(func(_ context.Context, owner string, c documents.ReadyScanCommand) (documents.ScanAdmission, error) {
		if owner != "principal_service_001" {
			t.Fatal(owner)
		}
		command = c
		return documents.ScanAdmission{}, backendErr
	}))
	if err != nil {
		t.Fatal(err)
	}
	limits := readyServiceLimits()
	limits.RegisteredDevices = []string{"scanner_service_001"}
	r := readyServiceRequest()
	if _, err = service.AdmitReadyScan(context.Background(), "principal_service_001", r, limits); err != backendErr {
		t.Fatal(err)
	}
	limits.RegisteredDevices[0] = "caller_mutation_001"
	copy, policy, err := command.Request()
	if err != nil || copy.PhotoFormat != "jpeg" || copy.WorkerBootID != "" || policy.MaxConcurrentScans != 1 || policy.RegisteredDevices[0] != "scanner_service_001" || command.Operation() != "scan-new" {
		t.Fatal(copy, policy, err)
	}
	policy.RegisteredDevices[0] = "accessor_mutation_001"
	_, policy, err = command.Request()
	if err != nil || policy.RegisteredDevices[0] != "scanner_service_001" {
		t.Fatal("policy changed", err)
	}
	key, digest := command.Hashes()
	wire := `{"capture":{"color":"gray","dpi":300,"size":"a4"},"device_id":"scanner_service_001","photo_format":"jpeg","purpose":"photo"}`
	if key != sha256.Sum256([]byte(r.IdempotencyKey)) || digest != sha256.Sum256([]byte("scan-new-v1\x00"+wire)) {
		t.Fatal("v1 scan-new hash changed")
	}
	r.DocumentID = "document_service_001"
	r.Revision = 7
	r.Purpose = ""
	r.PhotoFormat = ""
	r.ReserveBytes = -1
	if _, err = service.AdmitReadyScan(context.Background(), "principal_service_001", r, readyServiceLimits()); err != backendErr {
		t.Fatal(err)
	}
	_, digest = command.Hashes()
	wire = `{"capture":{"color":"gray","dpi":300,"size":"a4"},"device_id":"scanner_service_001","document_id":"document_service_001","revision":7}`
	if command.Operation() != "scan-next" || digest != sha256.Sum256([]byte("scan-next-v1\x00"+wire)) {
		t.Fatal("v1 scan-next hash changed")
	}
	for _, registry := range [][]string{nil, {}} {
		l := readyServiceLimits()
		l.RegisteredDevices = registry
		if _, err = service.AdmitReadyScan(context.Background(), "principal_service_001", r, l); err != backendErr {
			t.Fatal(err)
		}
		_, got, err := command.Request()
		if err != nil || (got.RegisteredDevices == nil) != (registry == nil) {
			t.Fatal("nil/empty registry semantics changed", err)
		}
	}
}

func TestSQLiteRejectsUnpreparedReadyScanCommand(t *testing.T) {
	var store *documents.Store
	if _, err := store.ApplyReadyScan(context.Background(), "principal_service_001", documents.ReadyScanCommand{}); !errors.Is(err, documents.ErrInvalid) {
		t.Fatal("zero command reached SQL", err)
	}
}
