//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"scanner-kiosk/internal/apiruntime"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/pkg/ocrplugin"
)

// One real store/coordinator/child graph; scanner acquisition and external
// services are synthetic. The browser never substitutes a successful API reply.
func TestServerUXIntegratedSavePrimary(t *testing.T) {
	for _, key := range []string{"SCANNER_TEST_PLAYWRIGHT_MODULE", "SCANNER_TEST_CHROMIUM", "SCANNER_TEST_PDF_RUNTIME"} {
		if os.Getenv(key) == "" {
			t.Fatalf("required runtime %s missing", key)
		}
	}
	configurePDFRaster(t)
	c, _, db, root := fixture(t)
	state, err := storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	project, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("npm", "--prefix", "web", "run", "build")
	build.Dir = project
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build %v: %s", e, out)
	}
	var mode, posts, ocrCalls atomic.Int32
	var marker atomic.Value
	marker.Store("")
	var uploadedHash atomic.Value
	uploadedHash.Store("")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Version", "3.4.2")
		w.Header().Set("X-Api-Version", "10")
		if r.URL.Path == "/chat/completions" {
			ocrCalls.Add(1)
			if mode.Load() == 1 {
				http.Error(w, "synthetic OCR failure", 503)
				return
			}
			if mode.Load() == 2 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": `{"spans":[{"text":"SYNTHETIC RECEIPT","box":{"x":2,"y":2,"width":15,"height":10}}]}`}}}})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/documents/post_document/" {
			posts.Add(1)
			if r.Header.Get("Authorization") != "Token synthetic-integrated-only" {
				t.Error("wrong synthetic credential")
			}
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, e := r.FormFile("document")
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			data, e := io.ReadAll(file)
			file.Close()
			if e != nil {
				t.Error(e)
				return
			}
			marker.Store(header.Filename)
			uploadedHash.Store(fmt.Sprintf("%x", sha256.Sum256(data)))
			if mode.Load() == 4 {
				conn, _, e := w.(http.Hijacker).Hijack()
				if e == nil {
					conn.Close()
				}
				return
			}
			json.NewEncoder(w).Encode(deliveryTask)
			return
		}
		switch r.URL.Path {
		case "/api/":
			fmt.Fprint(w, `{}`)
		case "/api/tasks/":
			if r.URL.RawQuery != "task_id="+deliveryTask {
				t.Error("nonexact remote identity")
			}
			status := "SUCCESS"
			if mode.Load() == 3 {
				status = "FAILURE"
			}
			fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":%q,"related_document":42}]`, deliveryTask, marker.Load(), status)
		case "/api/documents/42/metadata/":
			fmt.Fprintf(w, `{"original_checksum":%q}`, uploadedHash.Load())
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	var api *apiruntime.App
	assets := http.FileServer(http.Dir(filepath.Join(project, "web/dist")))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__fixture/bootstrap":
			var principal string
			if e := state.QueryRow("SELECT id FROM principals WHERE id!=? AND kind='guest'", owner).Scan(&principal); e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			if _, e := db.Exec("INSERT OR IGNORE INTO owners(id) VALUES(?)", principal); e != nil {
				t.Error(e)
				return
			}
			now := time.Now().UnixMilli()
			data, e := os.ReadFile(filepath.Join(root, "original.png"))
			if e != nil {
				t.Error(e)
				return
			}
			digest := sha256.Sum256(data)
			if e = os.WriteFile(filepath.Join(root, "integrated-scan.png"), data, 0600); e != nil {
				t.Error(e)
				return
			}
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{"INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_integrated_scan',?,'document','review',?,?)", []any{principal, now, now + 3600000}},
				{"INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_integrated_scan','document_integrated_scan',?,'original','ready','integrated-scan.png','image/png',?,?,1,?)", []any{principal, len(data), digest[:], now}},
				{"INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_integrated_scan','document_integrated_scan',?,0,'original_integrated_scan','a4','gray',300,80,100)", []any{principal}},
			} {
				if _, e = db.Exec(q.sql, q.args...); e != nil {
					t.Error(e)
					w.WriteHeader(500)
					return
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"scan": "document_integrated_scan", "photo": photoJPEG(t), "pdf": importFixturePDF()})
		case "/__fixture/mode":
			var value struct{ Mode int }
			if json.NewDecoder(r.Body).Decode(&value) != nil {
				w.WriteHeader(400)
				return
			}
			mode.Store(int32(value.Mode))
			fmt.Fprint(w, `{}`)
		case "/__fixture/observe":
			var docs, deliveries, reservations int
			for q, p := range map[string]*int{"SELECT count(*) FROM documents WHERE id!=?": &docs, "SELECT count(*) FROM deliveries WHERE document_id!=?": &deliveries, "SELECT count(*) FROM reservations WHERE job_id!=?": &reservations} {
				if e := db.QueryRow(q, document).Scan(p); e != nil {
					t.Error(e)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"documents": docs, "deliveries": deliveries, "uploads": posts.Load(), "ocr_calls": ocrCalls.Load(), "reservations": reservations})
		default:
			if len(r.URL.Path) >= 5 && (r.URL.Path[:5] == "/api/" || r.URL.Path[:5] == "/auth") {
				api.Handler().ServeHTTP(w, r)
			} else {
				assets.ServeHTTP(w, r)
			}
		}
	}))
	defer server.Close()
	profiles, _ := json.Marshal([]config.LLMProfile{{ID: "fixture", Name: "Fixture", ProcessingDestination: "Synthetic loopback", Protocol: "chat-completions", Endpoint: remote.URL + "/chat/completions", AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.PositionedText}, MaxCompletionTokens: 1000}})
	fontPath := filepath.Join(t.TempDir(), "approved-fixture.ttf")
	if e := os.WriteFile(fontPath, goregular.TTF, 0600); e != nil {
		t.Fatal(e)
	}
	values := map[string]string{"SCANNER_STATE_DIR": c.StateDir, "SCANNER_DOCUMENT_DIR": root, "SCANNER_PUBLIC_URL": server.URL, "SCANNER_AUTHENTICATE_TYPE": "none", "SCANNER_SCANNERS_JSON": "[]", "SCANNER_OCR_PLUGINS": "llm", "SCANNER_OCR_DEFAULT_PLUGIN": "llm", "SCANNER_OCR_DEFAULT_LANGUAGES": "en", "SCANNER_OCR_LLM_PROFILES_JSON": string(profiles), "SCANNER_PAPERLESS_ENABLED": "true", "SCANNER_PAPERLESS_DESTINATION": "Synthetic archive", "SCANNER_PAPERLESS_BASE_URL": remote.URL, "SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP": "true", "SCANNER_PAPERLESS_TOKEN_ENV": "SCANNER_PAPERLESS_TOKEN_INTEGRATED", "SCANNER_PAPERLESS_TOKEN_INTEGRATED": "synthetic-integrated-only"}
	values["SCANNER_OCR_LLM_TEXT_LAYER_FONT"] = fontPath
	// The composed fixture intentionally performs a burst of identity/source reads.
	values["SCANNER_API_REQUESTS_PER_MINUTE"] = "10000"
	values["SCANNER_API_BURST"] = "1024"
	for k, v := range values {
		t.Setenv(k, v)
	}
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	apiConfig, e := config.Load(lookup)
	if e != nil {
		t.Fatal(e)
	}
	api, e = apiruntime.New(context.Background(), apiConfig)
	if e != nil {
		t.Fatal(e)
	}
	defer api.Close()
	workerConfig, e := config.LoadWorker(lookup)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- Run(ctx, workerConfig, false) }()
	defer func() {
		cancel()
		if e := <-finished; e != nil && !errors.Is(e, context.Canceled) {
			t.Error(e)
		}
	}()
	evidence := t.TempDir()
	browser := exec.Command("node", "scripts/check-web-server-ux.cjs")
	browser.Dir = project
	browser.Env = append(os.Environ(), "PLAYWRIGHT_MODULE="+os.Getenv("SCANNER_TEST_PLAYWRIGHT_MODULE"), "CHROMIUM_EXECUTABLE="+os.Getenv("SCANNER_TEST_CHROMIUM"), "SCANNER_TEST_ORIGIN="+server.URL, "SCANNER_SERVER_UX_OUTPUT="+evidence)
	out, e := browser.CombinedOutput()
	if e != nil {
		t.Fatalf("composed browser invariant: %v\n%s", e, out)
	}
	t.Log(string(out))
	var receipt struct {
		Passed    bool
		Downloads []struct {
			Name, DocumentID, SHA256 string `json:",omitempty"`
		}
		Checks []string
	}
	data, e := os.ReadFile(filepath.Join(evidence, "browser.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &receipt); e != nil || !receipt.Passed || len(receipt.Downloads) < 5 {
		t.Fatal("incomplete real browser evidence", e)
	}
	for _, saved := range receipt.Downloads {
		data, e := os.ReadFile(filepath.Join(evidence, saved.Name+".pdf"))
		if e != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != saved.SHA256 {
			t.Fatal("browser download hash", saved, e)
		}
		digest := sha256.Sum256(data)
		var recorded int
		if e = db.QueryRow("SELECT count(*) FROM artifacts WHERE document_id=? AND kind='output' AND sha256=? AND byte_size=?", saved.DocumentID, digest[:], len(data)).Scan(&recorded); e != nil || recorded < 1 {
			t.Fatal("download not bound to real published artifact", saved.Name, e)
		}
		pixels, e := exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-singlefile", "-r", "36", "-png", filepath.Join(evidence, saved.Name+".pdf")).Output()
		if e != nil {
			t.Fatal("independent Poppler", saved, e)
		}
		if saved.Name == "photo" {
			assertPhotoCorners(t, pixels)
		}
		if saved.Name == "corrected" || saved.Name == "adopted" {
			assertPhotoCornerColors(t, pixels, [][3]int{{20, 20, 240}, {20, 20, 240}, {240, 220, 20}, {240, 220, 20}})
		}
		if saved.Name == "scan" {
			original, e := os.ReadFile(filepath.Join(root, "original.png"))
			if e != nil {
				t.Fatal(e)
			}
			baseline, e := pdfassemble.Build(context.Background(), []pdfassemble.Page{{ID: "scan-baseline", Size: "a4", Color: "gray", Width: 80, Height: 100, Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(original)), nil }}}, "jpeg", pdfassemble.Options{MaxInputBytes: 8 << 20, MaxOutputBytes: 8 << 20, MaxPagePixels: 40000000, MaxPages: 10, JPEGQuality: 92})
			if e != nil {
				t.Fatal(e)
			}
			file := filepath.Join(evidence, "baseline.pdf")
			if e = os.WriteFile(file, baseline, 0600); e != nil {
				t.Fatal(e)
			}
			want, e := exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-singlefile", "-r", "36", "-png", file).Output()
			if e != nil || !bytes.Equal(want, pixels) {
				t.Fatal("legacy scan pixels changed", e)
			}
		}
		if saved.Name == "pdf" {
			file := filepath.Join(evidence, "import-original.pdf")
			if e = os.WriteFile(file, importFixturePDF(), 0600); e != nil {
				t.Fatal(e)
			}
			want, e := exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-singlefile", "-r", "36", "-png", file).Output()
			if e != nil || !bytes.Equal(want, pixels) {
				t.Fatal("imported PDF pixels changed", e)
			}
			want, e = exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-f", "2", "-l", "2", "-singlefile", "-r", "36", "-png", file).Output()
			second, e2 := exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-f", "2", "-l", "2", "-singlefile", "-r", "36", "-png", filepath.Join(evidence, saved.Name+".pdf")).Output()
			if e != nil || e2 != nil || !bytes.Equal(want, second) {
				t.Fatal("second imported PDF page pixels changed", e, e2)
			}
		}
	}
	rows, e := db.Query("SELECT configuration_json FROM jobs WHERE kind='render'")
	if e != nil {
		t.Fatal(e)
	}
	var frozen []documents.RenderSnapshot
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var snapshot documents.RenderSnapshot
		if e = json.Unmarshal([]byte(raw), &snapshot); e != nil {
			t.Fatal(e)
		}
		frozen = append(frozen, snapshot)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	photoSources := 0
	for _, snapshot := range frozen {
		for _, source := range snapshot.Sources {
			for _, artifact := range []documents.ArtifactRecord{source.Original, source.Effective} {
				var hash []byte
				if e = db.QueryRow("SELECT sha256 FROM artifacts WHERE id=?", artifact.ID).Scan(&hash); e != nil || !bytes.Equal(hash, artifact.SHA256[:]) {
					t.Fatal("frozen RenderSource identity mismatch", artifact.ID, e)
				}
			}
			if source.Original.MediaType == "image/jpeg" {
				photoSources++
				if source.Original.SHA256 != sha256.Sum256(photoJPEG(t)) || source.Original.ID == source.Effective.ID {
					t.Fatal("photo raw/effective lineage changed")
				}
			}
		}
	}
	if photoSources < 4 {
		t.Fatal("missing normalized/corrected/reOCR frozen source coverage", photoSources)
	}
	if e = os.WriteFile(filepath.Join(project, ".planning/phases/49-server-app-completion/49-03-BROWSER.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("independent Poppler and actual browser download hashes verified: %d artifacts", len(receipt.Downloads))
}
