package documenthttp_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
	"scanner-kiosk/pkg/ocrplugin"
)

const resultID = "ocr_result_http_0001"
const reocrBody = `{"format":"pdf","ocr":{"enabled":true,"provider_id":"local","model_id":"model","languages":["ja"]}}`

type httpOCRResolver struct{}

func (httpOCRResolver) ResolveOCR(context.Context, documents.RenderOCR, bool) (renderadmission.ResolvedOCR, error) {
	return renderadmission.ResolvedOCR{ModelVersion: "v1", MaxPages: 10, MaxPagePixels: 100000, Output: ocrplugin.Text}, nil
}

type ocrHTTPFixture struct {
	t           *testing.T
	state, docs *sql.DB
	store       *documents.Store
	server      *httptest.Server
	cookie      *http.Cookie
	owner, root string
	offset      atomic.Int64
	reads       atomic.Int64
	beforeRead  func(int64)
}

// Keep the real Store as the authority, but inject a state change exactly on
// the second resolution (after stream opened and hashed the file).
type ocrReadBoundary struct {
	*documents.Store
	fixture *ocrHTTPFixture
}

func (s ocrReadBoundary) ReadOCRResultArtifact(ctx context.Context, owner, document, result, page, kind string) (documents.ArtifactReference, error) {
	n := s.fixture.reads.Add(1)
	if s.fixture.beforeRead != nil {
		s.fixture.beforeRead(n)
	}
	return s.Store.ReadOCRResultArtifact(ctx, owner, document, result, page, kind)
}

