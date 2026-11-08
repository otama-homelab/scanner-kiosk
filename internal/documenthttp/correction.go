package documenthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func (h *Handler) correction(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.ReadDocument(r.Context(), p.ID, r.PathValue("document_id"))
	if err != nil {
		failure(w, err)
		return
	}
	for _, page := range value.Pages {
		if page.ID == r.PathValue("page_id") && page.Correction != nil {
			w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
			write(w, 200, page.Correction)
			return
		}
	}
	failure(w, documents.ErrUnavailable)
}

func (h *Handler) correctionPreview(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	document, page := r.PathValue("document_id"), r.PathValue("page_id")
	reference, err := h.store.ReadCorrectionArtifact(r.Context(), p.ID, document, page)
	if err != nil {
		failure(w, err)
		return
	}
	h.stream(w, r, reference, func(ctx context.Context) (documents.ArtifactReference, error) {
		return h.store.ReadCorrectionArtifact(ctx, p.ID, document, page)
	}, "inline", false)
}

func (h *Handler) applyCorrection(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	request, ok := correctionMutation(w, r, true)
	if !ok {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		failure(w, documents.ErrInvalid)
		return
	}
	var input struct {
		SuggestionID string `json:"suggestion_id"`
		SourceSHA256 string `json:"source_sha256"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!opaque(input.SuggestionID) || len(input.SourceSHA256) != 64 {
		failure(w, documents.ErrInvalid)
		return
	}
	request.SuggestionID = input.SuggestionID
	request.SourceSHA256 = input.SourceSHA256
	value, err := h.store.ApplyCorrection(r.Context(), p.ID, request, h.retention)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
	write(w, 200, value)
}

func (h *Handler) undoCorrection(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	request, ok := correctionMutation(w, r, false)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(body) != 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.UndoCorrection(r.Context(), p.ID, request, h.retention)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
	write(w, 200, value)
}

func correctionMutation(w http.ResponseWriter, r *http.Request, apply bool) (documents.CorrectionRequest, bool) {
	key, match := r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match")
	if r.URL.RawQuery != "" || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) ||
		len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		failure(w, documents.ErrInvalid)
		return documents.CorrectionRequest{}, false
	}
	revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
	if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
		failure(w, documents.ErrInvalid)
		return documents.CorrectionRequest{}, false
	}
	request := documents.CorrectionRequest{
		DocumentID: r.PathValue("document_id"), PageID: r.PathValue("page_id"),
		Revision: revision, IdempotencyKey: key,
	}
	if !opaque(request.DocumentID) || len(request.DocumentID) > 96 || !opaque(request.PageID) || len(request.PageID) > 96 {
		failure(w, documents.ErrInvalid)
		return documents.CorrectionRequest{}, false
	}
	if !apply && len(r.Header.Values("Content-Type")) != 0 {
		failure(w, documents.ErrInvalid)
		return documents.CorrectionRequest{}, false
	}
	return request, true
}
