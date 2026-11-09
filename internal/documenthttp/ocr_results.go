package documenthttp

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func resultIDs(r *http.Request) bool {
	for _, name := range []string{"document_id", "result_id"} {
		value := r.PathValue(name)
		if !opaque(value) || len(value) > 96 {
			return false
		}
	}
	return r.URL.RawQuery == ""
}

func (h *Handler) ocrResult(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !resultIDs(r) {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.ReadOCRResult(r.Context(), p.ID, r.PathValue("document_id"), r.PathValue("result_id"))
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, value)
}

func (h *Handler) ocrResultArtifact(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if !resultIDs(r) {
		failure(w, documents.ErrInvalid)
		return
	}
	document, result, page, kind := r.PathValue("document_id"), r.PathValue("result_id"), r.PathValue("page_id"), r.PathValue("kind")
	if page == "" {
		kind = "output"
	} else if !opaque(page) || len(page) > 96 {
		failure(w, documents.ErrInvalid)
		return
	}
	// Only published comparison artifacts are addressable. Provider work files
	// and arbitrary kind/path selectors never gain filesystem authority.
	switch kind {
	case "output", "original", "effective", "text", "positions", "preview", "thumbnail", "layer":
	default:
		failure(w, documents.ErrUnavailable)
		return
	}
	resolve := func(ctx context.Context) (documents.ArtifactReference, error) {
		return h.store.ReadOCRResultArtifact(ctx, p.ID, document, result, page, kind)
	}
	reference, err := resolve(r.Context())
	if err != nil {
		failure(w, err)
		return
	}
	media := reference.Artifact.MediaType
	if media != "text/plain" && media != "application/json" && media != "application/pdf" && media != "image/png" && media != "image/jpeg" {
		failure(w, documents.ErrStorage)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	disposition := "inline"
	if kind == "output" {
		disposition = `attachment; filename="scan.pdf"`
	}
	h.stream(w, r, reference, resolve, disposition, media == "application/pdf")
}

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
	if uniqueJSON(data) != nil || json.Unmarshal(data, &fields) != nil || fields == nil || len(fields) != 0 {
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
