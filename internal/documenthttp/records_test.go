package documenthttp_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
)

const documentID = "document_memory_0001"
const pageID = "page_memory_0000001"
const jobID = "job_memory_00000001"
const requestKey = "request_memory_001"

// Only session bootstrap uses SQLite. All document/job projections, commands
// and file authorizations below use this non-SQL implementation of the port.
type memoryDocuments struct {
	mu             sync.Mutex
	owner          string
	projection     documents.Projection
	reference      documents.ArtifactReference
	edit           documents.EditRequest
	correction     documents.CorrectionRequest
	deletion       documents.DeleteRequest
	key            string
	retention      time.Duration
	failure        error
	reads          int
	denySecondRead bool
	calls          int
}

func (m *memoryDocuments) allow(owner string) error {
	m.calls++
	if owner != m.owner {
		return documents.ErrUnavailable
	}
	return m.failure
}
func (m *memoryDocuments) ReadDocument(_ context.Context, owner, id string) (documents.Projection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.allow(owner)
	if id != documentID {
		return documents.Projection{}, documents.ErrUnavailable
	}
	return m.projection, err
}
func (m *memoryDocuments) ReadHistory(_ context.Context, owner string, limit int, after *documents.Position) ([]documents.Projection, *documents.Position, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.allow(owner); err != nil {
		return nil, nil, err
	}
	if after != nil {
		return []documents.Projection{}, nil, nil
	}
	return []documents.Projection{m.projection}, &documents.Position{ID: documentID, ExpiresAtMS: m.projection.ExpiresAt.UnixMilli()}, nil
}
func (m *memoryDocuments) ReadJob(_ context.Context, owner, id string) (documents.JobProjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.allow(owner)
	return documents.JobProjection{ID: id, DocumentID: documentID, Kind: "render", State: "queued", InputRevision: 7}, err
}
func (m *memoryDocuments) CancelJob(_ context.Context, owner, id, key string, retention time.Duration) (documents.JobProjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.key = key
	m.retention = retention
	err := m.allow(owner)
	return documents.JobProjection{ID: id, DocumentID: documentID, Kind: "render", State: "cancelling", InputRevision: 7}, err
}
func (m *memoryDocuments) Edit(_ context.Context, owner string, r documents.EditRequest, retention time.Duration) (documents.Projection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.edit = r
	m.retention = retention
	return m.projection, m.allow(owner)
}
func (m *memoryDocuments) RequestDelete(_ context.Context, owner string, r documents.DeleteRequest, retention time.Duration) (documents.JobProjection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletion = r
	m.retention = retention
	err := m.allow(owner)
	return documents.JobProjection{ID: jobID, DocumentID: r.DocumentID, Kind: "cleanup", State: "queued", InputRevision: r.Revision}, err
}
func (m *memoryDocuments) file(owner string) (documents.ArtifactReference, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	err := m.allow(owner)
	if m.denySecondRead && m.reads > 1 {
		err = documents.ErrUnavailable
	}
	return m.reference, err
}
func (m *memoryDocuments) ReadPageOriginal(_ context.Context, owner, document, page string) (documents.PageOriginal, error) {
	return m.file(owner)
}
func (m *memoryDocuments) ReadCorrectionArtifact(_ context.Context, owner, document, page string) (documents.ArtifactReference, error) {
	return m.file(owner)
}
func (m *memoryDocuments) ReadPageThumbnail(_ context.Context, owner, document, page string) (documents.ArtifactReference, error) {
	return m.file(owner)
}
func (m *memoryDocuments) ReadCurrentOutput(_ context.Context, owner, document string) (documents.ArtifactReference, error) {
	return m.file(owner)
}
func (m *memoryDocuments) ApplyCorrection(_ context.Context, owner string, r documents.CorrectionRequest, retention time.Duration) (documents.Projection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.correction, m.retention = r, retention
	return m.projection, m.allow(owner)
}
func (m *memoryDocuments) UndoCorrection(_ context.Context, owner string, r documents.CorrectionRequest, retention time.Duration) (documents.Projection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.correction, m.retention = r, retention
	return m.projection, m.allow(owner)
}

type scanFixture struct {
	owner   string
	request documents.ScanRequest
}

func (s *scanFixture) Admit(_ context.Context, owner string, r documents.ScanRequest) (documents.ScanAdmission, error) {
	s.owner = owner
	s.request = r
	return documents.ScanAdmission{JobID: jobID}, nil
}

type renderFixture struct {
	owner   string
	request documents.RenderAdmission
}

func (s *renderFixture) Admit(_ context.Context, owner string, r documents.RenderAdmission) (documents.Admission, error) {
	s.owner = owner
	s.request = r
	return documents.Admission{JobID: jobID}, nil
}

