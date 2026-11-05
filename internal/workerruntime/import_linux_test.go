package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/importupload"
	"scanner-kiosk/internal/pdfimport"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__pdf-import" {
		os.Exit(pdfimport.Child(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
func importFixturePDF() []byte {
	content := []string{"BT /F1 12 Tf 20 30 Td (IMPORT_ALPHA) Tj ET\n0 0 1 rg 10 10 20 20 re f\n", "BT /F1 12 Tf 20 30 Td (IMPORT_BETA) Tj ET\n1 0 0 rg 10 10 20 20 re f\n"}
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content[0]), content[0]), "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 250] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content[1]), content[1]), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}
func TestWorkerImportsPDFThroughBoundedChild(t *testing.T) {
	for _, mode := range []string{"valid", "malformed", "corrupt-input", "insufficient-reservation"} {
		t.Run(mode, func(t *testing.T) {
			c, store, db, root := fixture(t)
			raw := importFixturePDF()
			if mode == "malformed" {
				raw = []byte("not a PDF")
			}
			upload, err := importupload.New(root, store, documents.ImportLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer upload.Close()
			a, err := upload.Receive(context.Background(), owner, documents.ImportRequest{IdempotencyKey: "request_import_run1", InputBytes: int64(len(raw)), SHA256: sha256.Sum256(raw)}, bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			var source string
			if err = db.QueryRow("SELECT a.relative_path FROM artifacts a JOIN import_intakes i ON i.artifact_id=a.id WHERE i.job_id=?", a.JobID).Scan(&source); err != nil {
				t.Fatal(err)
			}
			if mode == "corrupt-input" {
				raw = append([]byte(nil), raw...)
				raw[len(raw)-1] ^= 1
				if err = os.WriteFile(filepath.Join(root, source), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "insufficient-reservation" {
				if _, err = db.Exec("UPDATE reservations SET byte_size=1 WHERE job_id=?", a.JobID); err != nil {
					t.Fatal(err)
				}
			}
			if err = Run(context.Background(), c, true); err != nil {
				t.Fatal(err)
			}
			job, err := store.ReadJob(context.Background(), owner, a.JobID)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := store.ReadDocument(context.Background(), owner, a.DocumentID)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(root, source))
			if err != nil || !bytes.Equal(saved, raw) {
				t.Fatal("uploaded original changed", err)
			}
			var count int
			if err = db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", a.JobID).Scan(&count); err != nil || count != 0 {
				t.Fatal("import reservation retained", count, err)
			}
			if mode != "valid" {
				if job.State != "failed" || len(projection.Pages) != 0 {
					t.Fatal("bad input not rejected", job, projection)
				}
				return
			}
			if job.State != "succeeded" || len(projection.Pages) != 2 || projection.State != "review" {
				t.Fatal("PDF not imported", job, projection)
			}
			for i, page := range projection.Pages {
				if page.Capture != nil || page.PDF == nil || page.SourceType != "pdf" {
					t.Fatal("false raster page", page)
				}
				var path string
				if err = db.QueryRow("SELECT a.relative_path FROM pages p JOIN artifacts a ON a.id=p.original_artifact_id WHERE p.id=?", page.ID).Scan(&path); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(data), model.NewStatelessConfiguration(), nil)
				if err != nil || parsed.PageCount != 1 {
					t.Fatal("invalid output page", err)
				}
				dict, _, _, err := parsed.PageDict(context.Background(), 1, false)
				if err != nil {
					t.Fatal(err)
				}
				content, err := parsed.PageContent(dict, 1)
				word := "IMPORT_ALPHA"
				if i == 1 {
					word = "IMPORT_BETA"
				}
				if err != nil || !bytes.Contains(content, []byte(word)) || !bytes.Contains(content, []byte(" re f")) {
					t.Fatal("lost text/vector", word, err)
				}
			}
		})
	}
}
