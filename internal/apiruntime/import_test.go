package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/documents"
)

func TestHTTPImportPersistsOwnedQueuedSource(t *testing.T) {
	c := fixture(t)
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewTLSServer(app.Handler())
	defer server.Close()
	client := server.Client()
	call := func(method, path string, body []byte, cookie *http.Cookie, headers map[string]string) (int, http.Header, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "scanner.example.test"
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if method == "POST" {
			r.Header.Set("Origin", "https://scanner.example.test")
			r.Header.Set("Idempotency-Key", "request_import_http1")
		}
		for k, v := range headers {
			if v == "" {
				r.Header.Del(k)
			} else {
				r.Header.Set(k, v)
			}
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, raw
	}
	login := func() *http.Cookie {
		status, headers, _ := call("GET", "/api/session", nil, nil, nil)
		if status != 200 {
			t.Fatal(status)
		}
		anonymous := (&http.Response{Header: headers}).Cookies()[0]
		status, headers, _ = call("POST", "/auth/guest", nil, anonymous, nil)
		if status != 200 {
			t.Fatal(status)
		}
		return (&http.Response{Header: headers}).Cookies()[0]
	}
	raw := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("source fixture bytes\n"), 5000)...)
	sum := sha256.Sum256(raw)
	headers := map[string]string{"Content-Type": "application/pdf", "X-Content-SHA256": hex.EncodeToString(sum[:])}
	if status, _, _ := call("POST", "/api/documents/import", raw, nil, headers); status != 401 {
		t.Fatal("anonymous upload", status)
	}
	guest := login()
	foreign := map[string]string{"Content-Type": "application/pdf", "X-Content-SHA256": headers["X-Content-SHA256"], "Origin": "https://foreign.invalid"}
	if status, _, _ := call("POST", "/api/documents/import", raw, guest, foreign); status != 403 && status != 400 {
		t.Fatal("foreign origin upload", status)
	}
	for _, bad := range []map[string]string{
		{"Content-Type": "application/pdf"},
		{"Content-Type": "application/pdf", "X-Content-SHA256": "bad"},
		{"Content-Type": "application/json", "X-Content-SHA256": headers["X-Content-SHA256"]},
		{"Content-Type": "application/pdf", "X-Content-SHA256": headers["X-Content-SHA256"], "If-Match": "\"1\""},
	} {
		if status, _, _ := call("POST", "/api/documents/import", raw, guest, bad); status != 400 && status != 415 {
			t.Fatal("invalid upload accepted", status)
		}
	}
	var count int
	if err = app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&count); err != nil || count != 0 {
		t.Fatal("rejection created work", count, err)
	}
	status, resultHeaders, result := call("POST", "/api/documents/import", raw, guest, headers)
	var job documents.JobProjection
	if status != 202 || json.Unmarshal(result, &job) != nil || job.Kind != "import" || job.State != "queued" || resultHeaders.Get("Location") != "/api/jobs/"+job.ID {
		t.Fatal("upload", status, string(result))
	}
	var path, state string
	var size int64
	if err = app.docs.QueryRow("SELECT a.relative_path,i.state,a.byte_size FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id WHERE i.job_id=?", job.ID).Scan(&path, &state, &size); err != nil || state != "ready" || size != int64(len(raw)) {
		t.Fatal("body not sealed", state, size, err)
	}
	saved, err := os.ReadFile(filepath.Join(c.DocumentDir, path))
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("HTTP bytes changed", err)
	}
	status, _, result = call("POST", "/api/documents/import", raw, guest, headers)
	var replay documents.JobProjection
	if status != 202 || json.Unmarshal(result, &replay) != nil || replay.ID != job.ID {
		t.Fatal("upload replay", status, string(result))
	}
	changed := append(append([]byte{}, raw...), 1)
	sum = sha256.Sum256(changed)
	headers["X-Content-SHA256"] = hex.EncodeToString(sum[:])
	if status, _, _ := call("POST", "/api/documents/import", changed, guest, headers); status != 409 {
		t.Fatal("changed input reused key", status)
	}
	wrong := sha256.Sum256([]byte("wrong digest"))
	badDigest := map[string]string{"Content-Type": "application/pdf", "X-Content-SHA256": hex.EncodeToString(wrong[:]), "Idempotency-Key": "request_http_badsha1"}
	if status, _, _ := call("POST", "/api/documents/import", raw, guest, badDigest); status != 400 {
		t.Fatal("claimed checksum trusted", status)
	}
	if err = app.docs.QueryRow("SELECT count(*) FROM jobs WHERE kind='import' AND state='failed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("bad checksum did not finalize failure", count, err)
	}
	other := login()
	if status, _, _ := call("GET", "/api/jobs/"+job.ID, nil, other, nil); status != 404 {
		t.Fatal("cross-owner import read", status)
	}
	if err = app.docs.QueryRow("SELECT count(*) FROM pages").Scan(&count); err != nil || count != 0 {
		t.Fatal("raw unparsed upload published pages")
	}
}
