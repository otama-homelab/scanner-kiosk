package documenthttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

// preview streams source orientation. UI rotation comes from page metadata.
func (h *Handler) preview(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	document, page := r.PathValue("document_id"), r.PathValue("page_id")
	reference, err := h.store.ReadPageOriginal(r.Context(), p.ID, document, page)
	if err != nil {
		failure(w, err)
		return
	}
	h.stream(w, r, reference, func(ctx context.Context) (documents.ArtifactReference, error) {
		return h.store.ReadPageOriginal(ctx, p.ID, document, page)
	}, "inline", false)
}

// thumbnail streams a sealed source-orientation derivative using the same
// repeated authorization, expiry and descriptor checks as the original preview.
func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	document, page := r.PathValue("document_id"), r.PathValue("page_id")
	reference, err := h.store.ReadPageThumbnail(r.Context(), p.ID, document, page)
	if err != nil {
		failure(w, err)
		return
	}
	h.stream(w, r, reference, func(ctx context.Context) (documents.ArtifactReference, error) {
		return h.store.ReadPageThumbnail(ctx, p.ID, document, page)
	}, "inline", false)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	if r.URL.RawQuery != "" {
		failure(w, documents.ErrInvalid)
		return
	}
	document := r.PathValue("document_id")
	reference, err := h.store.ReadCurrentOutput(r.Context(), p.ID, document)
	if err != nil {
		failure(w, err)
		return
	}
	var name string
	switch reference.Artifact.MediaType {
	case "application/pdf":
		name = "scan.pdf"
	case "image/jpeg":
		name = "scan.jpg"
	case "image/png":
		name = "scan.png"
	default:
		failure(w, documents.ErrStorage)
		return
	}
	h.stream(w, r, reference, func(ctx context.Context) (documents.ArtifactReference, error) {
		return h.store.ReadCurrentOutput(ctx, p.ID, document)
	}, "attachment; filename=\""+name+"\"", true)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, reference documents.ArtifactReference, resolve func(context.Context) (documents.ArtifactReference, error), disposition string, ranges bool) {
	start, length, status := int64(0), reference.Artifact.Size, 200
	if ranges {
		w.Header().Set("Accept-Ranges", "bytes")
		// Without a matching representation validator, If-Range falls back to full GET.
		if raw, exists := r.Header["Range"]; exists && r.Method == "GET" && len(r.Header.Values("If-Range")) == 0 {
			var err error
			if len(raw) != 1 {
				err = errRange
			} else {
				start, length, err = byteRange(raw[0], reference.Artifact.Size)
			}
			if err != nil {
				w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(reference.Artifact.Size, 10))
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(416)
				return
			}
			status = 206
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	file, err := h.files.OpenRange(ctx, artifactread.Input{RelativePath: reference.Artifact.RelativePath, Size: reference.Artifact.Size, SHA256: reference.Artifact.SHA256}, start, length)
	if err != nil {
		failure(w, documents.ErrStorage)
		return
	}
	defer file.Close()
	// Hash verification may take time; repeat authority after it, before headers.
	check := func(ctx context.Context) error {
		if err := h.sessions.CheckAuthorization(ctx); err != nil {
			return err
		}
		current, err := resolve(ctx)
		if err != nil {
			return err
		}
		if current != reference {
			return documents.ErrUnavailable
		}
		return nil
	}
	if err = check(ctx); err != nil {
		failure(w, err)
		return
	}
	controller := http.NewResponseController(w)
	deadline := minTime(time.Now().Add(h.transferTimeout), time.UnixMilli(reference.ExpiresAtMS))
	if d, ok := ctx.Deadline(); ok {
		deadline = minTime(deadline, d)
	}
	// A blocked network write must not outlive expiry. Production net/http
	// supports write deadlines; unsupported response wrappers fail closed.
	if err = controller.SetWriteDeadline(deadline); err != nil {
		failure(w, documents.ErrStorage)
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
				err := check(probe)
				stop()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					cancel()
					_ = controller.SetWriteDeadline(time.Now())
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	w.Header().Set("Content-Type", reference.Artifact.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Disposition", disposition)
	if status == 206 {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(start+length-1, 10)+"/"+strconv.FormatInt(reference.Artifact.Size, 10))
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	if r.Method == "HEAD" {
		w.WriteHeader(status)
		return
	}
	w.WriteHeader(status)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			// Reauthorize every bounded chunk as well as while writes are blocked.
			if err = check(ctx); err != nil {
				panic(http.ErrAbortHandler)
			}
			if written, e := w.Write(buffer[:n]); e != nil || written != n {
				panic(http.ErrAbortHandler)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
