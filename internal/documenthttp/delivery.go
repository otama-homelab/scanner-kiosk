package documenthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/paperless"
	"scanner-kiosk/internal/websession"
)

// DeliveryService holds GET-only provider functions. Upload authority stays in the worker.
type DeliveryService struct {
	Records                          DeliveryRecords
	DestinationID, ConfigFingerprint string
	Task                             func(context.Context, string, string) (paperless.Task, error)
	Metadata                         func(context.Context, int64, [32]byte) error
}

type deliveryProjection struct {
	ID                          string `json:"id"`
	DocumentID                  string `json:"document_id"`
	Revision                    int64  `json:"revision"`
	Destination                 string `json:"destination"`
	State                       string `json:"state"`
	CreatedAtMS                 int64  `json:"created_at_ms"`
	ExpiresAtMS                 int64  `json:"expires_at_ms"`
	AttemptedAtMS               int64  `json:"attempted_at_ms,omitempty"`
	RemoteProcessingMayContinue bool   `json:"remote_processing_may_continue"`
}

func projectDelivery(d documents.Delivery) deliveryProjection {
	return deliveryProjection{d.ID, d.DocumentID, d.Revision, "paperless", d.State, d.CreatedAtMS, d.ExpiresAtMS, d.AttemptedAtMS, d.AttemptedAtMS != 0 && d.State != "confirmed" && d.State != "failed"}
}

// deliveryJSON admits only exact field spellings and non-null object values.
func deliveryJSON(w http.ResponseWriter, r *http.Request, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			write(w, 413, map[string]string{"code": "payload_too_large", "message": "Payload too large"})
		} else {
			failure(w, documents.ErrInvalid)
		}
		return nil, false
	}
	var fields map[string]json.RawMessage
	if uniqueJSON(data) != nil || json.Unmarshal(data, &fields) != nil || fields == nil {
		failure(w, documents.ErrInvalid)
		return nil, false
	}
	for k, v := range fields {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			failure(w, documents.ErrInvalid)
			return nil, false
		}
	}
	return fields, true
}
func (h *Handler) deliveryReady(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return false
	}
	if h.deliveryService == nil {
		failure(w, documents.ErrUnavailable)
		return false
	}
	return true
}
func (h *Handler) deliver(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !h.deliveryReady(w, r) {
		return
	}
	key, match := r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match")
	if len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		failure(w, documents.ErrInvalid)
		return
	}
	revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
	if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
		failure(w, documents.ErrInvalid)
		return
	}
	fields, ok := deliveryJSON(w, r, map[string]bool{"destination": true, "confirmed": true})
	if !ok {
		return
	}
	var dest string
	var confirmed bool
	if len(fields) != 2 || json.Unmarshal(fields["destination"], &dest) != nil || dest != "paperless" || json.Unmarshal(fields["confirmed"], &confirmed) != nil || !confirmed || !opaque(r.PathValue("document_id")) {
		failure(w, documents.ErrInvalid)
		return
	}
	s := h.deliveryService
	d, err := s.Records.AdmitDelivery(r.Context(), p.ID, documents.DeliveryRequest{DocumentID: r.PathValue("document_id"), Revision: revision, IdempotencyKey: key, DestinationID: s.DestinationID, ConfigFingerprint: s.ConfigFingerprint})
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", "/api/deliveries/"+d.ID)
	write(w, 202, projectDelivery(d))
}
func (h *Handler) currentDelivery(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !h.deliveryReady(w, r) {
		return
	}
	d, err := h.deliveryService.Records.ReadCurrentDelivery(r.Context(), p.ID, r.PathValue("document_id"), h.deliveryService.DestinationID)
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, projectDelivery(d))
}
func (h *Handler) readDelivery(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !h.deliveryReady(w, r) {
		return
	}
	d, err := h.deliveryService.Records.ReadDelivery(r.Context(), p.ID, r.PathValue("delivery_id"))
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, projectDelivery(d))
}
func (h *Handler) cancelDelivery(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !h.deliveryReady(w, r) {
		return
	}
	if _, ok := deliveryJSON(w, r, nil); !ok {
		return
	}
	d, err := h.deliveryService.Records.CancelDelivery(r.Context(), p.ID, r.PathValue("delivery_id"))
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, projectDelivery(d))
}
func (h *Handler) reconcileDelivery(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !h.deliveryReady(w, r) {
		return
	}
	fields, ok := deliveryJSON(w, r, map[string]bool{"task_id": true})
	if !ok {
		return
	}
	var taskID string
	if raw, exists := fields["task_id"]; exists {
		if json.Unmarshal(raw, &taskID) != nil || !paperless.ValidTaskID(taskID) {
			failure(w, documents.ErrInvalid)
			return
		}
	}
	s := h.deliveryService
	d, err := s.Records.ReadDelivery(r.Context(), p.ID, r.PathValue("delivery_id"))
	if err != nil {
		failure(w, err)
		return
	}
	if taskID == "" {
		taskID = d.TaskID
	}
	if d.DestinationID != s.DestinationID || d.ConfigFingerprint != s.ConfigFingerprint || s.Task == nil || s.Metadata == nil || d.State != "unknown" && d.State != "accepted" && d.State != "attempted" || taskID == "" || d.TaskID != "" && d.TaskID != taskID {
		failure(w, documents.ErrConflict)
		return
	}
	task, remoteErr := s.Task(r.Context(), taskID, d.Filename)
	if remoteErr == nil && task.Status == "SUCCESS" {
		remoteErr = s.Metadata(r.Context(), *task.RelatedDocument, d.Artifact.SHA256)
	}
	if remoteErr != nil {
		err = s.Records.MarkDeliveryUnknown(r.Context(), p.ID, d.ID)
	} else {
		err = s.Records.AcceptDelivery(r.Context(), p.ID, d.ID, taskID)
		if err == nil && task.Status == "SUCCESS" {
			err = s.Records.ConfirmDelivery(r.Context(), p.ID, d.ID, taskID, *task.RelatedDocument, d.Artifact.SHA256)
		}
		if err == nil && (task.Status == "FAILURE" || task.Status == "REVOKED") {
			err = s.Records.FailDelivery(r.Context(), p.ID, d.ID, taskID)
		}
	}
	if err != nil {
		failure(w, err)
		return
	}
	d, err = s.Records.ReadDelivery(r.Context(), p.ID, d.ID)
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, projectDelivery(d))
}
