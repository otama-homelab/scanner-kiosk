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

func opaque(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// exactlyOneField rejects unknown/duplicate fields, null, and trailing JSON.
func exactlyOneField(data []byte, name string, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return documents.ErrInvalid
	}
	token, err = d.Token()
	if err != nil || token != name {
		return documents.ErrInvalid
	}
	var raw json.RawMessage
	if err = d.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return documents.ErrInvalid
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return documents.ErrInvalid
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return documents.ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return documents.ErrInvalid
	}
	return nil
}
func (h *Handler) edit(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key := r.Header.Get("Idempotency-Key")
	match := r.Header.Get("If-Match")
	if r.URL.RawQuery != "" || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		failure(w, documents.ErrInvalid)
		return
	}
	revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
	if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
		failure(w, documents.ErrInvalid)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			write(w, 413, map[string]string{"code": "payload_too_large", "message": "Payload too large"})
		} else {
			failure(w, documents.ErrInvalid)
		}
		return
	}
	request := documents.EditRequest{DocumentID: r.PathValue("document_id"), PageID: r.PathValue("page_id"), Revision: revision, IdempotencyKey: key}
	if !opaque(request.DocumentID) || len(request.DocumentID) > 96 || request.PageID != "" && (!opaque(request.PageID) || len(request.PageID) > 96) {
		failure(w, documents.ErrInvalid)
		return
	}
	if r.Method == "DELETE" {
		if len(data) != 0 {
			failure(w, documents.ErrInvalid)
			return
		}
		request.Operation = "delete_page"
	} else {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
			write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
			return
		}
		if r.Method == "PATCH" {
			request.Operation = "rotate"
			err = exactlyOneField(data, "rotation", &request.Rotation)
		} else {
			request.Operation = "reorder"
			err = exactlyOneField(data, "page_ids", &request.PageIDs)
		}
		if err != nil {
			failure(w, err)
			return
		}
	}
	value, err := h.store.Edit(r.Context(), p.ID, request, h.retention)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
	write(w, 200, value)
}
