//go:build linux

package workerruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/apiruntime"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/documents"
)

func TestPaperlessCompositionIdentity(t *testing.T) {
	c, store, db, _ := fixture(t)
	admit(t, store)
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	output, err := store.GetExport(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	var gets, posts atomic.Int32
	var delivery documents.Delivery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token synthetic-role-secret" || r.Header.Get("Accept") != "application/json; version=9" {
			t.Error("roles did not share pinned adapter config")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Version", "3.4.2")
		w.Header().Set("X-Api-Version", "10")
		if r.Method == "POST" && r.URL.Path == "/api/documents/post_document/" {
			posts.Add(1)
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if len(r.MultipartForm.File["document"]) != 1 || r.MultipartForm.File["document"][0].Filename != delivery.Filename {
				t.Error("worker uploaded outside immutable admitted identity")
			}
			fmt.Fprintf(w, "%q", deliveryTask)
			return
		}
		gets.Add(1)
		switch r.URL.Path {
		case "/api/":
			fmt.Fprint(w, `{"documents":"https://never-follow.invalid/documents/","tasks":"https://never-follow.invalid/tasks/"}`)
		case "/api/tasks/":
			if r.URL.RawQuery != "task_id="+deliveryTask {
				t.Error("non-exact task lookup")
			}
			fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":"SUCCESS","related_document":42}]`, deliveryTask, delivery.Filename)
		case "/api/documents/42/metadata/":
			fmt.Fprintf(w, `{"original_checksum":"%x"}`, output.SHA256)
		default:
			t.Error("unexpected remote path", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	values := map[string]string{"SCANNER_PUBLIC_URL": "https://scanner.example.test", "SCANNER_AUTH_MODE": "guest", "SCANNER_STATE_DIR": c.StateDir, "SCANNER_DOCUMENT_DIR": c.DocumentDir, "SCANNER_PAPERLESS_ENABLED": "true", "SCANNER_PAPERLESS_DESTINATION": "Archive", "SCANNER_PAPERLESS_BASE_URL": server.URL, "SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP": "true", "SCANNER_PAPERLESS_TOKEN_ENV": "SCANNER_PAPERLESS_TOKEN_ROLES", "SCANNER_PAPERLESS_TOKEN_ROLES": "synthetic-role-secret"}
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	apiConfig, err := config.Load(lookup)
	if err != nil {
		t.Fatal(err)
	}
	workerConfig, err := config.LoadWorker(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if apiConfig.Paperless.DestinationID != workerConfig.Paperless.DestinationID || apiConfig.Paperless.ConfigFingerprint != workerConfig.Paperless.ConfigFingerprint {
		t.Fatal("role destination drift")
	}
	delivery, err = store.AdmitDelivery(context.Background(), owner, documents.DeliveryRequest{DocumentID: document, Revision: current.Revision, IdempotencyKey: "request_role_delivery_01", DestinationID: apiConfig.Paperless.DestinationID, ConfigFingerprint: apiConfig.Paperless.ConfigFingerprint})
	if err != nil {
		t.Fatal(err)
	}
	app, err := apiruntime.New(context.Background(), apiConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://scanner.example.test"+path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if method != "GET" {
			r.Header.Set("Origin", "https://scanner.example.test")
			r.Header.Set("Idempotency-Key", "request_role_login_01")
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	anon := call("GET", "/api/session", "", nil).Result().Cookies()[0]
	cookie := call("POST", "/auth/guest", "", anon).Result().Cookies()[0]
	if w := call("POST", "/api/capabilities/paperless/health", "{}", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"healthy"`) {
		t.Fatal("API health composition missing", w.Code, w.Body.String())
	}
	if gets.Load() != 1 || posts.Load() != 0 {
		t.Fatal("API health gained upload authority", gets.Load(), posts.Load())
	}
	if err = Run(context.Background(), workerConfig, true); err != nil {
		t.Fatal(err)
	}
	known, err := store.ReadDelivery(context.Background(), owner, delivery.ID)
	if err != nil || known.State != "accepted" || posts.Load() != 1 || gets.Load() != 1 {
		t.Fatal("real worker did not upload fixed admitted destination", known.State, err, posts.Load(), gets.Load())
	}
	if err = Run(context.Background(), workerConfig, true); err != nil {
		t.Fatal(err)
	}
	known, err = store.ReadDelivery(context.Background(), owner, delivery.ID)
	if err != nil || known.State != "confirmed" || posts.Load() != 1 || gets.Load() != 3 {
		t.Fatal("real worker reconciliation failed", known.State, err, posts.Load(), gets.Load())
	}
	for range 2 {
		if err = Run(context.Background(), workerConfig, true); err != nil {
			t.Fatal(err)
		}
	}
	if gets.Load() != 3 || posts.Load() != 1 {
		t.Fatal("terminal delivery was repeated")
	}
	after, err := store.GetExport(context.Background(), owner, document)
	if err != nil || after.SHA256 != output.SHA256 {
		t.Fatal("delivery changed local export")
	}
	var active int
	if err = db.QueryRow(`SELECT count(*) FROM jobs WHERE state IN ('queued','running','cancelling','reconciling')`).Scan(&active); err != nil || active != 0 {
		t.Fatal("delivery occupied normal jobs", active, err)
	}
	t.Log("real role composition: API root GET1/POST0; worker POST1 + exact task/metadata GET2; restart repeats0")
}

func TestPaperlessSchedulerDoesNotBlockNormalJobs(t *testing.T) {
	c, store, db, _ := fixture(t)
	admit(t, store)
	if err := Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(context.Background(), owner, document)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/documents/post_document/" {
			t.Error("unexpected automatic request")
			w.WriteHeader(500)
			return
		}
		posts.Add(1)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	values := map[string]string{"SCANNER_PAPERLESS_ENABLED": "true", "SCANNER_PAPERLESS_DESTINATION": "Archive", "SCANNER_PAPERLESS_BASE_URL": server.URL, "SCANNER_PAPERLESS_ALLOW_INSECURE_HTTP": "true", "SCANNER_PAPERLESS_TOKEN_ENV": "SCANNER_PAPERLESS_TOKEN_SLOW", "SCANNER_PAPERLESS_TOKEN_SLOW": "synthetic-slow-token"}
	c.Paperless, err = config.LoadPaperless(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	d, err := store.AdmitDelivery(context.Background(), owner, documents.DeliveryRequest{DocumentID: document, Revision: current.Revision, IdempotencyKey: "request_slow_delivery_01", DestinationID: c.Paperless.DestinationID, ConfigFingerprint: c.Paperless.ConfigFingerprint})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, false) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("worker exited before delivery", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not schedule delivery")
	}
	job, err := store.AdmitRender(context.Background(), owner, documents.RenderAdmission{DocumentID: document, Revision: current.Revision, IdempotencyKey: "request_parallel_pdf_01", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour})
	if err != nil {
		t.Fatal("delivery blocked ordinary admission", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err = db.QueryRow(`SELECT state FROM jobs WHERE id=?`, job.JobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow Paperless blocked ordinary render", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker cancellation did not join delivery", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker left delivery goroutine running")
	}
	known, err := store.ReadDelivery(context.Background(), owner, d.ID)
	if err != nil || known.State != "attempted" || posts.Load() != 1 {
		t.Fatal("cancelled upload lost attempt safety", known.State, err, posts.Load())
	}
	t.Log("blocked upload POST1; ordinary PDF completed before upload response; cancellation joined delivery")
}

func TestPaperlessSchedulerDurablePollLimit(t *testing.T) {
	h := newDeliveryHarness(t)
	if err := ProcessDelivery(context.Background(), h.store, h.root, h.client, deliveryOwner, h.d.ID); err != nil {
		t.Fatal(err)
	}
	h.mode.Store(7)
	for range 23 {
		if err := runDeliveries(context.Background(), h.store, h.root, h.client, true); err != nil {
			t.Fatal(err)
		}
	}
	known, err := h.store.ReadDelivery(context.Background(), deliveryOwner, h.d.ID)
	if err != nil || known.State != "unknown" || h.posts.Load() != 1 || h.gets.Load() != 20 {
		t.Fatal("automatic poll bound lost", known.State, err, h.posts.Load(), h.gets.Load())
	}
	h.reopen(t)
	if err := runDeliveries(context.Background(), h.store, h.root, h.client, true); err != nil {
		t.Fatal(err)
	}
	if h.posts.Load() != 1 || h.gets.Load() != 20 {
		t.Fatal("restart reset automatic budget")
	}
}
