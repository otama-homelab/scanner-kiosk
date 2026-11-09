package apiruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
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
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/workercatalog"
	"scanner-kiosk/internal/workeridentity"
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

func newDeliveryFixture(t *testing.T, ttl ...time.Duration) *deliveryFixture {
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
			if r.URL.RawQuery != "task_id="+deliveryTask {
				t.Error("recovery did not query exact task UUID")
			}
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
	lifetime := time.Hour
	if len(ttl) > 0 {
		lifetime = ttl[0]
	}
	sum := sha256.Sum256(f.data)
	if err = os.WriteFile(filepath.Join(f.root, "delivery.pdf"), f.data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO owners(id) VALUES(?)`, []any{f.owner}},
		{`INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','ready',?,?)`, []any{deliveryDoc, f.owner, now, now + lifetime.Milliseconds()}},
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

func TestDeliveryRouteAdversarialInputs(t *testing.T) {
	for _, name := range []string{"foreign", "disabled", "expired-owner", "expired-document", "stale", "missing-output", "unreviewed", "non-pdf", "replaced-output", "anonymous", "origin", "csrf", "duplicate-key", "duplicate-revision", "uncanonical-revision", "duplicate-media", "media", "unknown-field", "duplicate-json", "case-field", "null", "array", "trailing", "oversize", "destination-url", "unconfirmed", "query", "invalid-key"} {
		t.Run(name, func(t *testing.T) {
			var f *deliveryFixture
			if name == "expired-document" {
				f = newDeliveryFixture(t, 500*time.Millisecond)
			} else {
				f = newDeliveryFixture(t)
			}
			path := "/api/documents/" + deliveryDoc + "/deliveries"
			body, key, revision := deliveryBody, "request_delivery_http01", `"1"`
			want := 400
			switch name {
			case "foreign":
				anon := previewRequest(f.app, "GET", "/api/session", nil).Result().Cookies()[0]
				f.cookie = previewRequest(f.app, "POST", "/auth/guest", anon).Result().Cookies()[0]
				want = 404
			case "disabled":
				if _, err := f.app.state.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), f.owner); err != nil {
					t.Fatal(err)
				}
				want = 401
			case "expired-owner":
				if _, err := f.app.state.Exec("UPDATE sessions SET expiry=julianday('now')+0.000001"); err != nil {
					t.Fatal(err)
				}
				time.Sleep(100 * time.Millisecond)
				want = 401
			case "expired-document":
				time.Sleep(510 * time.Millisecond)
				want = 404
			case "stale":
				revision = `"2"`
				want = 412
			case "missing-output":
				if _, err := f.app.docs.Exec("DELETE FROM published_outputs"); err != nil {
					t.Fatal(err)
				}
				want = 404
			case "unreviewed":
				if _, err := f.app.docs.Exec("UPDATE documents SET state='review' WHERE id=?", deliveryDoc); err != nil {
					t.Fatal(err)
				}
				want = 404
			case "non-pdf", "replaced-output":
				media, rev := "image/png", 1
				if name == "replaced-output" {
					media, rev = "application/pdf", 1
				}
				if _, err := f.app.docs.Exec(`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('output_delivery_other01',?,?,'output','ready','other-output',?,40,zeroblob(32),?,?)`, deliveryDoc, f.owner, media, rev, time.Now().UnixMilli()); err != nil {
					t.Fatal(err)
				}
				if name == "non-pdf" {
					if _, err := f.app.docs.Exec("UPDATE published_outputs SET artifact_id='output_delivery_other01'"); err != nil {
						t.Fatal(err)
					}
					want = 409
				} else {
					if _, err := f.app.docs.Exec("DELETE FROM published_outputs"); err != nil {
						t.Fatal(err)
					}
					if _, err := f.app.docs.Exec("UPDATE documents SET revision=2"); err != nil {
						t.Fatal(err)
					}
					want = 412
				}
			case "anonymous":
				f.cookie = nil
				want = 401
			case "uncanonical-revision":
				revision = `"01"`
			case "media":
				want = 415
			case "unknown-field":
				body = `{"destination":"paperless","confirmed":true,"url":"https://private.invalid/"}`
			case "duplicate-json":
				body = `{"destination":"paperless","confirmed":false,"confirmed":true}`
			case "case-field":
				body = `{"Destination":"paperless","confirmed":true}`
			case "null":
				body = `{"destination":"paperless","confirmed":null}`
			case "array":
				body = `[]`
			case "trailing":
				body += `{}`
			case "oversize":
				body = `{"destination":"` + strings.Repeat("x", 4096) + `","confirmed":true}`
				want = 413
			case "destination-url":
				body = `{"destination":"https://private.invalid/","confirmed":true}`
			case "unconfirmed":
				body = `{"destination":"paperless","confirmed":false}`
			case "query":
				path += "?url=https://private.invalid"
			case "invalid-key":
				key = "short"
			}
			r := httptest.NewRequest("POST", "https://scanner.example.test"+path, strings.NewReader(body))
			if f.cookie != nil {
				r.AddCookie(f.cookie)
			}
			r.Header.Set("Origin", "https://scanner.example.test")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", key)
			r.Header.Set("If-Match", revision)
			switch name {
			case "origin":
				r.Header.Set("Origin", "https://other.invalid")
				want = 403
			case "csrf":
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				want = 403
			case "duplicate-key":
				r.Header.Add("Idempotency-Key", key)
			case "duplicate-revision":
				r.Header.Add("If-Match", revision)
			case "duplicate-media":
				r.Header.Add("Content-Type", "application/json")
				want = 415
			case "media":
				r.Header.Set("Content-Type", "text/plain")
			}
			w := httptest.NewRecorder()
			f.app.Handler().ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s expected%d got%d %s", name, want, w.Code, w.Body.String())
			}
			if f.gets.Load() != 0 || f.posts.Load() != 0 {
				t.Fatal("rejection contacted provider")
			}
			for _, secret := range []string{f.p.BaseURL, f.p.ConfigFingerprint, f.p.DestinationID, "synthetic-delivery-secret", "private.invalid", "delivery.pdf"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("rejection leaked", secret)
				}
			}
		})
	}
}

