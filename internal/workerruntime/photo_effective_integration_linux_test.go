//go:build linux

package workerruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/importupload"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
	"scanner-kiosk/pkg/ocrplugin"
)

func photoJPEG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 80, 48))
	colors := []color.NRGBA{{240, 20, 20, 255}, {20, 230, 20, 255}, {20, 20, 240, 255}, {240, 220, 20, 255}}
	for y := 0; y < 48; y++ {
		for x := 0; x < 80; x++ {
			im.SetNRGBA(x, y, colors[(y/24)*2+x/40])
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, im, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	// Little-endian EXIF orientation 6, plus no private fixture data.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	out := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	return append(out, b.Bytes()[2:]...)
}
func photoLimits() documents.ImportLimits {
	return documents.ImportLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 16 << 20, GlobalMaxBytes: 32 << 20, JobMaxBytes: 4 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: time.Hour}
}
func photoRequest(t *testing.T, raw []byte) documents.ImportRequest {
	t.Helper()
	request := documents.ImportRequest{IdempotencyKey: "request_photo_trace001", InputBytes: int64(len(raw)), SHA256: sha256.Sum256(raw)}
	if err := json.Unmarshal([]byte(`{"media_type":"image/jpeg"}`), &request); err != nil {
		t.Fatal(err)
	}
	return request
}
func TestPhotoIntakeTracer(t *testing.T) {
	c, store, db, root := fixture(t)
	server, cookie, principal := photoHTTP(t, c, store, root)
	owner := principal
	raw := photoJPEG(t)
	upload, err := importupload.New(root, store, photoLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer upload.Close()
	request := photoRequest(t, raw)
	code, data := photoCall(t, server, cookie, "POST", "/api/documents/import", raw, map[string]string{"Content-Type": "image/jpeg", "Idempotency-Key": request.IdempotencyKey, "X-Content-SHA256": fmt.Sprintf("%x", request.SHA256)})
	if code != 202 {
		t.Fatalf("authenticated JPEG import %d %s", code, data)
	}
	var admitted documents.JobProjection
	if err = json.Unmarshal(data, &admitted); err != nil {
		t.Fatal(err)
	}
	a := documents.Admission{JobID: admitted.ID, DocumentID: admitted.DocumentID}
	var media, path string
	if err = db.QueryRow("SELECT a.media_type,a.relative_path FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id WHERE i.job_id=?", a.JobID).Scan(&media, &path); err != nil {
		t.Fatal(err)
	}
	if media != "image/jpeg" {
		t.Fatalf("photo upload must retain canonical JPEG media, got %s", media)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	p, err := store.ReadDocument(context.Background(), owner, a.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Purpose != "document" || len(p.Pages) != 1 || p.Pages[0].SourceType != "photo" || p.Pages[0].Capture != nil || p.Pages[0].PDF != nil {
		t.Fatalf("not a document photo: %+v", p)
	}
	saved, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("immutable upload changed", err)
	}
	replay, err := upload.Receive(context.Background(), owner, request, bytes.NewReader(nil))
	if err != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("duplicate admission", replay, err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM photo_sources WHERE document_id=?", a.DocumentID).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing normalized provenance", count, err)
	}
}

// photoHTTP exercises real sessions, HTTP deadlines, upload producer and Store.
func photoHTTP(t *testing.T, c config.Config, store *documents.Store, root string) (*httptest.Server, *http.Cookie, string) {
	t.Helper()
	state, err := storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	sessions, err := websession.New(state, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "guest", Lifetime: time.Hour, IdleTimeout: time.Minute, ReceiptLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	files, err := artifactread.OpenRoot(root, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	upload, err := importupload.New(root, store, photoLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { upload.Close() })
	mux := http.NewServeMux()
	mux.Handle("/auth/", sessions)
	mux.Handle("/api/session", sessions)
	mux.Handle("/api/", documenthttp.New(store, sessions, time.Hour, files, time.Minute, nil, nil, upload))
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	var cookie *http.Cookie
	for _, step := range []struct{ method, path string }{{"GET", "/api/session"}, {"POST", "/auth/guest"}} {
		r, _ := http.NewRequest(step.method, server.URL+step.path, nil)
		r.Host = "scanner.example.test"
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Idempotency-Key", "request_photo_guest001")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 || len(resp.Cookies()) == 0 {
			t.Fatal("session bootstrap", resp.StatusCode)
		}
		cookie = resp.Cookies()[0]
	}
	var principal string
	if err = state.QueryRow("SELECT id FROM principals WHERE id!=? AND kind='guest'", owner).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	return server, cookie, principal
}
func photoCall(t *testing.T, server *httptest.Server, cookie *http.Cookie, method, path string, raw []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
	r.Host = "scanner.example.test"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("Origin", "https://scanner.example.test")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}
func assertPhotoCorners(t *testing.T, raw []byte) {
	t.Helper()
	im, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// Orientation 6 rotates TL red/TR green/BL blue/BR yellow clockwise.
	expected := [][3]int{{20, 20, 240}, {240, 20, 20}, {240, 220, 20}, {20, 230, 20}}
	b := im.Bounds()
	for i, p := range []image.Point{{b.Dx() / 4, b.Dy() / 4}, {b.Dx() * 3 / 4, b.Dy() / 4}, {b.Dx() / 4, b.Dy() * 3 / 4}, {b.Dx() * 3 / 4, b.Dy() * 3 / 4}} {
		r, g, bl, _ := im.At(p.X, p.Y).RGBA()
		for c, v := range []uint32{r >> 8, g >> 8, bl >> 8} {
			d := int(v) - expected[i][c]
			if d < -18 || d > 18 {
				t.Fatalf("orientation corner %d channel %d: %d expected %d (%v)", i, c, v, expected[i][c], b)
			}
		}
	}
}
func TestPhotoEffectivePixelsPlainSavePreviewThumbnailAndReOCR(t *testing.T) {
	c, store, db, root := fixture(t)
	server, cookie, principal := photoHTTP(t, c, store, root)
	raw := photoJPEG(t)
	digest := sha256.Sum256(raw)
	headers := map[string]string{"Content-Type": "image/jpeg", "Idempotency-Key": "request_photo_http001", "X-Content-SHA256": fmt.Sprintf("%x", digest)}
	code, data := photoCall(t, server, cookie, "POST", "/api/documents/import", raw, headers)
	if code != 202 {
		t.Fatalf("HTTP JPEG intake: %d %s", code, data)
	}
	var job documents.JobProjection
	if err := json.Unmarshal(data, &job); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	p, err := store.ReadDocument(context.Background(), principal, job.DocumentID)
	if err != nil || len(p.Pages) != 1 {
		t.Fatal("photo publication", p, err)
	}
	page := p.Pages[0]
	preview := "/api/documents/" + p.ID + "/pages/" + page.ID + "/preview"
	code, data = photoCall(t, server, cookie, "GET", preview, nil, nil)
	if code != 200 {
		t.Fatalf("effective preview %d %s", code, data)
	}
	assertPhotoCorners(t, data)
	baseline, err := store.ReadPageEffective(context.Background(), principal, p.ID, page.ID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.ReadPageOriginal(context.Background(), principal, p.ID, page.ID)
	if err != nil || original.Artifact.ID == baseline.Artifact.ID || original.Artifact.SHA256 != digest {
		t.Fatal("raw/effective provenance", err)
	}
	if baseline.Artifact.PixelWidth != 48 || baseline.Artifact.PixelHeight != 80 {
		t.Fatal("normalized dimensions", baseline)
	}
	limits := photoLimits().AdmissionLimits
	a, err := store.AdmitRender(context.Background(), principal, documents.RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_photo_save001", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 4 << 20}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	saved, err := store.ReadJob(context.Background(), principal, a.JobID)
	if err != nil || saved.State != "succeeded" {
		t.Fatal("plain Save", saved, err)
	}
	code, data = photoCall(t, server, cookie, "GET", "/api/documents/"+p.ID+"/pages/"+page.ID+"/thumbnail", nil, nil)
	if code != 200 {
		t.Fatalf("effective thumbnail %d %s", code, data)
	}
	assertPhotoCorners(t, data)
	var thumbSource string
	if err = db.QueryRow("SELECT original_artifact_id FROM page_thumbnails WHERE document_id=?", p.ID).Scan(&thumbSource); err != nil || thumbSource != baseline.Artifact.ID {
		t.Fatal("thumbnail bound to raw", thumbSource, err)
	}
	output, err := store.GetExport(context.Background(), principal, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	configurePDFRaster(t)
	pixels, err := exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-singlefile", "-r", "36", "-png", filepath.Join(root, output.RelativePath)).Output()
	if err != nil {
		t.Fatal("independent official Poppler raster", err)
	}
	assertPhotoCorners(t, pixels)
	// Real worker and provider protocol; the synthetic transcript is not OCR quality evidence.
	ocrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "{\"text\":\"SYNTHETIC PHOTO TEXT\"}"}}}})
	}))
	defer ocrServer.Close()
	profiles, _ := json.Marshal([]config.LLMProfile{{ID: "fixture", Name: "Fixture", ProcessingDestination: "Synthetic loopback", Protocol: "chat-completions", Endpoint: ocrServer.URL, AllowInsecureHTTP: true, Model: "fixture", ResponseModel: "fixture", Version: "1", Languages: []string{"en"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text}, MaxCompletionTokens: 1000}})
	for k, v := range map[string]string{"SCANNER_OCR_PLUGINS": "llm", "SCANNER_OCR_DEFAULT_PLUGIN": "llm", "SCANNER_OCR_DEFAULT_LANGUAGES": "en", "SCANNER_OCR_LLM_PROFILES_JSON": string(profiles)} {
		t.Setenv(k, v)
	}
	metadata := &processingRecords{renderExecutionRecords: store, config: c}
	catalog, err := metadata.catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.close()
	selection := catalog.Preferred
	if selection == nil {
		t.Fatal("missing OCR selection")
	}
	var version string
	for _, provider := range catalog.Providers {
		for _, model := range provider.Models {
			if model.ID == selection.ModelID {
				version = model.Version
			}
		}
	}
	p, err = store.ReadDocument(context.Background(), principal, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	reocr, err := store.AdmitRender(context.Background(), principal, documents.RenderAdmission{DocumentID: p.ID, Revision: p.Revision, IdempotencyKey: "request_photo_reocr001", ReOCR: true, ResolvedOutput: ocrplugin.Text, Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless", OCR: documents.RenderOCR{Enabled: true, ProviderID: selection.ProviderID, ModelID: selection.ModelID, ModelVersion: version, Languages: selection.Languages, QualityPreset: selection.QualityPreset}}, ReserveBytes: 4 << 20}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	var bundleJSON string
	if err = db.QueryRow("SELECT bundle_json FROM ocr_results WHERE job_id=?", reocr.JobID).Scan(&bundleJSON); err != nil {
		j, _ := store.ReadJob(context.Background(), principal, reocr.JobID)
		t.Fatalf("reOCR publication %+v code=%v err=%v", j, *j.ErrorCode, err)
	}
	var bundle documents.OCRResultBundle
	if err = json.Unmarshal([]byte(bundleJSON), &bundle); err != nil {
		t.Fatal(err)
	}
	resultPage := bundle.Pages[0]
	if resultPage.SourceType != "photo" || resultPage.OriginalArtifactID != original.Artifact.ID || resultPage.EffectiveArtifactID != baseline.Artifact.ID || resultPage.OriginalSHA256 != digest || resultPage.EffectiveSHA256 != baseline.Artifact.SHA256 || resultPage.Preview == nil {
		t.Fatal("reOCR frozen source", resultPage)
	}
	data, err = os.ReadFile(filepath.Join(root, resultPage.Preview.RelativePath))
	if err != nil {
		t.Fatal(err)
	}
	assertPhotoCorners(t, data)
	adopted, err := store.AdoptOCRResult(context.Background(), principal, documents.OCRResultRequest{DocumentID: p.ID, ResultID: bundle.ID, Revision: p.Revision, IdempotencyKey: "request_photo_adopt001"}, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.GetExport(context.Background(), principal, p.ID)
	if err != nil || current.ID != bundle.Output.ID || adopted.Revision != p.Revision+1 {
		t.Fatal("photo adoption", adopted, err)
	}
	pixels, err = exec.Command(os.Getenv("SCANNER_PDF_RASTER_PATH"), "-singlefile", "-r", "36", "-png", filepath.Join(root, current.RelativePath)).Output()
	if err != nil {
		t.Fatal(err)
	}
	assertPhotoCorners(t, pixels)
}
