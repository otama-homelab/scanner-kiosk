// Package scanadmission applies server policy to HTTP-neutral scan intent.
package scanadmission

import (
	"context"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/scannermetadata"
	"slices"
)

var ErrUnsupported = documents.ErrScannerSelection

type Records interface {
	AdmitReadyScan(context.Context, string, documents.ScanRequest, documents.ScanLimits) (documents.ScanAdmission, error)
}
type Service struct {
	records Records
	limits  documents.ScanLimits
}

func New(records Records, devices []string, limits documents.ScanLimits) (*Service, error) {
	if records == nil || len(devices) > 32 || limits.JobMaxBytes < 1 || limits.MaxPagePixels < 1 || limits.MaxDocuments < 1 || limits.DocumentLifetime <= 0 {
		return nil, documents.ErrInvalid
	}
	limits.RegisteredDevices = append([]string{}, devices...)
	out := &Service{records: records, limits: limits}
	seen := map[string]bool{}
	for _, device := range devices {
		if !scannermetadata.ValidID(device) || seen[device] {
			return nil, documents.ErrInvalid
		}
		seen[device] = true
	}
	return out, nil
}
func (s *Service) Admit(ctx context.Context, owner string, request documents.ScanRequest) (documents.ScanAdmission, error) {
	if request.WorkerBootID != "" || request.ReserveBytes != 0 {
		return documents.ScanAdmission{}, documents.ErrInvalid
	}
	// Reserve the full native output ceiling; shared logical/disk checks and live
	// worker capacity/readiness still run inside the admission transaction.
	request.ReserveBytes = s.limits.JobMaxBytes
	limits := s.limits
	limits.RegisteredDevices = slices.Clone(s.limits.RegisteredDevices)
	return s.records.AdmitReadyScan(ctx, owner, request, limits)
}
