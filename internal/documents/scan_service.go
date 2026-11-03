package documents

import (
	"context"
	"crypto/sha256"
	"slices"
	"time"
)

// ReadyScanRecords must resolve live readiness/boot/capacity/idle epoch and
// capabilities in the same transaction as job/reservation/receipt admission.
// A prepared command supplies shape/policy, never proof of physical authority.
type ReadyScanRecords interface {
	ApplyReadyScan(context.Context, string, ReadyScanCommand) (ScanAdmission, error)
}
type ReadyScanService struct{ records ReadyScanRecords }

func NewReadyScanService(records ReadyScanRecords) (*ReadyScanService, error) {
	if records == nil {
		return nil, ErrInvalid
	}
	return &ReadyScanService{records: records}, nil
}

// ReadyScanCommand cannot request the trusted, non-readiness admission path.
// Its zero value is invalid. The registry accessor preserves nil/empty semantics.
type ReadyScanCommand struct{ command scanCommand }

func (c ReadyScanCommand) Request() (ScanRequest, ScanLimits, error) {
	return c.command.requestValues()
}
func (c ReadyScanCommand) Hashes() ([32]byte, [32]byte) { return c.command.key, c.command.digest }
func (c ReadyScanCommand) Operation() string            { return c.command.operation }

type scanCommand struct {
	request     ScanRequest
	limits      ScanLimits
	operation   string
	key, digest [32]byte
	valid       bool
}

func (c scanCommand) requestValues() (ScanRequest, ScanLimits, error) {
	if !c.valid {
		return ScanRequest{}, ScanLimits{}, ErrInvalid
	}
	limits := c.limits
	limits.RegisteredDevices = slices.Clone(limits.RegisteredDevices)
	return c.request, limits, nil
}

func (s *ReadyScanService) AdmitReadyScan(ctx context.Context, owner string, request ScanRequest, limits ScanLimits) (ScanAdmission, error) {
	if request.WorkerBootID != "" || limits.MaxPagePixels < 1 || limits.MaxPagePixels > 1<<60 {
		return ScanAdmission{}, ErrInvalid
	}
	command, err := prepareScan(request, limits)
	if err != nil {
		return ScanAdmission{}, err
	}
	return s.records.ApplyReadyScan(ctx, owner, ReadyScanCommand{command: command})
}
func prepareScan(request ScanRequest, limits ScanLimits) (scanCommand, error) {
	if limits.MaxConcurrentScans == 0 {
		limits.MaxConcurrentScans = 1
	}
	if limits.MaxConcurrentScans < 1 || limits.MaxConcurrentScans > 32 {
		return scanCommand{}, ErrInvalid
	}
	first := request.DocumentID == ""
	if !id(request.IdempotencyKey) || !id(request.DeviceID) || !validAdmissionLimits(limits.AdmissionLimits) || limits.MaxDocuments < 1 || limits.DocumentLifetime < time.Millisecond || limits.DocumentLifetime > 365*24*time.Hour || !validCapture(request.Capture) {
		return scanCommand{}, ErrInvalid
	}
	if first {
		if request.Revision != 0 || (request.Purpose != "document" && request.Purpose != "photo") {
			return scanCommand{}, ErrInvalid
		}
	} else if !id(request.DocumentID) || request.Revision < 1 || request.Purpose != "" {
		return scanCommand{}, ErrInvalid
	}
	if first && request.Purpose == "photo" {
		if request.PhotoFormat == "" {
			request.PhotoFormat = "jpeg"
		}
		if request.PhotoFormat != "jpeg" && request.PhotoFormat != "png" {
			return scanCommand{}, ErrInvalid
		}
	} else if request.PhotoFormat != "" {
		return scanCommand{}, ErrInvalid
	}
	command, err := canonicalCommand(request)
	if err != nil {
		return scanCommand{}, ErrInvalid
	}
	operation := "scan-next"
	if first {
		operation = "scan-new"
	}
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	digest := sha256.Sum256(append([]byte(operation+"-v1\x00"), command...))
	limits.RegisteredDevices = slices.Clone(limits.RegisteredDevices)
	return scanCommand{request: request, limits: limits, operation: operation, key: key, digest: digest, valid: true}, nil
}

func validCapture(c Capture) bool {
	switch c.Size {
	case "a4", "a5", "a6", "letter", "photo10x15":
	default:
		return false
	}
	if c.Color != "color" && c.Color != "gray" {
		return false
	}
	switch c.DPI {
	case 75, 100, 150, 200, 300, 600:
		return true
	}
	return false
}
