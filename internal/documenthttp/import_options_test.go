package documenthttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/websession"
)

type importOptionsRecords struct {
	Records
	received documents.ImportRequest
	calls    int
}

func (s *importOptionsRecords) Receive(_ context.Context, _ string, r documents.ImportRequest, body io.Reader) (documents.Admission, error) {
	s.received = r
	s.calls++
	return documents.Admission{JobID: "job_import_options001"}, nil
}
func (s *importOptionsRecords) ReadJob(context.Context, string, string) (documents.JobProjection, error) {
	return documents.JobProjection{ID: "job_import_options001"}, nil
}
func (s *importOptionsRecords) ReadImportReceipt(_ context.Context, _ string, r documents.ImportRequest) (documents.Admission, error) {
	s.received = r
	s.calls++
	return documents.Admission{JobID: "job_import_options001"}, nil
}
func TestImageImportOptionsHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, media, purpose, format string
		duplicate                    bool
		status                       int
	}{
		{"legacy", "image/jpeg", "", "", false, 202}, {"document", "image/png", "document", "", false, 202},
		{"photojpeg", "image/jpeg", "photo", "jpeg", false, 202}, {"photopng", "image/png", "photo", "png", false, 202}, {"defaultjpeg", "image/png", "photo", "", false, 202},
		{"pdfphoto", "application/pdf", "photo", "jpeg", false, 400}, {"documentformat", "image/png", "document", "png", false, 400}, {"whitespace", "image/png", "photo image", "", false, 400}, {"badformat", "image/png", "photo", "webp", false, 400}, {"duplicate", "image/png", "photo", "png", true, 400},
		{"duplicatepurpose", "image/png", "photo", "", false, 400},
		{"blankpurpose", "image/png", "", "", false, 400},
		{"blankformat", "image/png", "photo", "", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := &importOptionsRecords{}
			h := &Handler{store: records, importService: records}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.importPDF(w, r, websession.Principal{ID: "owner_options_0001"})
			}))
			defer server.Close()
			r, _ := http.NewRequest("POST", server.URL, strings.NewReader("image"))
			r.Header.Set("Content-Type", tc.media)
			r.Header.Set("Idempotency-Key", "request_import_options1")
			sum := sha256.Sum256([]byte("image"))
			r.Header.Set("X-Content-SHA256", hex.EncodeToString(sum[:]))
			if tc.purpose != "" {
				r.Header.Set("X-Document-Purpose", tc.purpose)
			}
			if tc.format != "" {
				r.Header.Set("X-Photo-Format", tc.format)
			}
			if tc.duplicate {
				r.Header.Add("X-Photo-Format", tc.format)
			}
			if tc.name == "duplicatepurpose" {
				r.Header.Add("X-Document-Purpose", "photo")
			}
			if tc.name == "blankpurpose" {
				r.Header.Set("X-Document-Purpose", "")
			}
			if tc.name == "blankformat" {
				r.Header.Set("X-Photo-Format", "")
			}
			response, e := server.Client().Do(r)
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatal(response.StatusCode, tc.status)
			}
			if tc.status == 202 {
				if records.calls != 1 || records.received.Purpose != tc.purpose || records.received.PhotoFormat != tc.format {
					t.Fatal("intent not passed", records.received)
				}
			} else if records.calls != 0 {
				t.Fatal("invalid metadata reached admission")
			}
		})
	}
}
func TestImageImportReconcileOptionsHTTP(t *testing.T) {
	for _, extra := range []string{``, `,"purpose":"document"`, `,"purpose":"photo","photo_format":"png"`, `,"purpose":"photo"`, `,"purpose":null`, `,"purpose":""`, `,"photo_format":"png"`, `,"purpose":"photo","photo_format":""`, `,"purpose":"photo","purpose":"document"`} {
		records := &importOptionsRecords{}
		h := &Handler{store: records}
		body := fmt.Sprintf(`{"media_type":"image/png","input_bytes":5,"sha256":"%s"%s}`, strings.Repeat("a", 64), extra)
		r := httptest.NewRequest("POST", "https://scanner.test/api/documents/import/reconcile", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "request_import_options1")
		w := httptest.NewRecorder()
		h.reconcileImport(w, r, websession.Principal{ID: "owner_options_0001"})
		valid := extra == `` || extra == `,"purpose":"document"` || extra == `,"purpose":"photo","photo_format":"png"` || extra == `,"purpose":"photo"`
		if valid {
			if w.Code != 200 || records.calls != 1 {
				t.Fatal(extra, w.Code, records.calls)
			}
			if strings.Contains(extra, `"photo"`) && records.received.Purpose != "photo" {
				t.Fatal("reconcile lost purpose")
			}
		} else if w.Code != 400 || records.calls != 0 {
			t.Fatal("invalid reconcile accepted", extra, w.Code, records.calls)
		}
	}
}

func TestImportOptionsRejectUntrimmedValues(t *testing.T) {
	for _, purpose := range []string{" photo", "photo ", "document ", "PHOTO"} {
		if validImportOptions("image/jpeg", purpose, "") {
			t.Fatal("invalid purpose", purpose)
		}
	}
	for _, format := range []string{" png", "png ", "JPEG"} {
		if validImportOptions("image/jpeg", "photo", format) {
			t.Fatal("invalid format", format)
		}
	}
}