func TestDeliveryRecoveryOwnership(t *testing.T) {
	for _, mode := range []string{"marker", "missing", "checksum", "drift", "destination", "foreign-task", "foreign-owner", "expired", "stored-task", "wrong-uuid", "malformed", "duplicate", "case", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			var f *deliveryFixture
			if mode == "expired" {
				f = newDeliveryFixture(t, 500*time.Millisecond)
			} else {
				f = newDeliveryFixture(t)
			}
			s := f.store(t)
			var id string
			if mode == "drift" || mode == "destination" {
				r := documents.DeliveryRequest{DocumentID: deliveryDoc, Revision: 1, IdempotencyKey: "request_delivery_http01", DestinationID: f.p.DestinationID, ConfigFingerprint: f.p.ConfigFingerprint}
				if mode == "drift" {
					r.ConfigFingerprint = strings.Repeat("b", 64)
				} else {
					r.DestinationID = "other-fixed-destination"
				}
				d, err := s.AdmitDelivery(context.Background(), f.owner, r)
				if err != nil {
					t.Fatal(err)
				}
				id = d.ID
				f.marker = id + ".pdf"
			} else {
				id = f.admit(t)
			}
			if _, err := s.ClaimDeliveryAttempt(context.Background(), f.owner, id, "runner_http_delivery01"); err != nil {
				t.Fatal(err)
			}
			if err := s.MarkDeliveryUnknown(context.Background(), f.owner, id); err != nil {
				t.Fatal(err)
			}
			f.mode = mode
			body := `{"task_id":"` + deliveryTask + `"}`
			want, gets := 200, int32(1)
			switch mode {
			case "checksum":
				gets = 2
			case "drift", "destination":
				gets = 0
			case "foreign-task":
				f.marker = "delivery_otherowner001.pdf"
			case "foreign-owner":
				anon := previewRequest(f.app, "GET", "/api/session", nil).Result().Cookies()[0]
				f.cookie = previewRequest(f.app, "POST", "/auth/guest", anon).Result().Cookies()[0]
				want, gets = 404, 0
			case "expired":
				time.Sleep(510 * time.Millisecond)
				want, gets = 410, 0
			case "stored-task":
				if err := s.AcceptDelivery(context.Background(), f.owner, id, deliveryTask); err != nil {
					t.Fatal(err)
				}
				body = `{}`
				f.mode = "marker"
			case "wrong-uuid":
				body = `{"task_id":"12345678-1234-4234-8234-123456789ABC"}`
				want, gets = 400, 0
			case "malformed":
				body = `{"task_id":"https://private.invalid/"}`
				want, gets = 400, 0
			case "duplicate":
				body = `{"task_id":"` + deliveryTask + `","task_id":"` + deliveryTask + `"}`
				want, gets = 400, 0
			case "case":
				body = `{"Task_id":"` + deliveryTask + `"}`
				want, gets = 400, 0
			case "oversize":
				body = `{"task_id":"` + strings.Repeat("a", 4096) + `"}`
				want, gets = 413, 0
			}
			w := f.call("POST", "/api/deliveries/"+id+"/reconcile", body, "", "")
			if w.Code != want {
				t.Fatalf("%s expected%d got%d %s", mode, want, w.Code, w.Body.String())
			}
			if want == 200 && !strings.Contains(w.Body.String(), `"state":"unknown"`) {
				t.Fatal("mismatch must remain unknown", w.Body.String())
			}
			if f.posts.Load() != 0 || f.gets.Load() != gets {
				t.Fatal("GET-only bounded recovery", f.posts.Load(), f.gets.Load(), gets)
			}
			d, err := s.ReadDelivery(context.Background(), f.owner, id)
			if mode != "expired" && err != nil {
				t.Fatal(err)
			}
			if mode != "stored-task" && d.TaskID != "" {
				t.Fatal("mismatch bound remote task")
			}
			for _, secret := range []string{f.p.BaseURL, f.p.ConfigFingerprint, f.p.DestinationID, "other-owner", "private.invalid", "original_checksum", deliveryTask} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("remote predicate leaked", secret)
				}
			}
		})
	}
}

