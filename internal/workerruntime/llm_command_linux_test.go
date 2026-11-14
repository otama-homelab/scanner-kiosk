//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	ocrruntime "scanner-kiosk/internal/ocr/runtime"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestLLMWorkerCommandPublishesPositionedPDF(t *testing.T) {
	// Build before admission so a cold toolchain cannot consume the job deadline.
	binary := filepath.Join(t.TempDir(), "scanner")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/scanner")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("unexpected method")
		}
		number := calls.Add(1)
		content, _ := json.Marshal(map[string]any{"supplement": "Synthetic layout interpretation.", "spans": []any{map[string]any{"text": fmt.Sprintf("Page%d", number), "box": map[string]any{"x": 50, "y": 100, "width": 700, "height": 200}}}})
		json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(content)}}}})
	}))
	defer server.Close()
	c, store, db, root := fixture(t)
	original, err := os.ReadFile(filepath.Join(root, "original.png"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	if err = os.WriteFile(filepath.Join(root, "second.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_runtime_002',?,?,'original','ready','second.png','image/png',?,?,1,?)`, document, owner, len(original), digest[:], time.Now().UnixMilli())
	execute(t, db, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,rotation) VALUES('page_runtime_00002',?,?,1,'original_runtime_002','a4','gray',300,90)`, document, owner)
	fontPath := filepath.Join(t.TempDir(), "approved-fixture.ttf")
	if err = os.WriteFile(fontPath, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	profile := config.LLMProfile{ID: "local", Name: "Local", ProcessingDestination: "Fixture OCR service", Protocol: "chat-completions", Endpoint: server.URL, AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PositionedText}, MaxCompletionTokens: 1000}
	profiles, _ := json.Marshal([]config.LLMProfile{profile})
	values := map[string]string{"SCANNER_OCR_PLUGINS": "llm", "SCANNER_OCR_DEFAULT_LANGUAGES": "en", "SCANNER_OCR_LLM_PROFILES_JSON": string(profiles), "SCANNER_OCR_LLM_TEXT_LAYER_FONT": fontPath}
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	metadata, err := ocrruntime.NewWorker(context.Background(), c, lookup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defaults := metadata.Defaults()
	metadata.Close()
	if calls.Load() != 0 {
		t.Fatal("metadata initialization contacted model")
	}
	before, err := store.Get(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: 1, IdempotencyKey: "request_llm_command_001", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: defaults.PluginID, ModelID: defaults.ModelID, ModelVersion: defaults.ModelVersion, Languages: defaults.Languages, QualityPreset: defaults.QualityPreset}}, ReserveBytes: 8 << 20}, documents.AdmissionLimits{UserMaxBytes: 32 << 20, GlobalMaxBytes: 64 << 20, JobMaxBytes: 16 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "worker", "--once")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "SCANNER_STATE_DIR=" + c.StateDir, "SCANNER_DOCUMENT_DIR=" + root}
	for k, v := range values {
		command.Env = append(command.Env, k+"="+v)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("worker: %v %s", err, output)
	}
	exported, err := store.GetExport(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one request per page, got %d", calls.Load())
	}
	var state string
	var reservations int
	if err = db.QueryRow("SELECT state FROM jobs WHERE id=?", admitted.JobID).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("job %s: %v", state, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM reservations WHERE job_id=?", admitted.JobID).Scan(&reservations); err != nil || reservations != 0 {
		t.Fatalf("reservations %d: %v", reservations, err)
	}
	after, err := store.Get(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	if before.ExpiresAtMS != after.ExpiresAtMS {
		t.Fatal("OCR extended original retention")
	}
	for _, name := range []string{"original.png", "second.png"} {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(retained, original) {
			t.Fatal("original changed")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	// Final export and thumbnail scopes may both remain; every retained scope
	// must be justified by a ready artifact, rather than a fixed directory count.
	rows, err := db.Query("SELECT relative_path FROM artifacts WHERE document_id=? AND state='ready'", document)
	if err != nil {
		t.Fatal(err)
	}
	retainedScopes := map[string]bool{}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			t.Fatal(err)
		}
		name := strings.Split(filepath.ToSlash(path), "/")[0]
		if strings.HasPrefix(name, "work_") {
			retainedScopes[name] = true
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "work_") && !retainedScopes[entry.Name()] {
			t.Fatalf("unpublished intermediate scope retained: %s", entry.Name())
		}
	}
	t.Run("independent-page-text", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("requires independent Poppler extractor")
		}
		for i := 1; i <= 2; i++ {
			number := fmt.Sprint(i)
			text, err := exec.Command(tool, "-f", number, "-l", number, "-raw", filepath.Join(root, exported.RelativePath), "-").Output()
			normalized := strings.Join(strings.Fields(string(text)), " ")
			if err != nil || !strings.Contains(normalized, "Page"+number) || strings.Contains(normalized, "Page"+fmt.Sprint(3-i)) || !strings.Contains(normalized, "LLM supplement (interpretation, not OCR): Synthetic layout interpretation.") {
				t.Fatalf("page %d: %q %v", i, text, err)
			}
		}
	})
}
