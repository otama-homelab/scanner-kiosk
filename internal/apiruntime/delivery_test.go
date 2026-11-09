package apiruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
)

const deliveryDoc = "document_delivery_http01"
const deliveryTask = "12345678-1234-4234-8234-123456789abc"
const deliveryBody = `{"destination":"paperless","confirmed":true}`

type deliveryFixture struct {
	app         *App
	cookie      *http.Cookie
	owner       string
	p           config.Paperless
	data        []byte
	root        string
	gets, posts atomic.Int32
	marker      string
	mode        string
}

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	f := &deliveryFixture{data: []byte("%PDF-1.4\nSynthetic delivery transport fixture\n")}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			f.posts.Add(1)
			w.WriteHeader(500)
			return
		}
		f.gets.Add(1)
		if r.Header.Get("Accept") != "application/json; version=9" || r.Header.Get("Authorization") != "Token synthetic-delivery-secret" {
			t.Error("remote authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		if f.mode == "missing" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Path == "/api/tasks/" {
			marker := f.marker
			if f.mode == "marker" {
				marker = "other-owner.pdf"
			}
			fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":"SUCCESS","related_document":42}]`, deliveryTask, marker)
		} else if r.URL.Path == "/api/documents/42/metadata/" {
			sum := sha256.Sum256(f.data)
			if f.mode == "checksum" {
				sum = sha256.Sum256([]byte("different"))
			}
			fmt.Fprintf(w, `{"original_checksum":"%x"}`, sum)
		} else {
			w.WriteHeader(503)
		}
	}))
	t.Cleanup(remote.Close)
	values := map[string]string{"SCANNER_PAPERLESS_ENABLED": "true", "SCANNER_PAPERLESS_DESTINATION": "Household archive", "SCANNER_PAPERLESS_BASE_URL": remote.URL, "SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP": "true", "SCANNER_PAPERLESS_TOKEN_ENV": "SCANNER_PAPERLESS_TOKEN_TEST", "SCANNER_PAPERLESS_TOKEN_TEST": "synthetic-delivery-secret"}
	var err error
	f.p, err = config.LoadPaperless(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	c := fixture(t)
	c.Paperless = f.p
	f.root = c.DocumentDir
	f.app, err = New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.app.Close() })
	anon := previewRequest(f.app, "GET", "/api/session", nil).Result().Cookies()[0]
	f.cookie = previewRequest(f.app, "POST", "/auth/guest", anon).Result().Cookies()[0]
	if err = f.app.state.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&f.owner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	sum := sha256.Sum256(f.data)
	if err = os.WriteFile(filepath.Join(f.root, "delivery.pdf"), f.data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO owners(id) VALUES(?)`, []any{f.owner}},
		{`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','ready',?,?)`, []any{deliveryDoc, f.owner, now, now + 3600000}},
		{`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('output_delivery_http01',?,?,'output','ready','delivery.pdf','application/pdf',?,?,1,?)`, []any{deliveryDoc, f.owner, len(f.data), sum[:], now}},
		{`INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,'output_delivery_http01')`, []any{deliveryDoc, f.owner}},
	} {
		if _, err = f.app.docs.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *deliveryFixture) call(method, path, body, key, revision string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://scanner.example.test"+path, strings.NewReader(body))
	if f.cookie != nil {
		r.AddCookie(f.cookie)
	}
	if method != "GET" {
		r.Header.Set("Origin", "https://scanner.example.test")
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if revision != "" {
		r.Header.Set("If-Match", revision)
	}
	w := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(w, r)
	return w
}
func (f *deliveryFixture) admit(t *testing.T) string {
	t.Helper()
	w := f.call("POST", "/api/documents/"+deliveryDoc+"/deliveries", deliveryBody, "request_delivery_http01", `"1"`)
	var d struct{ ID, State, Destination string }
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 202 || d.ID == "" || d.State != "queued" || d.Destination != "paperless" {
		t.Fatalf("explicit reviewed delivery expected 202 queued: %d %s", w.Code, w.Body.String())
	}
	f.marker = d.ID + ".pdf"
	return d.ID
}
func (f *deliveryFixture) store(t *testing.T) *documents.Store {
	t.Helper()
	s, err := documents.New(f.app.state, f.app.docs, time.Now, func(context.Context) (int64, error) { return 1 << 40, nil })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDeliveryRoutesOwnerRevisionTTL(t *testing.T) {
	f := newDeliveryFixture(t)
	f.admit(t)
	for _, p := range []string{"/api/documents/" + deliveryDoc + "/deliveries/current", "/api/deliveries/" + strings.TrimSuffix(f.marker, ".pdf")} {
		w := f.call("GET", p, "", "", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{f.p.BaseURL, f.p.ConfigFingerprint, f.p.DestinationID, "synthetic-delivery-secret", "delivery.pdf", "relative_path", "OwnerID", "TaskID"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private projection", secret)
			}
		}
	}
	if f.posts.Load() != 0 || f.gets.Load() != 0 {
		t.Fatal("admission/read made remote request")
	}
}
func TestDeliveryReplayAndCurrentStatus(t *testing.T) {
	f := newDeliveryFixture(t)
	id := f.admit(t)
	s := f.store(t)
	if _, err := s.ClaimDeliveryAttempt(context.Background(), f.owner, id, "runner_http_delivery01"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDeliveryUnknown(context.Background(), f.owner, id); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request_delivery_http01", "request_delivery_http02"} {
		w := f.call("POST", "/api/documents/"+deliveryDoc+"/deliveries", deliveryBody, key, `"1"`)
		if w.Code != 202 || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), `"unknown"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := f.call("GET", "/api/documents/"+deliveryDoc+"/deliveries/current", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"unknown"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	var n int
	if err := f.app.docs.QueryRow("SELECT count(*) FROM deliveries").Scan(&n); err != nil || n != 1 || f.posts.Load() != 0 || f.gets.Load() != 0 {
		t.Fatal("reload resend", n, err)
	}
}
func TestDeliveryUnknownRecovery(t *testing.T) {
	f := newDeliveryFixture(t)
	id := f.admit(t)
	s := f.store(t)
	if _, err := s.ClaimDeliveryAttempt(context.Background(), f.owner, id, "runner_http_delivery01"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDeliveryUnknown(context.Background(), f.owner, id); err != nil {
		t.Fatal(err)
	}
	w := f.call("POST", "/api/deliveries/"+id+"/reconcile", `{"task_id":"`+deliveryTask+`"}`, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"confirmed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if f.posts.Load() != 0 || f.gets.Load() != 2 {
		t.Fatal("GET-only exact recovery", f.posts.Load(), f.gets.Load())
	}
}
func TestDeliveryLocalCancel(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			f := newDeliveryFixture(t)
			id := f.admit(t)
			if attempted {
				if _, err := f.store(t).ClaimDeliveryAttempt(context.Background(), f.owner, id, "runner_http_delivery01"); err != nil {
					t.Fatal(err)
				}
			}
			w := f.call("POST", "/api/deliveries/"+id+"/cancel", `{}`, "", "")
			want := `"cancelled"`
			if attempted {
				want = `"remote_processing_may_continue":true`
			}
			if w.Code != 200 || !strings.Contains(w.Body.String(), want) || f.posts.Load() != 0 || f.gets.Load() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
