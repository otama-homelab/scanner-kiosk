//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	ocrruntime "scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/internal/pdfassemble"
)

// This invokes the compiled product command, not a substituted executor. All
// documents are synthetic; physical scanning/HTTP/production are not exercised.
func TestInstalledWorkerOCRCommand(t *testing.T) {
	tess := os.Getenv("SCANNER_TEST_TESSERACT_BINARY")
	if tess == "" {
		t.Skip("requires installed Tesseract and synthetic fixture")
	}
	dataDir := os.Getenv("SCANNER_TEST_TESSDATA")
	t.Setenv("SCANNER_OCR_TESSERACT_PATH", tess)
	t.Setenv("SCANNER_OCR_TESSERACT_DATA_DIR", dataDir)
	t.Setenv("SCANNER_OCR_PLUGINS", "tesseract")
	t.Setenv("SCANNER_OCR_DEFAULT_PLUGIN", "tesseract")
	t.Setenv("SCANNER_OCR_DEFAULT_LANGUAGES", "ja,en")
	c, store, docs, root := fixture(t)
	source, err := os.ReadFile(os.Getenv("SCANNER_TEST_OCR_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	geometry, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	execute(t, docs, "DELETE FROM pages WHERE document_id=?", document)
	execute(t, docs, "DELETE FROM artifacts WHERE document_id=?", document)
	digest := sha256.Sum256(source)
	now := time.Now().UnixMilli()
	for i, name := range []string{"original.png", "second.png"} {
		if err = os.WriteFile(filepath.Join(root, name), source, 0600); err != nil {
			t.Fatal(err)
		}
		original, page := "original_runtime_001", "page_runtime_00001"
		if i == 1 {
			original, page = "original_runtime_002", "page_runtime_00002"
		}
		execute(t, docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',?,?,1,?)`, original, document, owner, name, len(source), digest[:], now)
		execute(t, docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation) VALUES(?,?,?,?,?,'a4','color',300,?)`, page, document, owner, i, original, i*90)
	}
	metadataRoot := filepath.Join(root, "metadata-native")
	if err = os.Mkdir(metadataRoot, 0700); err != nil {
		t.Fatal(err)
	}
	metadata, err := ocrruntime.NewWorker(context.Background(), c, os.LookupEnv, metadataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defaults := metadata.Defaults()
	metadata.Close()
	if err = os.Remove(metadataRoot); err != nil {
		t.Fatal(err)
	}
	admitted, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 1, IdempotencyKey: "request_worker_ocr_001", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: defaults.PluginID, ModelID: defaults.ModelID, ModelVersion: defaults.ModelVersion, Languages: defaults.Languages, QualityPreset: defaults.QualityPreset}}, ReserveBytes: 64 << 20}, documents.AdmissionLimits{UserMaxBytes: 256 << 20, GlobalMaxBytes: 512 << 20, JobMaxBytes: 128 << 20, MaxPages: 50, JobTimeout: 2 * time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("SCANNER_TEST_STANDALONE_BINARY")
	standalone := binary != ""
	if standalone {
		if !filepath.IsAbs(binary) {
			t.Fatal("standalone fixture must be an absolute path")
		}
	} else {
		binary = filepath.Join(t.TempDir(), "scanner")
		build := exec.Command("go", "build", "-o", binary, "../../cmd/scanner")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v: %s", err, output)
		}
	}
	command := exec.Command(binary, "worker", "--once")
	command.Env = []string{"SCANNER_STATE_DIR=" + c.StateDir, "SCANNER_DOCUMENT_DIR=" + root, "SCANNER_OCR_PLUGINS=tesseract", "SCANNER_OCR_DEFAULT_PLUGIN=tesseract", "SCANNER_OCR_DEFAULT_LANGUAGES=ja,en"}
	if standalone {
		command.Env = append(command.Env, "PATH=/nonexistent", "XDG_CACHE_HOME="+t.TempDir())
	} else {
		command.Env = append(command.Env, "PATH="+os.Getenv("PATH"), "SCANNER_OCR_TESSERACT_PATH="+tess, "SCANNER_OCR_TESSERACT_DATA_DIR="+dataDir)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("worker command: %v: %s", err, output)
	}
	exported, err := store.GetExport(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(root, exported.RelativePath)
	for _, number := range []string{"1", "2"} {
		text, err := exec.Command("pdftotext", "-f", number, "-l", number, "-raw", finalPath, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		compact := strings.Join(strings.Fields(string(text)), "")
		if !strings.Contains(compact, "日本語の書類") || !strings.Contains(compact, "ScannerOCR2026") {
			t.Fatal("worker PDF missing expected OCR text")
		}
	}
	var state string
	var reservations int
	if err = docs.QueryRow("SELECT state FROM jobs WHERE id=?", admitted.JobID).Scan(&state); err != nil || state != "succeeded" {
		t.Fatal("OCR job did not succeed")
	}
	if err = docs.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", admitted.JobID).Scan(&reservations); err != nil || reservations != 0 {
		t.Fatal("successful OCR reservation retained")
	}
	native, err := os.ReadDir(filepath.Join(root, ".native"))
	if err != nil || len(native) != 0 {
		t.Fatal("native staging retained")
	}
	rows, err := docs.Query(`SELECT a.relative_path,a.byte_size,a.sha256 FROM job_artifacts j JOIN artifacts a ON a.id=j.artifact_id WHERE j.job_id=? AND a.state='ready'`, admitted.JobID)
	if err != nil {
		t.Fatal(err)
	}
	expectedFiles := map[string]bool{}
	expectedScopes := map[string]bool{}
	for rows.Next() {
		var relative string
		var size int64
		var want []byte
		if err = rows.Scan(&relative, &size, &want); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		data, readErr := os.ReadFile(filepath.Join(root, relative))
		hash := sha256.Sum256(data)
		if readErr != nil || int64(len(data)) != size || !bytes.Equal(hash[:], want) {
			rows.Close()
			t.Fatal("published bytes differ from stored artifact")
		}
		expectedFiles[relative] = true
		expectedScopes[filepath.Dir(relative)] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(expectedFiles) != 3 {
		t.Fatal("expected PDF and two page thumbnails", expectedFiles)
	}
	directories, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range directories {
		if !strings.HasPrefix(entry.Name(), "work_") {
			continue
		}
		if !entry.IsDir() || !expectedScopes[entry.Name()] {
			t.Fatal("unpublished OCR scope retained", entry.Name())
		}
		children, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range children {
			relative := filepath.Join(entry.Name(), child.Name())
			if !child.Type().IsRegular() || !expectedFiles[relative] {
				t.Fatal("unpublished OCR file retained", relative)
			}
			delete(expectedFiles, relative)
		}
	}
	if len(expectedFiles) != 0 {
		t.Fatal("published artifact missing from scopes", expectedFiles)
	}
	for _, name := range []string{"original.png", "second.png"} {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(retained, source) {
			t.Fatal("captured original changed")
		}
	}
	if outputRoot := os.Getenv("SCANNER_TEST_OCR_COMPOSE_OUTPUT"); outputRoot != "" {
		pages := []pdfassemble.Page{}
		for i := 0; i < 2; i++ {
			pages = append(pages, pdfassemble.Page{ID: []string{"page_runtime_00001", "page_runtime_00002"}[i], Size: "a4", Color: "color", Width: geometry.Width, Height: geometry.Height, Rotation: i * 90, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(source)), nil }})
		}
		base, err := pdfassemble.Build(context.Background(), pages, "lossless", pdfassemble.Options{MaxInputBytes: 32 << 20, MaxOutputBytes: 16 << 20, MaxPagePixels: 40000000, MaxPages: 2, JPEGQuality: 92})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(finalPath)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(outputRoot, "worker-base.pdf"), base, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(outputRoot, "worker-composed.pdf"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
