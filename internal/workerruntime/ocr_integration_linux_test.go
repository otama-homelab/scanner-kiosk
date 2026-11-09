//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/image/font/gofont/goregular"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/correction"
	"scanner-kiosk/internal/documents"
	ocrruntime "scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdfimport"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestRuntimeReOCRCompletion(t *testing.T) {
	testRuntimeReOCRDocument(t, "image")
}
func TestReOCRMixedDocument(t *testing.T)         { testRuntimeReOCRDocument(t, "mixed") }
func TestReOCRPDFOnlyDocument(t *testing.T)       { testRuntimeReOCRDocument(t, "pdf") }
func TestReOCRPDFExistingTextPolicy(t *testing.T) { testRuntimeReOCRDocument(t, "existing-text") }

func testRuntimeReOCRDocument(t *testing.T, mode string) {
	for _, kind := range []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PositionedText} {
		t.Run(string(kind), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				payload := map[string]any{"text": "RUNTIME OCR TEXT"}
				if kind == ocrplugin.PositionedText {
					payload = map[string]any{"spans": []any{map[string]any{"text": "RUNTIME OCR TEXT", "box": map[string]any{"x": 2, "y": 3, "width": 60, "height": 10}}}}
				}
				content, _ := json.Marshal(payload)
				json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(content)}}}})
			}))
			defer server.Close()
			c, store, db, root := fixture(t)
			wantPages := 1
			if mode != "image" {
				configurePDFRaster(t)
				parts, e := pdfimport.Split(context.Background(), importFixturePDF(), pdfimport.Options{MaxInputBytes: 1 << 20, MaxOutputBytes: 1 << 20, MaxPages: 10})
				if e != nil {
					t.Fatal(e)
				}
				if mode != "mixed" {
					execute(t, db, "DELETE FROM pages WHERE document_id=?", document)
					wantPages = 0
				}
				for i, part := range parts {
					name := fmt.Sprintf("source-%d.pdf", i)
					if e = os.WriteFile(filepath.Join(root, name), part.PDF, 0600); e != nil {
						t.Fatal(e)
					}
					digest := sha256.Sum256(part.PDF)
					id := fmt.Sprintf("original_pdf_%05d", i)
					page := fmt.Sprintf("page_pdf_%09d", i)
					execute(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'application/pdf',?,?,1,?)`, id, document, owner, name, len(part.PDF), digest[:], time.Now().UnixMilli())
					execute(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,source_type,pdf_width_points,pdf_height_points,rotation) VALUES(?,?,?,?,?,'pdf',?,?,?)`, page, document, owner, wantPages, id, part.WidthPoints, part.HeightPoints, i*90)
					wantPages++
				}
				if mode == "mixed" {
					corrected := image.NewGray(image.Rect(0, 0, 80, 100))
					corrected.SetGray(0, 0, color.Gray{Y: 255})
					var encoded bytes.Buffer
					if e := png.Encode(&encoded, corrected); e != nil {
						t.Fatal(e)
					}
					data := encoded.Bytes()
					if e := os.WriteFile(filepath.Join(root, "corrected.png"), data, 0600); e != nil {
						t.Fatal(e)
					}
					digest := sha256.Sum256(data)
					var original []byte
					if e := db.QueryRow("SELECT sha256 FROM artifacts WHERE id='original_runtime_001'").Scan(&original); e != nil {
						t.Fatal(e)
					}
					transform, _ := json.Marshal(correction.Transform{CropQuad: [4]correction.Point{{X: 0, Y: 0}, {X: 80, Y: 0}, {X: 80, Y: 100}, {X: 0, Y: 100}}, OutputWidth: 80, OutputHeight: 100, ConfidenceKind: "unavailable"})
					execute(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('corrected_runtime_001',?,?,'intermediate','ready','corrected.png','image/png',?,?,1,?)`, document, owner, len(data), digest[:], time.Now().UnixMilli())
					execute(t, db, `INSERT INTO page_corrections(suggestion_id,document_id,owner_id,page_id,original_artifact_id,correction_artifact_id,source_sha256,correction_sha256,transform_json,confidence_kind,created_at_ms) VALUES('suggestion_runtime_001',?,?,'page_runtime_00001','original_runtime_001','corrected_runtime_001',?,?,?,'unavailable',?)`, document, owner, original, digest[:], string(transform), time.Now().UnixMilli())
					execute(t, db, `UPDATE page_corrections SET state='applied',applied_at_ms=? WHERE suggestion_id='suggestion_runtime_001'`, time.Now().UnixMilli())
				}
			}
			admit(t, store)
			if e := Run(context.Background(), c, true); e != nil {
				t.Fatal(e)
			}
			old, e := store.GetExport(context.Background(), owner, document)
			if e != nil {
				t.Fatal(e)
			}
			profile := config.LLMProfile{ID: "fixture", Name: "Fixture", ProcessingDestination: "Synthetic loopback fixture", Protocol: "chat-completions", Endpoint: server.URL, AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{kind}, MaxCompletionTokens: 1000}
			profiles, _ := json.Marshal([]config.LLMProfile{profile})
			t.Setenv("SCANNER_OCR_PLUGINS", "llm")
			t.Setenv("SCANNER_OCR_DEFAULT_PLUGIN", "llm")
			t.Setenv("SCANNER_OCR_DEFAULT_LANGUAGES", "en")
			t.Setenv("SCANNER_OCR_LLM_PROFILES_JSON", string(profiles))
			fontPath := filepath.Join(t.TempDir(), "font.ttf")
			if e = os.WriteFile(fontPath, goregular.TTF, 0600); e != nil {
				t.Fatal(e)
			}
			t.Setenv("SCANNER_OCR_LLM_TEXT_LAYER_FONT", fontPath)
			metadata, e := ocrruntime.NewWorker(context.Background(), c, os.LookupEnv, t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defaults := metadata.Defaults()
			metadata.Close()
			a, e := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 2, IdempotencyKey: "request_reocr_runtime01", ReOCR: true, ResolvedOutput: kind, Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: defaults.PluginID, ModelID: defaults.ModelID, ModelVersion: defaults.ModelVersion, Languages: defaults.Languages, QualityPreset: defaults.QualityPreset}}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 4 << 20, GlobalMaxBytes: 8 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
			if e != nil {
				t.Fatal(e)
			}
			if e = Run(context.Background(), c, true); e != nil {
				t.Fatal("actual runtime route failed", e)
			}
			var raw string
			if e = db.QueryRow("SELECT bundle_json FROM ocr_results WHERE job_id=?", a.JobID).Scan(&raw); e != nil {
				t.Fatal("actual runtime failed to complete candidate", e)
			}
			var bundle documents.OCRResultBundle
			if e = json.Unmarshal([]byte(raw), &bundle); e != nil {
				t.Fatal(e)
			}
			if len(bundle.Pages) != wantPages || (bundle.Pages[0].Positions != nil) != (kind == ocrplugin.PositionedText) || calls.Load() != int32(wantPages) {
				t.Fatal("runtime candidate geometry or invocation mismatch")
			}
			text, e := os.ReadFile(filepath.Join(root, bundle.Pages[0].Text.RelativePath))
			if e != nil || string(text) != "RUNTIME OCR TEXT" {
				t.Fatal("durable runtime transcript missing", e)
			}
			current, e := store.GetExport(context.Background(), owner, document)
			if e != nil || current.ID != old.ID {
				t.Fatal("runtime adopted candidate", e)
			}
			if mode != "image" {
				assertRuntimePDFSources(t, root, bundle, old, kind)
			}
			if mode == "mixed" && (bundle.Pages[0].CorrectionID != "suggestion_runtime_001" || bundle.Pages[0].EffectiveArtifactID != "corrected_runtime_001" || bundle.Pages[0].Preview.SHA256 != bundle.Pages[0].EffectiveSHA256) {
				t.Fatal("current correction source was not recognized/previewed")
			}
		})
	}
}

func configurePDFRaster(t *testing.T) {
	t.Helper()
	runtimeRoot := os.Getenv("SCANNER_TEST_PDF_RUNTIME")
	binary, e := os.ReadFile(filepath.Join(runtimeRoot, "usr/bin/pdftoppm"))
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(binary)) != "e969e55d7e2229f1e438284f9e8cfd666313e45a9b7b6265ccbf8b4ade5f2070" {
		t.Fatal("verified official Poppler required: SCANNER_TEST_PDF_RUNTIME")
	}
	dir := t.TempDir()
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(runtimeRoot, filepath.Join(dir, "runtime")); e != nil {
		t.Fatal(e)
	}
	tool := filepath.Join(dir, "pdf-test-tool")
	if e = os.Symlink(self, tool); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SCANNER_PDF_RASTER_PATH", tool)
}

func assertRuntimePDFSources(t *testing.T, root string, b documents.OCRResultBundle, old documents.Export, kind ocrplugin.OutputKind) {
	t.Helper()
	output := filepath.Join(root, b.Output.RelativePath)
	text, e := exec.Command("pdftotext", "-layout", output, "-").CombinedOutput()
	if e != nil {
		t.Fatalf("independent extraction: %v %s", e, text)
	}
	for _, word := range []string{"IMPORT_ALPHA", "IMPORT_BETA"} {
		if strings.Count(string(text), word) != 1 {
			t.Fatalf("native text duplicated or lost: %s", text)
		}
	}
	for i, p := range b.Pages {
		if p.SourceType != "pdf" {
			continue
		}
		if p.PDFTextOrigin != "retained-source" || p.PDFRaster == nil || p.PDFRaster.DPI != 300 || p.Preview == nil || (p.Positions != nil) != (kind == ocrplugin.PositionedText) {
			t.Fatalf("missing real PDF provenance: %+v", p)
		}
		transcript, e := os.ReadFile(filepath.Join(root, p.Text.RelativePath))
		if e != nil || string(transcript) != "RUNTIME OCR TEXT" {
			t.Fatal("PDF transcript missing", e)
		}
		preview, e := os.ReadFile(filepath.Join(root, p.Preview.RelativePath))
		if e != nil {
			t.Fatal(e)
		}
		config, _, e := image.DecodeConfig(bytes.NewReader(preview))
		if e != nil || config.Width != p.PixelWidth || config.Height != p.PixelHeight {
			t.Fatal("preview does not match recognition frame", e)
		}
		var renders [][]byte
		for _, path := range []string{filepath.Join(root, old.RelativePath), output} {
			pixels, e := exec.Command("pdftoppm", "-f", fmt.Sprint(i+1), "-l", fmt.Sprint(i+1), "-singlefile", "-r", "72", "-png", path).Output()
			if e != nil {
				t.Fatal("independent raster", e)
			}
			renders = append(renders, pixels)
		}
		if !bytes.Equal(renders[0], renders[1]) {
			t.Fatal("original vector/text appearance or rotation changed")
		}
		original, e := os.ReadFile(filepath.Join(root, fmt.Sprintf("source-%d.pdf", i-(len(b.Pages)-2))))
		if e != nil || sha256.Sum256(original) != p.OriginalSHA256 {
			t.Fatal("original PDF replaced", e)
		}
	}
	images, e := exec.Command("pdfimages", "-list", output).CombinedOutput()
	if e != nil {
		t.Fatal("independent image structure", e)
	}
	// The final imported pages must remain vectors/text, not recognition images.
	if len(b.Pages) == 2 && len(strings.Split(strings.TrimSpace(string(images)), "\n")) != 2 {
		t.Fatalf("PDF-only candidate contains replacement raster: %s", images)
	}
}

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
