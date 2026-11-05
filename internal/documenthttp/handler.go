// Package documenthttp projects authorized document metadata through HTTP.
package documenthttp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"scanner-kiosk/internal/artifactread"
	"strconv"
	"strings"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanadmission"
	"scanner-kiosk/internal/websession"
)

type Handler struct {
	store           Records
	retention       time.Duration
	files           *artifactread.Root
	sessions        *websession.Handler
	transferTimeout time.Duration
	renderService   RenderAdmission
	scanService     ScanAdmission
	importService   ImportAdmission
}

func New(store Records, sessions *websession.Handler, retention time.Duration, files *artifactread.Root, transferTimeout time.Duration, renderService RenderAdmission, scanService ScanAdmission, importService ImportAdmission) http.Handler {
	h := &Handler{store: store, retention: retention, files: files, sessions: sessions, transferTimeout: transferTimeout, renderService: renderService, scanService: scanService, importService: importService}
	mux := http.NewServeMux()
	mux.Handle("POST /api/documents/import", sessions.RequirePrincipal(h.importPDF))
	mux.Handle("POST /api/documents", sessions.RequirePrincipal(h.scan))
	mux.Handle("POST /api/documents/{document_id}/scans", sessions.RequirePrincipal(h.scan))
	mux.Handle("GET /api/documents", sessions.RequirePrincipal(h.list))
	mux.Handle("DELETE /api/documents/{document_id}", sessions.RequirePrincipal(h.deleteDocument))
	mux.Handle("GET /api/documents/{document_id}", sessions.RequirePrincipal(h.get))
	mux.Handle("PATCH /api/documents/{document_id}/pages/{page_id}", sessions.RequirePrincipal(h.edit))
	mux.Handle("DELETE /api/documents/{document_id}/pages/{page_id}", sessions.RequirePrincipal(h.edit))
	mux.Handle("PUT /api/documents/{document_id}/pages/order", sessions.RequirePrincipal(h.edit))
	mux.Handle("GET /api/documents/{document_id}/pages/{page_id}/thumbnail", sessions.RequirePrincipal(h.thumbnail))
	mux.Handle("GET /api/documents/{document_id}/pages/{page_id}/preview", sessions.RequirePrincipal(h.preview))
	mux.Handle("GET /api/documents/{document_id}/download", sessions.RequirePrincipal(h.download))
	mux.Handle("GET /api/jobs/{job_id}", sessions.RequirePrincipal(h.job))
	mux.Handle("POST /api/jobs/{job_id}/cancel", sessions.RequirePrincipal(h.cancelJob))
	mux.Handle("POST /api/documents/{document_id}/render", sessions.RequirePrincipal(h.render))
	return mux
}
func write(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	status, code := 503, "storage_unavailable"
	if errors.Is(err, documents.ErrDeviceBusy) {
		status, code = 503, "scanner_unavailable"
	}
	if errors.Is(err, documents.ErrUnavailable) {
		status, code = 404, "not_found"
	}
	if errors.Is(err, documents.ErrInvalid) {
		status, code = 400, "invalid_request"
	}
	if errors.Is(err, documents.ErrRevision) {
		status, code = 412, "revision_changed"
	}
	if errors.Is(err, documents.ErrConflict) || errors.Is(err, documents.ErrIdempotencyConflict) {
		status, code = 409, "conflict"
	}
	if errors.Is(err, documents.ErrGone) {
		status, code = 410, "gone"
	}
	if errors.Is(err, websession.ErrSession) {
		status, code = 401, "session_required"
	}
	if errors.Is(err, documents.ErrQuota) {
		status, code = 429, "limit_exceeded"
	}
	if errors.Is(err, renderadmission.ErrProviderUnavailable) {
		status, code = 503, "provider_unavailable"
	}
	if errors.Is(err, renderadmission.ErrUnsupported) || errors.Is(err, documents.ErrOCRLimits) || errors.Is(err, scanadmission.ErrUnsupported) {
		status, code = 422, "unsupported_capability"
	}
	write(w, status, map[string]string{"code": code, "message": http.StatusText(status)})
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	value, err := h.store.ReadDocument(r.Context(), p.ID, r.PathValue("document_id"))
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(value.Revision, 10)))
	write(w, 200, value)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if len(r.URL.RawQuery) > 1024 {
		failure(w, documents.ErrInvalid)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, documents.ErrInvalid)
		return
	}
	limit := 20
	for key, values := range query {
		if key != "limit" && key != "cursor" || len(values) != 1 {
			failure(w, documents.ErrInvalid)
			return
		}
	}
	if raw, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(raw[0])
		if err != nil || limit < 1 || limit > 100 {
			failure(w, documents.ErrInvalid)
			return
		}
	}
	var after *documents.Position
	if values, ok := query["cursor"]; ok {
		raw := values[0]
		if len(raw) == 0 || len(raw) > 384 {
			failure(w, documents.ErrInvalid)
			return
		}
		decoded, e := base64.RawURLEncoding.Strict().DecodeString(raw)
		if e != nil {
			failure(w, documents.ErrInvalid)
			return
		}
		fields := strings.Split(string(decoded), ":")
		if len(fields) != 2 {
			failure(w, documents.ErrInvalid)
			return
		}
		expiry, e := strconv.ParseInt(fields[0], 10, 64)
		if e != nil {
			failure(w, documents.ErrInvalid)
			return
		}
		after = &documents.Position{ExpiresAtMS: expiry, ID: fields[1]}
	}
	values, next, err := h.store.ReadHistory(r.Context(), p.ID, limit, after)
	if err != nil {
		failure(w, err)
		return
	}
	var cursor *string
	if next != nil {
		s := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(next.ExpiresAtMS, 10) + ":" + next.ID))
		cursor = &s
	}
	write(w, 200, struct {
		Items []documents.Projection `json:"items"`
		Next  *string                `json:"next_cursor"`
	}{values, cursor})
}
