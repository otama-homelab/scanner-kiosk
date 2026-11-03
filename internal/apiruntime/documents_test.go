package apiruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"scanner-kiosk/internal/documents"
	"testing"
	"time"
)

func TestAuthenticatedDocumentHTTPIsolationAndPagination(t *testing.T) {
	app, err := New(context.Background(), fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	call := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://scanner.example.test"+path, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if method == "POST" {
			r.Header.Set("Origin", "https://scanner.example.test")
			r.Header.Set("Idempotency-Key", "request_guest_0001")
		}
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/documents", nil); w.Code != 401 {
		t.Fatal("anonymous history", w.Code)
	}
	login := func() *http.Cookie {
		w := call("GET", "/api/session", nil)
		anonymous := w.Result().Cookies()[0]
		w = call("POST", "/auth/guest", anonymous)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Result().Cookies()[0]
	}
	first := login()
	var owner string
	if err := app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	second := login()
	if _, err := app.docs.Exec("INSERT INTO owners(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	for _, id := range []string{"document_http_0001", "document_http_0002"} {
		if _, err := app.docs.Exec(`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)`, id, owner, now, now+3600000); err != nil {
			t.Fatal(err)
		}
	}
	type history struct {
		Items []documents.Projection `json:"items"`
		Next  *string                `json:"next_cursor"`
	}
	if _, err := app.docs.Exec(`INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES('job_http_resume_0001','document_http_0001',?,'render','queued',1,'{}',?,?)`, owner, now, now+60000); err != nil {
		t.Fatal(err)
	}
	w := call("GET", "/api/documents?limit=1", first)
	var page history
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 || page.Next == nil {
		t.Fatal("history failed", w.Code, w.Body.String())
	}
	w = call("GET", "/api/documents?limit=1&cursor="+*page.Next, first)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || len(page.Items) != 1 || page.Next != nil || page.Items[0].ID != "document_http_0002" {
		t.Fatal("next history failed", w.Code, w.Body.String())
	}
	w = call("GET", "/api/documents/document_http_0001", first)
	if w.Code != 200 || w.Header().Get("ETag") != "\"1\"" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("document projection", w.Code, w.Body.String())
	}
	var projected documents.Projection
	if json.Unmarshal(w.Body.Bytes(), &projected) != nil || projected.ActiveJobID == nil || *projected.ActiveJobID != "job_http_resume_0001" {
		t.Fatal("owned active work missing", w.Body.String())
	}
	if active := call("GET", "/api/jobs/"+*projected.ActiveJobID, first); active.Code != 200 {
		t.Fatal("discovered job not accessible to owner", active.Code)
	}
	w = call("GET", "/api/documents/document_http_0001", second)
	if w.Code != 404 {
		t.Fatal("cross owner get", w.Code)
	}
	w = call("GET", "/api/documents", second)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 0 {
		t.Fatal("cross owner history", w.Body.String())
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=1&limit=2", "?cursor=bad", "?owner=another", "?limit=%XX"} {
		w = call("GET", "/api/documents"+query, first)
		if w.Code != 400 {
			t.Fatal("invalid history query", query, w.Code)
		}
	}
	// Disable must be observed even with an otherwise valid session cookie.
	if _, err := app.state.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", now, owner); err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/documents", first)
	if w.Code != 401 {
		t.Fatal("disabled principal history", w.Code)
	}
}
