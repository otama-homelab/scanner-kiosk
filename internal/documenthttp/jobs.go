package documenthttp

import (
	"io"
	"net/http"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

func (h *Handler) job(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.ReadJob(r.Context(), p.ID, r.PathValue("job_id"))
	if err != nil {
		failure(w, err)
		return
	}
	write(w, 200, value)
}
func (h *Handler) cancelJob(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	key := r.Header.Get("Idempotency-Key")
	if r.URL.RawQuery != "" || len(r.Header.Values("Idempotency-Key")) != 1 || !opaque(key) {
		failure(w, documents.ErrInvalid)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(data) != 0 {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.CancelJob(r.Context(), p.ID, r.PathValue("job_id"), key, h.retention)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", "/api/jobs/"+value.ID)
	write(w, 202, value)
}
