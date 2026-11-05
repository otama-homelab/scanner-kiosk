package documenthttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

// exactObject rejects casing aliases, unknown/duplicate keys and explicit nulls
// at every command-object boundary. uniqueJSON also rejects trailing content.
func exactObject(data []byte, allowed []string, required []string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return documents.ErrInvalid
	}
	known := map[string]bool{}
	for _, key := range allowed {
		known[key] = true
	}
	for key, raw := range fields {
		if !known[key] || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return documents.ErrInvalid
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return documents.ErrInvalid
		}
	}
	return nil
}
func (h *Handler) scan(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key := r.Header.Get("Idempotency-Key")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) {
		failure(w, documents.ErrInvalid)
		return
	}
	document := r.PathValue("document_id")
	request := documents.ScanRequest{DocumentID: document, IdempotencyKey: key}
	if document == "" {
		if len(r.Header.Values("If-Match")) != 0 {
			failure(w, documents.ErrInvalid)
			return
		}
	} else {
		match := r.Header.Get("If-Match")
		if !opaque(document) || len(document) > 96 || len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
			failure(w, documents.ErrInvalid)
			return
		}
		revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
		if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
			failure(w, documents.ErrInvalid)
			return
		}
		request.Revision = revision
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			write(w, 413, map[string]string{"code": "payload_too_large", "message": "Payload too large"})
		} else {
			failure(w, documents.ErrInvalid)
		}
		return
	}
	if err = uniqueJSON(data); err != nil {
		failure(w, err)
		return
	}
	allowed := []string{"scanner_id", "capture"}
	required := []string{"scanner_id", "capture"}
	if document == "" {
		allowed = append(allowed, "purpose", "photo_format")
		required = append(required, "purpose")
	} else {
		allowed = append(allowed, "insert_before_page_id", "replace_page_id")
	}
	if err = exactObject(data, allowed, required); err != nil {
		failure(w, err)
		return
	}
	var input struct {
		ScannerID          string          `json:"scanner_id"`
		Capture            json.RawMessage `json:"capture"`
		Purpose            string          `json:"purpose"`
		PhotoFormat        string          `json:"photo_format"`
		InsertBeforePageID string          `json:"insert_before_page_id"`
		ReplacePageID      string          `json:"replace_page_id"`
	}
	if err = json.Unmarshal(data, &input); err != nil {
		failure(w, documents.ErrInvalid)
		return
	}
	if err = exactObject(input.Capture, []string{"size", "color", "dpi"}, []string{"size", "color", "dpi"}); err != nil {
		failure(w, err)
		return
	}
	if json.Unmarshal(input.Capture, &request.Capture) != nil || !opaque(input.ScannerID) || len(input.ScannerID) > 96 {
		failure(w, documents.ErrInvalid)
		return
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if _, present := fields["photo_format"]; present && (input.Purpose != "photo" || input.PhotoFormat != "jpeg" && input.PhotoFormat != "png") {
		failure(w, documents.ErrInvalid)
		return
	}
	for _, field := range []string{"insert_before_page_id", "replace_page_id"} {
		raw, present := fields[field]
		if present {
			var value string
			if json.Unmarshal(raw, &value) != nil || !opaque(value) {
				failure(w, documents.ErrInvalid)
				return
			}
		}
	}
	if input.InsertBeforePageID != "" && input.ReplacePageID != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	request.InsertBeforePageID, request.ReplacePageID = input.InsertBeforePageID, input.ReplacePageID
	request.DeviceID, request.Purpose, request.PhotoFormat = input.ScannerID, input.Purpose, input.PhotoFormat
	admission, err := h.scanService.Admit(r.Context(), p.ID, request)
	if err != nil {
		failure(w, err)
		return
	}
	job, err := h.store.ReadJob(r.Context(), p.ID, admission.JobID)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", "/api/jobs/"+job.ID)
	write(w, 202, job)
}
