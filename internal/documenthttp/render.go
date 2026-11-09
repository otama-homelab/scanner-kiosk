package documenthttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
	"strconv"
	"unicode/utf8"
)

// uniqueJSON checks every object so nested duplicate command fields cannot be
// interpreted differently by validation, canonical hashing and execution.
func uniqueJSON(data []byte) error {
	if !utf8.Valid(data) {
		return documents.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 16 {
			return documents.ErrInvalid
		}
		token, err := decoder.Token()
		if err != nil {
			return documents.ErrInvalid
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return documents.ErrInvalid
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return documents.ErrInvalid
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return documents.ErrInvalid
			}
		case '[':
			for decoder.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return documents.ErrInvalid
			}
		default:
			return documents.ErrInvalid
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return documents.ErrInvalid
	}
	return nil
}
func (h *Handler) render(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	h.renderCommand(w, r, p, false)
}
func (h *Handler) reocr(w http.ResponseWriter, r *http.Request, p websession.Principal) {
	h.renderCommand(w, r, p, true)
}
func (h *Handler) renderCommand(w http.ResponseWriter, r *http.Request, p websession.Principal, reocr bool) {
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
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		write(w, 415, map[string]string{"code": "unsupported_media", "message": "JSON required"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
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
	var input struct {
		Format      string  `json:"format"`
		PDFEncoding *string `json:"pdf_encoding"`
		OCR         *struct {
			Enabled       *bool    `json:"enabled"`
			ProviderID    string   `json:"provider_id"`
			ModelID       string   `json:"model_id"`
			Languages     []string `json:"languages"`
			QualityPreset string   `json:"quality_preset"`
		} `json:"ocr"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil || input.OCR == nil || input.OCR.Enabled == nil {
		failure(w, documents.ErrInvalid)
		return
	}
	// Reject explicit nulls rather than treating them as absent optional values.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for name, raw := range fields {
		if name != "format" && name != "pdf_encoding" && name != "ocr" || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			failure(w, documents.ErrInvalid)
			return
		}
	}
	var ocrFields map[string]json.RawMessage
	_ = json.Unmarshal(fields["ocr"], &ocrFields)
	for name, raw := range ocrFields {
		known := name == "enabled" || name == "provider_id" || name == "model_id" || name == "languages" || name == "quality_preset"
		if !known || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || name == "languages" && len(input.OCR.Languages) == 0 {
			failure(w, documents.ErrInvalid)
			return
		}
	}
	settings := documents.RenderSettings{Format: input.Format, OCR: documents.RenderOCR{Enabled: *input.OCR.Enabled, ProviderID: input.OCR.ProviderID, ModelID: input.OCR.ModelID, Languages: input.OCR.Languages, QualityPreset: input.OCR.QualityPreset}}
	if reocr && (settings.Format != "pdf" || !settings.OCR.Enabled) {
		failure(w, documents.ErrInvalid)
		return
	}
	if input.PDFEncoding != nil {
		if *input.PDFEncoding != "jpeg" && *input.PDFEncoding != "lossless" {
			failure(w, documents.ErrInvalid)
			return
		}
		settings.PDFEncoding = *input.PDFEncoding
	}
	document := r.PathValue("document_id")
	if !opaque(document) || len(document) > 96 {
		failure(w, documents.ErrInvalid)
		return
	}
	admission, err := h.renderService.Admit(r.Context(), p.ID, documents.RenderAdmission{DocumentID: document, Revision: revision, IdempotencyKey: key, Settings: settings, ReOCR: reocr})
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
