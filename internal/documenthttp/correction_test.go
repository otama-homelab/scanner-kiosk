package documenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

const (
	correctionDocumentID = "document_correction_001"
	correctionPageID     = "page_correction_http01"
	correctionRequestKey = "request_correction_http_001"
)

func TestCorrectionHTTP(t *testing.T) {
	records := correctionMemory(t)
	handler := &Handler{store: records, retention: time.Hour}
	request := httptest.NewRequest("POST",
		"https://scanner.example.test/api/documents/"+correctionDocumentID+"/pages/"+correctionPageID+"/correction",
		strings.NewReader(`{"suggestion_id":"suggestion_memory_001","source_sha256":"`+strings.Repeat("a", 64)+`"}`))
	request.SetPathValue("document_id", correctionDocumentID)
	request.SetPathValue("page_id", correctionPageID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", correctionRequestKey)
	request.Header.Set("If-Match", `"7"`)
	response := httptest.NewRecorder()
	handler.applyCorrection(response, request, websession.Principal{ID: records.owner})
	if response.Code != 200 || records.correction.SuggestionID != "suggestion_memory_001" ||
		records.correction.Revision != 7 || response.Header().Get("ETag") != `"7"` {
		t.Fatalf("apply response=%d headers=%v request=%+v body=%s", response.Code, response.Header(), records.correction, response.Body.String())
	}
	undo := httptest.NewRequest("DELETE",
		"https://scanner.example.test/api/documents/"+correctionDocumentID+"/pages/"+correctionPageID+"/correction", nil)
	undo.SetPathValue("document_id", correctionDocumentID)
	undo.SetPathValue("page_id", correctionPageID)
	undo.Header.Set("Idempotency-Key", correctionRequestKey)
	undo.Header.Set("If-Match", `"7"`)
	response = httptest.NewRecorder()
	handler.undoCorrection(response, undo, websession.Principal{ID: records.owner})
	if response.Code != 200 || records.correction.SuggestionID != "" {
		t.Fatalf("undo response=%d request=%+v", response.Code, records.correction)
	}
}

func TestCorrectionStale(t *testing.T) {
	records := correctionMemory(t)
	records.failure = documents.ErrRevision
	handler := &Handler{store: records, retention: time.Hour}
	request := httptest.NewRequest("POST",
		"https://scanner.example.test/api/documents/"+correctionDocumentID+"/pages/"+correctionPageID+"/correction",
		strings.NewReader(`{"suggestion_id":"suggestion_memory_001","source_sha256":"`+strings.Repeat("a", 64)+`"}`))
	request.SetPathValue("document_id", correctionDocumentID)
	request.SetPathValue("page_id", correctionPageID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", correctionRequestKey)
	request.Header.Set("If-Match", `"7"`)
	response := httptest.NewRecorder()
	handler.applyCorrection(response, request, websession.Principal{ID: records.owner})
	if response.Code != 412 {
		t.Fatalf("stale correction status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCorrectionUnknown(t *testing.T) {
	records := correctionMemory(t)
	records.failure = errors.New("unknown result")
	handler := &Handler{store: records, retention: time.Hour}
	request := httptest.NewRequest("DELETE",
		"https://scanner.example.test/api/documents/"+correctionDocumentID+"/pages/"+correctionPageID+"/correction", nil)
	request.SetPathValue("document_id", correctionDocumentID)
	request.SetPathValue("page_id", correctionPageID)
	request.Header.Set("Idempotency-Key", correctionRequestKey)
	request.Header.Set("If-Match", `"7"`)
	response := httptest.NewRecorder()
	handler.undoCorrection(response, request, websession.Principal{ID: records.owner})
	if response.Code != 503 || records.calls != 1 {
		t.Fatalf("unknown correction retried or misclassified: status=%d calls=%d", response.Code, records.calls)
	}
	var body map[string]string
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || body["code"] != "storage_unavailable" {
		t.Fatalf("unknown correction leaked detail: %s", response.Body.String())
	}
}

func correctionMemory(t *testing.T) *correctionRecords {
	t.Helper()
	return &correctionRecords{
		owner: correctionDocumentID,
		projection: documents.Projection{
			ID: correctionDocumentID, Revision: 7, Purpose: "document", State: "review",
			ExpiresAt: time.Now().Add(time.Hour), Pages: []documents.PageProjection{{ID: correctionPageID}},
		},
		reference: documents.ArtifactReference{
			Artifact:    documents.ArtifactRecord{ID: "artifact_correction_memory_001", MediaType: "image/png", RelativePath: "correction.png", Size: 20},
			ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
		},
	}
}

type correctionRecords struct {
	owner      string
	projection documents.Projection
	reference  documents.ArtifactReference
	correction documents.CorrectionRequest
	failure    error
	calls      int
}

func (r *correctionRecords) allow(owner string) error {
	r.calls++
	if owner != r.owner {
		return documents.ErrUnavailable
	}
	return r.failure
}
func (r *correctionRecords) ReadDocument(_ context.Context, owner, _ string) (documents.Projection, error) {
	return r.projection, r.allow(owner)
}
func (r *correctionRecords) ReadHistory(context.Context, string, int, *documents.Position) ([]documents.Projection, *documents.Position, error) {
	return nil, nil, documents.ErrUnavailable
}
func (r *correctionRecords) ReadJob(context.Context, string, string) (documents.JobProjection, error) {
	return documents.JobProjection{}, documents.ErrUnavailable
}
func (r *correctionRecords) CancelJob(context.Context, string, string, string, time.Duration) (documents.JobProjection, error) {
	return documents.JobProjection{}, documents.ErrUnavailable
}
func (r *correctionRecords) Edit(context.Context, string, documents.EditRequest, time.Duration) (documents.Projection, error) {
	return documents.Projection{}, documents.ErrUnavailable
}
func (r *correctionRecords) RequestDelete(context.Context, string, documents.DeleteRequest, time.Duration) (documents.JobProjection, error) {
	return documents.JobProjection{}, documents.ErrUnavailable
}
func (r *correctionRecords) ReadPageOriginal(context.Context, string, string, string) (documents.PageOriginal, error) {
	return documents.PageOriginal{}, documents.ErrUnavailable
}
func (r *correctionRecords) ReadCorrectionArtifact(context.Context, string, string, string) (documents.ArtifactReference, error) {
	return r.reference, nil
}
func (r *correctionRecords) ReadPageThumbnail(context.Context, string, string, string) (documents.ArtifactReference, error) {
	return documents.ArtifactReference{}, documents.ErrUnavailable
}
func (r *correctionRecords) ReadCurrentOutput(context.Context, string, string) (documents.ArtifactReference, error) {
	return documents.ArtifactReference{}, documents.ErrUnavailable
}
func (r *correctionRecords) ApplyCorrection(_ context.Context, owner string, request documents.CorrectionRequest, _ time.Duration) (documents.Projection, error) {
	r.correction = request
	return r.projection, r.allow(owner)
}
func (r *correctionRecords) UndoCorrection(_ context.Context, owner string, request documents.CorrectionRequest, _ time.Duration) (documents.Projection, error) {
	r.correction = request
	return r.projection, r.allow(owner)
}
