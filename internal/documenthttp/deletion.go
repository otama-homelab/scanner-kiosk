package documenthttp

import (
	"io"
	"net/http"
	"strconv"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key, match := r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match")
	if r.URL.RawQuery != "" || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) || len(r.Header.Values("If-Match")) != 1 || len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		failure(w, documents.ErrInvalid)
		return
	}
	revision, err := strconv.ParseInt(match[1:len(match)-1], 10, 64)
	if err != nil || revision < 1 || match != strconv.Quote(strconv.FormatInt(revision, 10)) {
		failure(w, documents.ErrInvalid)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.RequestDelete(r.Context(), p.ID, documents.DeleteRequest{DocumentID: r.PathValue("document_id"), Revision: revision, IdempotencyKey: key}, h.retention)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", "/api/jobs/"+value.ID)
	write(w, 202, value)
}
