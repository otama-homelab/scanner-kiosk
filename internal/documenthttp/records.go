package documenthttp

import (
	"context"
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
	ReadPageOriginal(context.Context, string, string, string) (documents.PageOriginal, error)
	ReadPageThumbnail(context.Context, string, string, string) (documents.ArtifactReference, error)
	ReadCurrentOutput(context.Context, string, string) (documents.ArtifactReference, error)
}

type RenderAdmission interface {
	Admit(context.Context, string, documents.RenderAdmission) (documents.Admission, error)
}
type ScanAdmission interface {
	Admit(context.Context, string, documents.ScanRequest) (documents.ScanAdmission, error)
}

var _ Records = (*documents.Store)(nil)