func TestDocumentHTTPRecordsInjectionCommandsAndFileReauthorization(t *testing.T) {
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	sessions, err := websession.New(state, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "guest", Lifetime: time.Hour, IdleTimeout: time.Minute, ReceiptLifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic-pdf")
	if err = os.WriteFile(filepath.Join(root, "sealed.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := artifactread.OpenRoot(root, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	expiry := time.Now().Add(time.Hour)
	records := &memoryDocuments{projection: documents.Projection{ID: documentID, Revision: 7, Purpose: "document", State: "review", ExpiresAt: expiry, Pages: []documents.PageProjection{}}, reference: documents.ArtifactReference{Artifact: documents.ArtifactRecord{ID: "artifact_memory_001", RelativePath: "sealed.pdf", MediaType: "application/pdf", Size: int64(len(data)), SHA256: sha256.Sum256(data)}, ExpiresAtMS: expiry.UnixMilli()}}
	scan, render := &scanFixture{}, &renderFixture{}
	routes := documenthttp.New(records, sessions, time.Hour, files, time.Minute, render, scan, nil)
	mux := http.NewServeMux()
	mux.Handle("/auth/", sessions)
	mux.Handle("/api/session", sessions)
	mux.Handle("/api/", routes)
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	call := func(method, path, body string, cookie *http.Cookie, headers map[string]string) (int, http.Header, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "scanner.example.test"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if method != "GET" && method != "HEAD" {
			r.Header.Set("Origin", "https://scanner.example.test")
			r.Header.Set("Idempotency-Key", requestKey)
			r.Header.Set("If-Match", `"7"`)
			r.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, bodyBytes
	}
	login := func() *http.Cookie {
		t.Helper()
		_, headers, _ := call("GET", "/api/session", "", nil, nil)
		response := http.Response{Header: headers}
		cookies := response.Cookies()
		if len(cookies) != 1 {
			t.Fatal("anonymous cookie missing")
		}
		status, headers, body := call("POST", "/auth/guest", "", cookies[0], nil)
		if status != 200 {
			t.Fatal(status, string(body))
		}
		response.Header = headers
		cookies = response.Cookies()
		if len(cookies) != 1 {
			t.Fatal("guest cookie missing")
		}
		return cookies[0]
	}
	alice := login()
	if err = state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&records.owner); err != nil {
		t.Fatal(err)
	}
	bob := login()
	if status, _, _ := call("GET", "/api/documents", "", nil, nil); status != 401 {
		t.Fatal("anonymous documents", status)
	}
	if records.calls != 0 {
		t.Fatal("unauthenticated request reached backend")
	}
	status, headers, body := call("GET", "/api/documents/"+documentID, "", alice, nil)
	if status != 200 || headers.Get("ETag") != `"7"` || strings.Contains(string(body), "sealed.pdf") {
		t.Fatal("document projection", status, string(body))
	}
	status, _, body = call("GET", "/api/documents?limit=1", "", alice, nil)
	var history struct {
		Items []documents.Projection `json:"items"`
		Next  *string                `json:"next_cursor"`
	}
	if status != 200 || json.Unmarshal(body, &history) != nil || len(history.Items) != 1 || history.Next == nil {
		t.Fatal("history projection", status, string(body))
	}
	if status, _, body = call("GET", "/api/documents?limit=1&cursor="+*history.Next, "", alice, nil); status != 200 || !strings.Contains(string(body), `"items":[]`) {
		t.Fatal("cursor forwarding", status, string(body))
	}
	for _, path := range []string{"/api/documents/" + documentID, "/api/jobs/" + jobID, "/api/documents/" + documentID + "/download", "/api/documents/" + documentID + "/pages/" + pageID + "/preview", "/api/documents/" + documentID + "/pages/" + pageID + "/thumbnail"} {
		if status, _, _ := call("GET", path, "", bob, nil); status != 404 {
			t.Fatal("cross-owner read", path, status)
		}
	}
	for _, path := range []string{"/api/documents/" + documentID + "/download", "/api/documents/" + documentID + "/pages/" + pageID + "/preview", "/api/documents/" + documentID + "/pages/" + pageID + "/thumbnail"} {
		records.mu.Lock()
		records.reads = 0
		records.denySecondRead = false
		records.mu.Unlock()
		status, _, body = call("GET", path, "", alice, nil)
		if status != 200 || string(body) != string(data) {
			t.Fatal("file port", path, status, string(body))
		}
		records.mu.Lock()
		reads := records.reads
		records.reads = 0
		records.denySecondRead = true
		records.mu.Unlock()
		if reads < 3 {
			t.Fatal("file authority not repeated")
		}
		status, _, body = call("GET", path, "", alice, nil)
		if status != 404 || strings.Contains(string(body), string(data)) {
			t.Fatal("revoked file authority leaked bytes", status)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"PATCH", "/api/documents/" + documentID + "/pages/" + pageID, `{"rotation":90}`},
		{"DELETE", "/api/documents/" + documentID, ""},
		{"POST", "/api/jobs/" + jobID + "/cancel", ""},
	} {
		status, _, body = call(tc.method, tc.path, tc.body, bob, nil)
		if status != 404 || strings.Contains(string(body), jobID) {
			t.Fatal("cross-owner command result", tc.path, status)
		}
	}
	records.mu.Lock()
	records.denySecondRead = false
	records.reads = 0
	records.mu.Unlock()
	status, headers, body = call("GET", "/api/documents/"+documentID+"/download", "", alice, map[string]string{"Range": "bytes=1-3"})
	if status != 206 || string(body) != string(data[1:4]) || headers.Get("Content-Range") != "bytes 1-3/13" {
		t.Fatal("range through records port", status, string(body), headers.Get("Content-Range"))
	}
	status, headers, body = call("HEAD", "/api/documents/"+documentID+"/download", "", alice, nil)
	if status != 200 || len(body) != 0 || headers.Get("Content-Length") != "13" {
		t.Fatal("HEAD through records port", status)
	}
	records.mu.Lock()
	before := records.calls
	records.mu.Unlock()
	status, _, _ = call("DELETE", "/api/documents/"+documentID, "", alice, map[string]string{"If-Match": `"07"`})
	records.mu.Lock()
	after := records.calls
	records.mu.Unlock()
	if status != 400 || before != after {
		t.Fatal("malformed command reached backend", status)
	}

	status, _, _ = call("PATCH", "/api/documents/"+documentID+"/pages/"+pageID, `{"rotation":90}`, alice, nil)
	if status != 200 || records.edit.DocumentID != documentID || records.edit.PageID != pageID || records.edit.Revision != 7 || records.edit.IdempotencyKey != requestKey || records.edit.Operation != "rotate" || records.edit.Rotation != 90 || records.retention != time.Hour {
		t.Fatal("edit command forwarding", status)
	}
	status, _, _ = call("POST", "/api/jobs/"+jobID+"/cancel", "", alice, nil)
	if status != 202 || records.key != requestKey {
		t.Fatal("cancel command", status)
	}
	status, headers, _ = call("DELETE", "/api/documents/"+documentID, "", alice, nil)
	if status != 202 || headers.Get("Location") != "/api/jobs/"+jobID || records.deletion.DocumentID != documentID || records.deletion.Revision != 7 || records.deletion.IdempotencyKey != requestKey {
		t.Fatal("delete command", status)
	}
	status, _, _ = call("POST", "/api/documents", `{"scanner_id":"scanner_memory_001","purpose":"document","capture":{"size":"a4","color":"gray","dpi":300}}`, alice, map[string]string{"If-Match": ""})
	// New-document admission requires If-Match to be absent, not an empty field.
	if status != 400 {
		t.Fatal("empty If-Match accepted", status)
	}
	status, _, _ = call("POST", "/api/documents/"+documentID+"/scans", `{"scanner_id":"scanner_memory_001","capture":{"size":"a4","color":"gray","dpi":300}}`, alice, nil)
	if status != 202 || scan.owner != records.owner || scan.request.DocumentID != documentID || scan.request.Revision != 7 || scan.request.ReserveBytes != 0 || scan.request.WorkerBootID != "" {
		t.Fatal("scan policy port", status)
	}
	for _, field := range []string{"insert_before_page_id", "replace_page_id"} {
		payload := `{"scanner_id":"scanner_memory_001","capture":{"size":"a4","color":"gray","dpi":300},"` + field + `":"` + pageID + `"}`
		status, _, _ = call("POST", "/api/documents/"+documentID+"/scans", payload, alice, nil)
		if status != 202 || field == "insert_before_page_id" && scan.request.InsertBeforePageID != pageID || field == "replace_page_id" && scan.request.ReplacePageID != pageID {
			t.Fatal("placement forwarding", field, status)
		}
	}
	for _, extra := range []string{`"insert_before_page_id":"x"`, `"replace_page_id":null`, `"replace_page_id":""`, `"insert_before_page_id":"` + pageID + `","replace_page_id":"` + pageID + `"`} {
		payload := `{"scanner_id":"scanner_memory_001","capture":{"size":"a4","color":"gray","dpi":300},` + extra + `}`
		status, _, _ = call("POST", "/api/documents/"+documentID+"/scans", payload, alice, nil)
		if status != 400 {
			t.Fatal("invalid placement accepted", extra, status)
		}
	}
	status, _, _ = call("POST", "/api/documents/"+documentID+"/render", `{"format":"pdf","ocr":{"enabled":false}}`, alice, nil)
	if status != 202 || render.owner != records.owner || render.request.DocumentID != documentID || render.request.Revision != 7 || render.request.ReserveBytes != 0 || render.request.Settings.Format != "pdf" {
		t.Fatal("render policy port", status)
	}
	for _, failure := range []struct {
		err    error
		status int
	}{{documents.ErrRevision, 412}, {documents.ErrCommitUncertain, 503}, {documents.ErrUnavailable, 404}} {
		records.mu.Lock()
		records.failure = failure.err
		records.mu.Unlock()
		status, _, body = call("DELETE", "/api/documents/"+documentID, "", alice, nil)
		if status != failure.status || strings.Contains(string(body), jobID) {
			t.Fatal("unconfirmed result exposed", status, string(body))
		}
	}
}