func (f *ocrHTTPFixture) exec(db *sql.DB, q string, args ...any) {
	f.t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		f.t.Fatal(q, err)
	}
}
func (f *ocrHTTPFixture) call(method, path, body string, cookie *http.Cookie, change func(http.Header)) (int, http.Header, []byte) {
	f.t.Helper()
	r, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	r.Host = "scanner.example.test"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if method == "POST" {
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"1"`)
		r.Header.Set("Idempotency-Key", "request_ocr_http_001")
	}
	if change != nil {
		change(r.Header)
	}
	resp, err := f.server.Client().Do(r)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}
func (f *ocrHTTPFixture) login() *http.Cookie {
	_, h, _ := f.call("GET", "/api/session", "", nil, nil)
	anon := (&http.Response{Header: h}).Cookies()[0]
	code, h, b := f.call("POST", "/auth/guest", "", anon, nil)
	if code != 200 {
		f.t.Fatal(code, string(b))
	}
	return (&http.Response{Header: h}).Cookies()[0]
}
func newOCRHTTP(t *testing.T) *ocrHTTPFixture {
	t.Helper()
	f := &ocrHTTPFixture{t: t, root: t.TempDir()}
	var err error
	f.state, err = storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.state.Close() })
	f.docs, err = storage.Open(filepath.Join(t.TempDir(), "documents.db"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.docs.Close() })
	f.store, err = documents.New(f.state, f.docs, func() time.Time { return time.Now().Add(time.Duration(f.offset.Load())) }, func(context.Context) (int64, error) { return 1 << 40, nil })
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := websession.New(f.state, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "guest", Lifetime: time.Hour, IdleTimeout: time.Minute, ReceiptLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(f.root, 0700)
	files, err := artifactread.OpenRoot(f.root, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	service := &renderadmission.Service{Records: f.store, Resolver: httpOCRResolver{}, Limits: documents.AdmissionLimits{UserMaxBytes: 1 << 30, GlobalMaxBytes: 1 << 32, JobMaxBytes: 1 << 20, MaxPages: 10, JobTimeout: time.Minute, ReceiptRetention: time.Hour}}
	mux := http.NewServeMux()
	mux.Handle("/auth/", sessions)
	mux.Handle("/api/session", sessions)
	mux.Handle("/api/", documenthttp.New(ocrReadBoundary{f.store, f}, sessions, time.Hour, files, time.Minute, service, nil, nil))
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	f.cookie = f.login()
	if err = f.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&f.owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	f.exec(f.docs, "INSERT INTO owners(id) VALUES(?)", f.owner)
	f.exec(f.docs, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)`, documentID, f.owner, now, now+3600000)
	original := []byte("synthetic source")
	sum := sha256.Sum256(original)
	if err = os.WriteFile(filepath.Join(f.root, "original.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	f.exec(f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_http_001',?,?,'original','ready','original.png','image/png',?,?,1,?)`, documentID, f.owner, len(original), sum[:], now)
	f.exec(f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES(?,?,?,0,'original_http_001','a4','color',300,32,16)`, pageID, documentID, f.owner)
	return f
}
func TestReOCRHTTPCommands(t *testing.T) {
	f := newOCRHTTP(t)
	path := "/api/documents/" + documentID + "/reocr"
	code, h, b := f.call("POST", path, reocrBody, f.cookie, nil)
	if code != 202 {
		t.Fatalf("reOCR admission: want 202, got %d %s", code, b)
	}
	var job documents.JobProjection
	if err := json.Unmarshal(b, &job); err != nil {
		t.Fatal(err)
	}
	if h.Get("Location") != "/api/jobs/"+job.ID {
		t.Fatal(h)
	}
	var snapshot string
	if err := f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", job.ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var input documents.RenderSnapshot
	if err := json.Unmarshal([]byte(snapshot), &input); err != nil || !input.ReOCR || input.Settings.OCR.ModelVersion != "v1" {
		t.Fatal(snapshot, err)
	}
	// Treat the first response as lost; the original key must recover the same job.
	code, _, b = f.call("POST", path, reocrBody, f.cookie, nil)
	var replay documents.JobProjection
	json.Unmarshal(b, &replay)
	if code != 202 || replay.ID != job.ID {
		t.Fatal("replay duplicated admission", code, string(b))
	}
	code, _, _ = f.call("POST", path, strings.Replace(reocrBody, `"ja"`, `"en"`, 1), f.cookie, nil)
	if code != 409 {
		t.Fatal("changed intent", code)
	}
	code, _, _ = f.call("POST", path, reocrBody, f.cookie, func(h http.Header) { h.Set("If-Match", `"9"`); h.Set("Idempotency-Key", "request_stale_http_001") })
	if code != 412 {
		t.Fatal("stale", code)
	}
	for _, op := range []string{"adopt", "discard"} {
		t.Run(op, func(t *testing.T) {
			g := newOCRHTTP(t)
			code, _, b := g.call("POST", "/api/documents/"+documentID+"/ocr-results/"+resultID+"/"+op, `{}`, g.cookie, nil)
			if code != 409 {
				t.Fatalf("unknown candidate: want 409 got %d %s", code, b)
			}
			bundle := g.candidate()
			headers := func(h http.Header) {
				h.Set("If-Match", strconv.Quote(strconv.FormatInt(bundle.InputRevision, 10)))
				h.Set("Idempotency-Key", "request_dispose_http001")
			}
			path := "/api/documents/" + documentID + "/ocr-results/" + resultID + "/" + op
			for _, bad := range []string{`{"page_id":"partial"}`, `{"x":1,"x":2}`, `null`, `[]`, strings.Repeat(" ", 65537)} {
				code, _, _ := g.call("POST", path, bad, g.cookie, headers)
				if code != 400 && code != 413 {
					t.Fatal("malformed disposition", code)
				}
			}
			code, _, _ = g.call("POST", path, `{}`, g.cookie, func(h http.Header) { headers(h); h.Set("If-Match", `"99"`) })
			if code != 412 {
				t.Fatal("stale disposition", code)
			}
			other := g.login()
			code, _, _ = g.call("POST", path, `{}`, other, headers)
			if code != 404 {
				t.Fatal("foreign disposition", code)
			}
			code, h, b := g.call("POST", path, `{}`, g.cookie, headers)
			if code != 200 {
				t.Fatal("candidate disposition", code, string(b))
			}
			var projection documents.Projection
			json.Unmarshal(b, &projection)
			if h.Get("ETag") != strconv.Quote(strconv.FormatInt(projection.Revision, 10)) {
				t.Fatal("ETag", h)
			}
			code, _, replay := g.call("POST", path, `{}`, g.cookie, headers)
			if code != 200 || string(replay) != string(b) {
				t.Fatal("lost response replay", code, string(replay))
			}
			code, _, _ = g.call("POST", strings.Replace(path, resultID, "ocr_result_other001", 1), `{}`, g.cookie, headers)
			if code != 409 {
				t.Fatal("changed disposition replay", code)
			}
			var state string
			g.docs.QueryRow("SELECT state FROM ocr_results WHERE id=?", resultID).Scan(&state)
			if state != map[string]string{"adopt": "adopted", "discard": "discarded"}[op] {
				t.Fatal(state)
			}
		})
	}
}

func (f *ocrHTTPFixture) candidate() documents.OCRResultBundle {
	code, _, body := f.call("POST", "/api/documents/"+documentID+"/reocr", reocrBody, f.cookie, nil)
	if code != 202 {
		f.t.Fatal("candidate admission", code, string(body))
	}
	var job documents.JobProjection
	json.Unmarshal(body, &job)
	claimed, err := f.store.ClaimExport(context.Background(), f.owner, job.ID, "runner_http_0001")
	if err != nil {
		f.t.Fatal(err)
	}
	var snapshot documents.RenderSnapshot
	if err = json.Unmarshal([]byte(claimed.Configuration), &snapshot); err != nil {
		f.t.Fatal(err)
	}
	makeArtifact := func(id, path, media string, data []byte) documents.ArtifactRecord {
		if err := os.WriteFile(filepath.Join(f.root, path), data, 0600); err != nil {
			f.t.Fatal(err)
		}
		return documents.ArtifactRecord{ID: id, RelativePath: path, MediaType: media, Size: int64(len(data)), SHA256: sha256.Sum256(data)}
	}
	originalHash := sha256.Sum256([]byte("synthetic source"))
	b := documents.OCRResultBundle{ID: resultID, JobID: job.ID, InputRevision: job.InputRevision, Settings: snapshot.Settings, Output: makeArtifact("output_http_0001", "output.pdf", "application/pdf", []byte("synthetic PDF")), Pages: []documents.OCRResultPage{{PageID: pageID, OriginalArtifactID: "original_http_001", EffectiveArtifactID: "original_http_001", OriginalSHA256: originalHash, EffectiveSHA256: originalHash, SourceType: "scan", PixelWidth: 32, PixelHeight: 16, PDFTextOrigin: "recognition", Text: makeArtifact("text_http_000001", "text.txt", "text/plain", []byte("<script>alert('OCR')</script> 日本語"))}}}
	if err = f.store.CompleteOCRResult(context.Background(), f.owner, job.ID, "runner_http_0001", job.InputRevision, b); err != nil {
		f.t.Fatal("complete candidate", err)
	}
	return b
}

func TestReOCRHTTPArtifacts(t *testing.T) {
	f := newOCRHTTP(t)
	f.candidate()
	base := "/api/documents/" + documentID + "/ocr-results/" + resultID
	code, _, body := f.call("GET", base, "", f.cookie, nil)
	if code != 200 {
		t.Fatalf("result metadata: want 200 got %d %s", code, body)
	}
	var result documents.OCRResultProjection
	if err := json.Unmarshal(body, &result); err != nil || len(result.Pages) != 1 {
		t.Fatal(string(body), err)
	}
	if strings.Contains(string(body), "relative_path") || strings.Contains(string(body), "text.txt") || strings.Contains(string(body), "<script>") {
		t.Fatal("private content/path in metadata", string(body))
	}
	if result.Pages[0].Artifacts["positions"] != "" || result.Pages[0].TranscriptUnavailable {
		t.Fatal("fabricated positions or missing transcript", result)
	}
	textPath := base + "/pages/" + pageID + "/text"
	code, h, body := f.call("GET", textPath, "", f.cookie, nil)
	if code != 200 || h.Get("Content-Type") != "text/plain" || string(body) != "<script>alert('OCR')</script> 日本語" || h.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatal("inert transcript", code, h, string(body))
	}
	if f.reads.Load() < 2 {
		t.Fatal("missing post-open authorization")
	}
	code, _, body = f.call("GET", base+"/pages/"+pageID+"/effective", "", f.cookie, nil)
	if code != 200 || string(body) != "synthetic source" {
		t.Fatal("recognition source", code, string(body))
	}
	code, h, body = f.call("GET", base+"/output", "", f.cookie, func(h http.Header) { h.Set("Range", "bytes=0-2") })
	if code != 206 || string(body) != "syn" || !strings.Contains(h.Get("Content-Disposition"), "attachment") {
		t.Fatal("result PDF range", code, h, string(body))
	}
	for _, path := range []string{base + "/pages/" + pageID + "/positions", base + "/pages/page_foreign_0001/text", strings.Replace(base, resultID, "result_foreign_001", 1), base + "?path=/etc/passwd", base + "/pages/" + pageID + "/raw", base + "/pages/" + pageID + "/%2e%2e"} {
		code, _, _ := f.call("GET", path, "", f.cookie, nil)
		if code == 200 {
			t.Fatal("substitution/query/traversal accepted", path)
		}
	}
	other := f.login()
	code, _, _ = f.call("GET", textPath, "", other, nil)
	if code != 404 {
		t.Fatal("foreign artifact", code)
	}
	if err := os.WriteFile(filepath.Join(f.root, "text.txt"), []byte(strings.Repeat("x", len("<script>alert('OCR')</script> 日本語"))), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, _ = f.call("GET", textPath, "", f.cookie, nil)
	if code != 503 {
		t.Fatal("corrupt sealed transcript", code)
	}
	for _, mode := range []string{"expiry", "disable"} {
		t.Run(mode+" after open", func(t *testing.T) {
			g := newOCRHTTP(t)
			g.candidate()
			g.beforeRead = func(n int64) {
				if n == 2 {
					if mode == "expiry" {
						g.offset.Store(int64(2 * time.Hour))
					} else {
						g.exec(g.state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), g.owner)
					}
				}
			}
			code, _, b := g.call("GET", textPath, "", g.cookie, nil)
			if code != 404 || strings.Contains(string(b), "<script>") || g.reads.Load() != 2 {
				t.Fatal("post-open authority ignored", code, string(b), g.reads.Load())
			}
		})
	}
}

func TestReOCRHTTPReadHasNoSideEffect(t *testing.T) {
	f := newOCRHTTP(t)
	f.candidate()
	base := "/api/documents/" + documentID + "/ocr-results/" + resultID
	count := func() [3]int {
		var n [3]int
		for i, table := range []string{"jobs", "reservations", "request_receipts"} {
			if err := f.docs.QueryRow("SELECT count(*) FROM " + table).Scan(&n[i]); err != nil {
				t.Fatal(err)
			}
		}
		return n
	}
	before := count()
	for _, path := range []string{base, base + "/pages/" + pageID + "/text", base + "/output"} {
		code, _, b := f.call("GET", path, "", f.cookie, nil)
		if code != 200 {
			t.Fatalf("read-only result route: want 200 got %d %s", code, b)
		}
	}
	if after := count(); after != before {
		t.Fatal("GET admitted work", before, after)
	}
}
func TestReOCRHTTPRejectsMalformed(t *testing.T) {
	f := newOCRHTTP(t)
	path := "/api/documents/" + documentID + "/reocr"
	for _, body := range []string{`{"format":"pdf","ocr":{"enabled":true,"enabled":false}}`, strings.Replace(reocrBody, `"enabled":true`, `"enabled":true,"pages":[]`, 1), strings.Replace(reocrBody, `"pdf"`, `"png"`, 1), strings.Replace(reocrBody, `true`, `false`, 1), `null`, strings.Repeat(" ", 65537)} {
		code, _, b := f.call("POST", path, body, f.cookie, nil)
		if code != 400 && code != 413 {
			t.Fatalf("malformed command accepted: %d %s", code, b)
		}
	}
	for _, name := range []string{"If-Match", "Idempotency-Key"} {
		for _, duplicate := range []bool{false, true} {
			code, _, _ := f.call("POST", path, reocrBody, f.cookie, func(h http.Header) {
				if duplicate {
					h.Add(name, h.Get(name))
				} else {
					h.Del(name)
				}
			})
			if code != 400 {
				t.Fatal(name, duplicate, code)
			}
		}
	}
	code, _, _ := f.call("POST", path, reocrBody, f.cookie, func(h http.Header) { h.Set("Origin", "https://evil.test") })
	if code != 403 {
		t.Fatal("CSRF", code)
	}
	code, _, _ = f.call("POST", path, reocrBody, nil, nil)
	if code != 401 {
		t.Fatal("anonymous", code)
	}
	other := f.login()
	code, _, _ = f.call("POST", path, reocrBody, other, nil)
	if code != 404 {
		t.Fatal("foreign", code)
	}
	f.offset.Store(int64(2 * time.Hour))
	code, _, _ = f.call("POST", path, reocrBody, f.cookie, nil)
	if code != 404 {
		t.Fatal("expired", code)
	}
	f.exec(f.state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), f.owner)
	code, _, _ = f.call("POST", path, reocrBody, f.cookie, nil)
	if code != 401 {
		t.Fatal("disabled", code)
	}
}