func TestDeliveryUnknownPreservesNormalSave(t *testing.T) {
	for _, state := range []string{"unknown", "failed", "health-failure"} {
		t.Run(state, func(t *testing.T) {
			f := newDeliveryFixture(t)
			id := f.admit(t)
			s := f.store(t)
			if _, err := s.ClaimDeliveryAttempt(context.Background(), f.owner, id, "runner_http_delivery01"); err != nil {
				t.Fatal(err)
			}
			if state == "failed" {
				if err := s.AcceptDelivery(context.Background(), f.owner, id, deliveryTask); err != nil {
					t.Fatal(err)
				}
				if err := s.FailDelivery(context.Background(), f.owner, id, deliveryTask); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.MarkDeliveryUnknown(context.Background(), f.owner, id); err != nil {
					t.Fatal(err)
				}
			}
			if state == "health-failure" {
				w := f.call("POST", "/api/capabilities/paperless/health", `{}`, "", "")
				if w.Code != 200 || !strings.Contains(w.Body.String(), "unavailable") {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			w := f.call("GET", "/api/documents/"+deliveryDoc, "", "", "")
			if w.Code != 200 || w.Header().Get("ETag") != `"1"` {
				t.Fatal("normal read", w.Code, w.Body.String())
			}
			server := httptest.NewServer(f.app.Handler())
			defer server.Close()
			request, _ := http.NewRequest("GET", server.URL+"/api/documents/"+deliveryDoc+"/download", nil)
			request.Host = "scanner.example.test"
			request.AddCookie(f.cookie)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			download, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || !bytes.Equal(download, f.data) {
				t.Fatal("normal download", response.StatusCode, err)
			}
			onDisk, err := os.ReadFile(filepath.Join(f.root, "delivery.pdf"))
			if err != nil || sha256.Sum256(onDisk) != sha256.Sum256(f.data) {
				t.Fatal("delivery changed output", err)
			}
			// A real original page and live catalogue exercise edit and ordinary save admission.
			var pngData bytes.Buffer
			if err = png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(pngData.Bytes())
			if err = os.WriteFile(filepath.Join(f.root, "original.png"), pngData.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = f.app.docs.Exec(`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('original_delivery_http01',?,?,'original','ready','original.png','image/png',?,?,1,?)`, deliveryDoc, f.owner, pngData.Len(), hash[:], time.Now().UnixMilli()); err != nil {
				t.Fatal(err)
			}
			if _, err = f.app.docs.Exec(`INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_delivery_http01',?,?,0,'original_delivery_http01','a4','color',300,16,16)`, deliveryDoc, f.owner); err != nil {
				t.Fatal(err)
			}
			w = f.call("PATCH", "/api/documents/"+deliveryDoc+"/pages/page_delivery_http01", `{"rotation":90}`, "request_delivery_edit01", `"1"`)
			if w.Code != 200 {
				t.Fatal("normal edit", w.Code, w.Body.String())
			}
			guard, err := workeridentity.Acquire(f.root)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			catalog, err := workercatalog.New(f.app.docs, time.Now, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := catalog.Begin(context.Background(), guard)
			if err != nil {
				t.Fatal(err)
			}
			if err = catalog.Publish(context.Background(), lease, renderadmission.CatalogSnapshot{}); err != nil {
				t.Fatal(err)
			}
			w = f.call("POST", "/api/documents/"+deliveryDoc+"/render", `{"format":"pdf","ocr":{"enabled":false}}`, "request_delivery_save01", `"2"`)
			if w.Code != 202 {
				t.Fatal("normal save", w.Code, w.Body.String())
			}
			onDisk, err = os.ReadFile(filepath.Join(f.root, "delivery.pdf"))
			if err != nil || sha256.Sum256(onDisk) != sha256.Sum256(f.data) {
				t.Fatal("old output changed", err)
			}
			if f.posts.Load() != 0 {
				t.Fatal("ordinary work uploaded")
			}
			t.Logf("baseline read/download/edit/render admission pass; remote POST=%d GET=%d", f.posts.Load(), f.gets.Load())
		})
	}
}

func TestDeliveryOutsideActiveJobs(t *testing.T) {
	f := newDeliveryFixture(t)
	f.admit(t)
	var n int
	if err := f.app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&n); err != nil || n != 0 {
		t.Fatal("delivery entered jobs", n, err)
	}
	w := f.call("GET", "/api/documents/"+deliveryDoc, "", "", "")
	var d documents.Projection
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 200 || d.ActiveJobID != nil {
		t.Fatal("delivery occupied document job", w.Code, w.Body.String(), err)
	}
	denied := false
	for i := 0; i < 400; i++ {
		w = f.call("GET", "/api/documents/"+deliveryDoc+"/deliveries/current", "", "", "")
		if w.Code == 429 {
			denied = true
			break
		}
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if !denied || f.posts.Load() != 0 || f.gets.Load() != 0 {
		t.Fatal("rate guard missing or remote request")
	}
}
