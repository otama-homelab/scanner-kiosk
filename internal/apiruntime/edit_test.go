package apiruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"scanner-kiosk/internal/documents"
	"strings"
	"testing"
	"time"
)

func TestDocumentEditHTTPRevisionReplayAndValidation(t *testing.T) {
	app, err := New(context.Background(), fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	call := func(method, path, body, revision, key string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://scanner.example.test"+path, strings.NewReader(body))
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if revision != "" {
			r.Header.Set("If-Match", revision)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	anonymous := call("GET", "/api/session", "", "", "", nil).Result().Cookies()[0]
	response := call("POST", "/auth/guest", "", "", "request_guest_0001", anonymous)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	guest := response.Result().Cookies()[0]
	var owner string
	if err := app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	commands := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO owners(id) VALUES(?)", []any{owner}},
		{`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES('document_http_edit',?,'document','review',?,?)`, []any{owner, now, now + 3600000}},
		{`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_http_edit','document_http_edit',?,'original','ready','private.png','image/png',1,zeroblob(32),1,?)`, []any{owner, now}},
		{`INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES('page_http_edit_001','document_http_edit',?,0,'original_http_edit','a4','color',300)`, []any{owner}},
	}
	for _, cmd := range commands {
		if _, err := app.docs.Exec(cmd.sql, cmd.args...); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/documents/document_http_edit/pages/page_http_edit_001"
	response = call("PATCH", path, `{"rotation":90}`, `"1"`, "request_rotate_001", guest)
	var value documents.Projection
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || response.Code != 200 || value.Revision != 2 || value.Pages[0].Rotation != 90 || response.Header().Get("ETag") != `"2"` {
		t.Fatal("HTTP rotate", response.Code, response.Body.String())
	}
	original := response.Body.String()
	response = call("PATCH", path, `{"rotation":90}`, `"1"`, "request_rotate_001", guest)
	if response.Code != 200 || response.Body.String() != original {
		t.Fatal("replay", response.Code, response.Body.String())
	}
	for _, test := range []struct {
		name, body, revision, key string
		status                    int
	}{
		{"stale", `{"rotation":180}`, `"1"`, "request_rotate_002", 412},
		{"changed-intent", `{"rotation":180}`, `"1"`, "request_rotate_001", 409},
		{"missing-precondition", `{"rotation":180}`, "", "request_rotate_003", 400},
		{"weak-precondition", `{"rotation":180}`, `W/"2"`, "request_rotate_003", 400},
		{"null", `{"rotation":null}`, `"2"`, "request_rotate_003", 400},
		{"duplicate", `{"rotation":90,"rotation":180}`, `"2"`, "request_rotate_003", 400},
		{"unknown", `{"rotation":90,"owner":"another"}`, `"2"`, "request_rotate_003", 400},
		{"trailing", `{"rotation":90} {}`, `"2"`, "request_rotate_003", 400},
		{"invalid-rotation", `{"rotation":45}`, `"2"`, "request_rotate_003", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := call("PATCH", path, test.body, test.revision, test.key, guest)
			if w.Code != test.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	response = call("PUT", "/api/documents/document_http_edit/pages/order", `{"page_ids":["page_http_edit_001"]}`, `"2"`, "request_reorder_01", guest)
	if response.Code != 200 || response.Header().Get("ETag") != `"3"` {
		t.Fatal("HTTP reorder", response.Code, response.Body.String())
	}
	response = call("DELETE", path, "", `"3"`, "request_delete_01", guest)
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || response.Code != 200 || value.PageCount != 0 || value.Revision != 4 {
		t.Fatal("HTTP delete", response.Code, response.Body.String())
	}
}
