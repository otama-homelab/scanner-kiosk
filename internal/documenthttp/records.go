package documenthttp

import (
	"context"
	"io"
	"time"

	"scanner-kiosk/internal/documents"
)

// Records exposes authorized projections and atomic commands. Implementations
// recheck enabled owners, expiry and revision inside each transaction, including
// repeated file reads. No SQL transaction, CRUD handle or unguarded path is
// exposed to HTTP. Deletion/cancellation record intent, never proof of stop.
type Records interface {
	ReadDocument(context.Context, string, string) (documents.Projection, error)
	ReadHistory(context.Context, string, int, *documents.Position) ([]documents.Projection, *documents.Position, error)
	ReadJob(context.Context, string, string) (documents.JobProjection, error)
	CancelJob(context.Context, string, string, string, time.Duration) (documents.JobProjection, error)
	Edit(context.Context, string, documents.EditRequest, time.Duration) (documents.Projection, error)
	RequestDelete(context.Context, string, documents.DeleteRequest, time.Duration) (documents.JobProjection, error)
	ReadPageEffective(context.Context, string, string, string) (documents.ArtifactReference, error)
	ReadPageOriginal(context.Context, string, string, string) (documents.PageOriginal, error)
	ReadCorrectionArtifact(context.Context, string, string, string) (documents.ArtifactReference, error)
	ReadPageThumbnail(context.Context, string, string, string) (documents.ArtifactReference, error)
	ReadCurrentOutput(context.Context, string, string) (documents.ArtifactReference, error)
	ApplyCorrection(context.Context, string, documents.CorrectionRequest, time.Duration) (documents.Projection, error)
	UndoCorrection(context.Context, string, documents.CorrectionRequest, time.Duration) (documents.Projection, error)
	AdoptOCRResult(context.Context, string, documents.OCRResultRequest, time.Duration) (documents.Projection, error)
	DiscardOCRResult(context.Context, string, documents.OCRResultRequest, time.Duration) (documents.Projection, error)
	ReadOCRResult(context.Context, string, string, string) (documents.OCRResultProjection, error)
	ReadOCRResultArtifact(context.Context, string, string, string, string, string) (documents.ArtifactReference, error)
}

type RenderAdmission interface {
	Admit(context.Context, string, documents.RenderAdmission) (documents.Admission, error)
}
type ScanAdmission interface {
	Admit(context.Context, string, documents.ScanRequest) (documents.ScanAdmission, error)
}

var _ Records = (*documents.Store)(nil)

type ImportAdmission interface {
	Receive(context.Context, string, documents.ImportRequest, io.Reader) (documents.Admission, error)
}

// DeliveryRecords exposes owner-authorized ledger operations without upload or file authority.
type DeliveryRecords interface {
	AdmitDelivery(context.Context, string, documents.DeliveryRequest) (documents.Delivery, error)
	ReadDelivery(context.Context, string, string) (documents.Delivery, error)
	ReadCurrentDelivery(context.Context, string, string, string) (documents.Delivery, error)
	CancelDelivery(context.Context, string, string) (documents.Delivery, error)
	AcceptDelivery(context.Context, string, string, string) error
	ConfirmDelivery(context.Context, string, string, string, int64, [32]byte) error
	MarkDeliveryUnknown(context.Context, string, string) error
	FailDelivery(context.Context, string, string, string) error
}
