package documenthttp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"strings"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func (h *Handler) importPDF(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key := r.Header.Get("Idempotency-Key")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 0 || len(r.Header.Values("Content-Encoding")) != 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (media != "application/pdf" && media != "image/jpeg" && media != "image/png") || len(params) != 0 || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JPEG, PNG or PDF required"})
		return
	}
	purpose, format := r.Header.Get("X-Document-Purpose"), r.Header.Get("X-Photo-Format")
	if len(r.Header.Values("X-Document-Purpose")) > 1 || len(r.Header.Values("X-Photo-Format")) > 1 ||
		len(r.Header.Values("X-Document-Purpose")) == 1 && purpose == "" || len(r.Header.Values("X-Photo-Format")) == 1 && format == "" || !validImportOptions(media, purpose, format) {
		failure(w, documents.ErrInvalid)
		return
	}
	if r.ContentLength < 0 {
		write(w, 411, map[string]string{"code": "length_required", "message": "Content-Length required"})
		return
	}
	if r.ContentLength < 1 || r.ContentLength > 1<<30 {
		failure(w, documents.ErrInvalid)
		return
	}
	raw := r.Header.Get("X-Content-SHA256")
	hash, err := hex.DecodeString(raw)
	if err != nil || len(hash) != 32 || raw != strings.ToLower(raw) || len(r.Header.Values("X-Content-SHA256")) != 1 {
		failure(w, documents.ErrInvalid)
		return
	}
	if h.importService == nil {
		failure(w, documents.ErrStorage)
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	if bound, ok := r.Context().Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	if err = http.NewResponseController(w).SetReadDeadline(deadline); err != nil {
		failure(w, documents.ErrStorage)
		return
	}
	request := documents.ImportRequest{IdempotencyKey: key, InputBytes: r.ContentLength, MediaType: media, Purpose: purpose, PhotoFormat: format}
	copy(request.SHA256[:], hash)
	a, err := h.importService.Receive(r.Context(), p.ID, request, r.Body)
	if err != nil {
		failure(w, err)
		return
	}
	job, err := h.store.ReadJob(r.Context(), p.ID, a.JobID)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", "/api/jobs/"+job.ID)
	write(w, 202, job)
}

// This is an identity lookup, never another upload/admission.
func (h *Handler) reconcileImport(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key := r.Header.Get("Idempotency-Key")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 0 || len(r.Header.Values("Content-Encoding")) != 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	var body struct {
		MediaType   string `json:"media_type"`
		InputBytes  int64  `json:"input_bytes"`
		SHA256      string `json:"sha256"`
		Purpose     string `json:"purpose"`
		PhotoFormat string `json:"photo_format"`
	}
	fields, ok := deliveryJSON(w, r, map[string]bool{"media_type": true, "input_bytes": true, "sha256": true, "purpose": true, "photo_format": true})
	if !ok {
		return
	}
	if json.Unmarshal(fields["media_type"], &body.MediaType) != nil || json.Unmarshal(fields["input_bytes"], &body.InputBytes) != nil || json.Unmarshal(fields["sha256"], &body.SHA256) != nil {
		failure(w, documents.ErrInvalid)
		return
	}
	if value, exists := fields["purpose"]; exists && (json.Unmarshal(value, &body.Purpose) != nil || body.Purpose == "") {
		failure(w, documents.ErrInvalid)
		return
	}
	if value, exists := fields["photo_format"]; exists && (json.Unmarshal(value, &body.PhotoFormat) != nil || body.PhotoFormat == "") {
		failure(w, documents.ErrInvalid)
		return
	}
	if !validImportOptions(body.MediaType, body.Purpose, body.PhotoFormat) {
		failure(w, documents.ErrInvalid)
		return
	}
	digest, err := hex.DecodeString(body.SHA256)
	if err != nil || len(digest) != 32 || body.SHA256 != strings.ToLower(body.SHA256) {
		failure(w, documents.ErrInvalid)
		return
	}
	reader, ok := h.store.(interface {
		ReadImportReceipt(context.Context, string, documents.ImportRequest) (documents.Admission, error)
	})
	if !ok {
		failure(w, documents.ErrStorage)
		return
	}
	request := documents.ImportRequest{IdempotencyKey: key, MediaType: body.MediaType, InputBytes: body.InputBytes, Purpose: body.Purpose, PhotoFormat: body.PhotoFormat}
	copy(request.SHA256[:], digest)
	receipt, err := reader.ReadImportReceipt(r.Context(), p.ID, request)
	if err != nil {
		failure(w, err)
		return
	}
	job, err := h.store.ReadJob(r.Context(), p.ID, receipt.JobID)
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, job)
}

func validImportOptions(media, purpose, format string) bool {
	if purpose == "" || purpose == "document" {
		return format == ""
	}
	return purpose == "photo" && (media == "image/jpeg" || media == "image/png") && (format == "" || format == "jpeg" || format == "png")
}
