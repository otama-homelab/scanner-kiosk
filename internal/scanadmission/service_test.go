package scanadmission

import (
	"context"
	"errors"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

type recordsFunc func(context.Context, string, documents.ScanRequest, documents.ScanLimits) (documents.ScanAdmission, error)

func (f recordsFunc) AdmitReadyScan(ctx context.Context, owner string, r documents.ScanRequest, l documents.ScanLimits) (documents.ScanAdmission, error) {
	return f(ctx, owner, r, l)
}

func TestServiceRegistryIsDetachedAcrossCalls(t *testing.T) {
	devices := []string{"scanner_service_001"}
	calls := 0
	records := recordsFunc(func(_ context.Context, owner string, r documents.ScanRequest, l documents.ScanLimits) (documents.ScanAdmission, error) {
		calls++
		if owner != "principal_service_001" || r.WorkerBootID != "" || r.ReserveBytes != 100 || len(l.RegisteredDevices) != 1 || l.RegisteredDevices[0] != "scanner_service_001" {
			t.Fatal("server policy changed", r, l)
		}
		l.RegisteredDevices[0] = "backend_mutation_001"
		return documents.ScanAdmission{}, nil
	})
	service, err := New(records, devices, documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{JobMaxBytes: 100}, MaxDocuments: 1, MaxPagePixels: 10000, DocumentLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	devices[0] = "caller_mutation_001"
	for range 2 {
		if _, err = service.Admit(context.Background(), "principal_service_001", documents.ScanRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestServiceKeepsEmptyRegistryAndRejectsClientAuthority(t *testing.T) {
	calls := 0
	records := recordsFunc(func(_ context.Context, _ string, _ documents.ScanRequest, l documents.ScanLimits) (documents.ScanAdmission, error) {
		calls++
		if l.RegisteredDevices == nil || len(l.RegisteredDevices) != 0 {
			t.Fatal("empty deployment registry became unrestricted")
		}
		return documents.ScanAdmission{}, documents.ErrScannerSelection
	})
	service, err := New(records, nil, documents.ScanLimits{AdmissionLimits: documents.AdmissionLimits{JobMaxBytes: 100}, MaxDocuments: 1, MaxPagePixels: 10000, DocumentLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Admit(context.Background(), "principal_service_001", documents.ScanRequest{}); !errors.Is(err, documents.ErrScannerSelection) {
		t.Fatal(err)
	}
	for _, request := range []documents.ScanRequest{{WorkerBootID: "worker_client_0001"}, {ReserveBytes: 1}} {
		if _, err = service.Admit(context.Background(), "principal_service_001", request); !errors.Is(err, documents.ErrInvalid) {
			t.Fatal("client authority accepted", err)
		}
	}
	if calls != 1 {
		t.Fatal("invalid client authority reached backend", calls)
	}
}
