package documenthttp

import (
	"encoding/hex"
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
	if err != nil || media != "application/pdf" || len(params) != 0 || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "PDF required"})
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
	request := documents.ImportRequest{IdempotencyKey: key, InputBytes: r.ContentLength}
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
