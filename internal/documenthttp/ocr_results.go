package documenthttp

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func (h *Handler) adoptOCRResult(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	h.disposeOCRResult(w, r, p, true)
}
func (h *Handler) discardOCRResult(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	h.disposeOCRResult(w, r, p, false)
}

// A disposition always names one complete immutable bundle. No page selector or
// caller-selected filesystem/model authority is accepted by this command.
func (h *Handler) disposeOCRResult(w http.ResponseWriter, r *http.Request, p websession.Principal, adopt bool) {
	document, result := r.PathValue("document_id"), r.PathValue("result_id")
	key, match := r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match")
	if r.URL.RawQuery != "" || !opaque(document) || len(document) > 96 || !opaque(result) || len(result) > 96 || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		failure(w, documents.ErrInvalid)
		return
	}
	revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
	if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
		failure(w, documents.ErrInvalid)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			write(w, 413, map[string]string{"code": "payload_too_large", "message": "Payload too large"})
		} else {
			failure(w, documents.ErrInvalid)
		}
		return
	}
	// The URL and concurrency headers carry the entire command. An explicit empty
	// JSON object prevents ignored fields, duplicate keys and partial adoption.
	var fields map[string]json.RawMessage
	if uniqueJSON(data) != nil || json.Unmarshal(data, &fields) != nil || fields == nil || len(fields) != 0 || len(bytes.TrimSpace(data)) == 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	request := documents.OCRResultRequest{DocumentID: document, ResultID: result, Revision: revision, IdempotencyKey: key}
	var value documents.Projection
	if adopt {
		value, err = h.store.AdoptOCRResult(r.Context(), p.ID, request, h.retention)
	} else {
		value, err = h.store.DiscardOCRResult(r.Context(), p.ID, request, h.retention)
	}
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
	write(w, 200, value)
}
