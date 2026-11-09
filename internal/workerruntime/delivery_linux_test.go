//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/paperless"
	"scanner-kiosk/internal/storage"
)

func TestDeliveryTracerAPI9Lifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	db, err := storage.Open(path, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.Open(path, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='deliveries'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("durable delivery ledger must exist independently of normal jobs before sending")
	}
	testDeliveryLifecycle(t, db, path)
}

type deliveryHarness struct {
	db, state   *sql.DB
	path        string
	store       *documents.Store
	root        *artifactread.Root
	client      *paperless.Client
	d           documents.Delivery
	request     documents.DeliveryRequest
	posts, gets atomic.Int64
	mode        atomic.Int64
}

const deliveryOwner = "principal_delivery_01"
const deliveryTask = "12345678-1234-4234-8234-123456789abc"

func newDeliveryHarness(t *testing.T) *deliveryHarness {
	t.Helper()
	h := &deliveryHarness{path: filepath.Join(t.TempDir(), "documents.sqlite3")}
	var err error
	h.db, err = storage.Open(h.path, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	h.state, err = storage.Open(filepath.Join(t.TempDir(), "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Close(); h.state.Close() })
	exec := func(db *sql.DB, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(h.state, `INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',1000)`, deliveryOwner)
	exec(h.db, `INSERT INTO owners(id) VALUES(?)`, deliveryOwner)
	exec(h.db, `INSERT INTO documents(id,owner_id,purpose,state,revision,created_at_ms,expires_at_ms) VALUES('document_delivery_01',?,'document','ready',1,1000,100000)`, deliveryOwner)
	pdf := []byte("%PDF-1.4\nsynthetic crash matrix\n%%EOF\n")
	hash := sha256.Sum256(pdf)
	exec(h.db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('artifact_delivery_01','document_delivery_01',?,'output','ready','output.pdf','application/pdf',?,?,1,1000)`, deliveryOwner, len(pdf), hash[:])
	exec(h.db, `INSERT INTO published_outputs VALUES('document_delivery_01',?,1,'artifact_delivery_01')`, deliveryOwner)
	rootPath := t.TempDir()
	if err = os.Chmod(rootPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(rootPath, "output.pdf"), pdf, 0600); err != nil {
		t.Fatal(err)
	}
	h.root, err = artifactread.OpenRoot(rootPath, 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.root.Close() })
	h.store, err = documents.New(h.state, h.db, func() time.Time { return time.UnixMilli(2000) }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	h.request = documents.DeliveryRequest{DocumentID: "document_delivery_01", Revision: 1, IdempotencyKey: "delivery_request_001", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)}
	h.d, err = h.store.AdmitDelivery(context.Background(), deliveryOwner, h.request)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/api/documents/post_document/" {
			h.posts.Add(1)
			var state string
			if err := h.db.QueryRow(`SELECT state FROM deliveries WHERE id=?`, h.d.ID).Scan(&state); err != nil || state != "attempted" {
				t.Error("POST without durable attempt", state, err)
			}
			if h.mode.Load() == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
				return
			}
			fmt.Fprintf(w, "%q", deliveryTask)
			return
		}
		h.gets.Add(1)
		if r.Method != "GET" {
			t.Error("unexpected remote mutation", r.Method)
			w.WriteHeader(500)
			return
		}
		if r.URL.Path == "/api/tasks/" {
			if r.URL.RawQuery != "task_id="+deliveryTask {
				t.Error("non-exact UUID query", r.URL.RawQuery)
			}
			filename := h.d.Filename
			if h.mode.Load() == 2 {
				filename = "delivery_" + strings.Repeat("b", 48) + ".pdf"
			}
			if h.mode.Load() == 3 {
				fmt.Fprint(w, `[]`)
				return
			}
			if h.mode.Load() == 4 {
				w.WriteHeader(403)
				return
			}
			if h.mode.Load() == 5 {
				fmt.Fprint(w, `{"untrusted":"private remote content"}`)
				return
			}
			status := "SUCCESS"
			if h.mode.Load() == 7 {
				status = "PENDING"
			}
			if h.mode.Load() == 8 {
				status = "FAILURE"
			}
			fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":%q,"related_document":42}]`, deliveryTask, filename, status)
			return
		}
		if r.URL.Path == "/api/documents/42/metadata/" {
			checksum := hex.EncodeToString(hash[:])
			if h.mode.Load() == 6 {
				checksum = strings.Repeat("0", 64)
			}
			fmt.Fprintf(w, `{"original_checksum":%q}`, checksum)
			return
		}
		t.Error("unexpected remote path", r.URL.Path)
		w.WriteHeader(404)
	}))
	t.Cleanup(server.Close)
	h.client, err = paperless.New(paperless.Options{BaseURL: server.URL, Token: "synthetic-token", DestinationID: h.d.DestinationID, ConfigFingerprint: h.d.ConfigFingerprint, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.client.Close)
	return h
}

func (h *deliveryHarness) reopen(t *testing.T) {
	t.Helper()
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	h.db, err = storage.Open(h.path, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	h.store, err = documents.New(h.state, h.db, func() time.Time { return time.UnixMilli(2000) }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
}

func (h *deliveryHarness) step() error {
	return ProcessDelivery(context.Background(), h.store, h.root, h.client, deliveryOwner, h.d.ID)
}

func TestDeliveryOneAttemptCrashMatrix(t *testing.T) {
	ctx := context.Background()
	for _, crash := range []string{"after-marker-before-socket", "response-loss", "uuid-commit-loss", "poll-restart", "final-commit-loss"} {
		t.Run(crash, func(t *testing.T) {
			h := newDeliveryHarness(t)
			wantPosts := int64(1)
			switch crash {
			case "after-marker-before-socket":
				if _, err := h.store.ClaimDeliveryAttempt(ctx, deliveryOwner, h.d.ID, "delivery_runner_crash"); err != nil {
					t.Fatal(err)
				}
				wantPosts = 0
			case "response-loss":
				h.mode.Store(1)
				if err := h.step(); err == nil {
					t.Fatal("lost response reported success")
				}
				h.mode.Store(0)
			case "uuid-commit-loss":
				if _, err := h.store.ClaimDeliveryAttempt(ctx, deliveryOwner, h.d.ID, "delivery_runner_crash"); err != nil {
					t.Fatal(err)
				}
				file, err := h.root.Open(ctx, artifactread.Input{RelativePath: h.d.Artifact.RelativePath, Size: h.d.Artifact.Size, SHA256: h.d.Artifact.SHA256})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = h.client.Upload(ctx, h.d.Filename, h.d.Artifact.Size, file); err != nil {
					t.Fatal(err)
				}
				file.Close()
			case "poll-restart":
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
				h.mode.Store(7)
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
				h.mode.Store(0)
			case "final-commit-loss":
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
			}
			h.reopen(t)
			if err := h.step(); err != nil {
				t.Fatal(err)
			}
			known, err := h.store.ReadDelivery(ctx, deliveryOwner, h.d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if known.TaskID == "" {
				if known.State != "unknown" {
					t.Fatal("restart did not conservatively mark unknown", known.State)
				}
				if wantPosts == 1 {
					if err = RecoverDeliveryTask(ctx, h.store, h.client, deliveryOwner, h.d.ID, deliveryTask); err != nil {
						t.Fatal(err)
					}
					if err = h.step(); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := h.request
			r.IdempotencyKey = "delivery_newkey_0001"
			if again, err := h.store.AdmitDelivery(ctx, deliveryOwner, r); err != nil || again.ID != h.d.ID {
				t.Fatal("new key created resend", err)
			}
			for i := 0; i < 2; i++ {
				if err = h.step(); err != nil {
					t.Fatal(err)
				}
			}
			if h.posts.Load() != wantPosts {
				t.Fatal("crash/reopen changed attempt count", h.posts.Load(), wantPosts)
			}
			t.Logf("file-backed reopen: POST=%d GET=%d", h.posts.Load(), h.gets.Load())
		})
	}
}

func TestDeliveryConcurrentClaimAtMostOneAttempt(t *testing.T) {
	h := newDeliveryHarness(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := h.step(); err != nil && !errors.Is(err, documents.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	h.reopen(t)
	if err := h.step(); err != nil {
		t.Fatal(err)
	}
	if h.posts.Load() != 1 {
		t.Fatal("two workers uploaded more than once", h.posts.Load())
	}
}

func TestDeliveryDestinationDrift(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			h := newDeliveryHarness(t)
			if attempted {
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
			}
			client, err := paperless.New(paperless.Options{BaseURL: "http://127.0.0.1:1", Token: "synthetic-token", DestinationID: h.d.DestinationID, ConfigFingerprint: strings.Repeat("b", 64), AllowHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if err = ProcessDelivery(context.Background(), h.store, h.root, client, deliveryOwner, h.d.ID); !errors.Is(err, documents.ErrConflict) {
				t.Fatal("destination drift authorized network", err)
			}
			if attempted {
				h.reopen(t)
				if err = h.step(); err != nil {
					t.Fatal(err)
				}
			}
			want := int64(0)
			if attempted {
				want = 1
			}
			if h.posts.Load() != want {
				t.Fatal("destination drift sent again", h.posts.Load())
			}
		})
	}
}

func TestDeliveryRecoveryAuthorizationAndPollBound(t *testing.T) {
	for _, mode := range []int64{2, 3, 4, 5, 6, 7, 8} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			h := newDeliveryHarness(t)
			ctx := context.Background()
			if err := h.step(); err != nil {
				t.Fatal(err)
			}
			h.mode.Store(mode)
			if mode <= 5 {
				if err := RecoverDeliveryTask(ctx, h.store, h.client, deliveryOwner, h.d.ID, deliveryTask); err == nil {
					t.Fatal("invalid remote task bound")
				}
			} else {
				for i := 0; i < 23; i++ {
					_ = h.step()
				}
			}
			known, err := h.store.ReadDelivery(ctx, deliveryOwner, h.d.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "unknown"
			if mode == 8 {
				want = "failed"
			}
			if known.State != want {
				t.Fatal("unsafe remote outcome", known.State, want)
			}
			if h.posts.Load() != 1 || h.gets.Load() > 40 {
				t.Fatal("unbounded network", h.posts.Load(), h.gets.Load())
			}
			before := h.gets.Load()
			if err = RecoverDeliveryTask(ctx, h.store, h.client, "other_owner_000001", h.d.ID, deliveryTask); err == nil {
				t.Fatal("foreign owner allowed recovery")
			}
			if err = RecoverDeliveryTask(ctx, h.store, h.client, deliveryOwner, h.d.ID, "invalid"); !errors.Is(err, documents.ErrInvalid) {
				t.Fatal("malformed UUID allowed", err)
			}
			if h.gets.Load() != before {
				t.Fatal("unauthorized recovery made remote request")
			}
		})
	}
}

func TestDeliveryCancelClaimRace(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			h := newDeliveryHarness(t)
			ctx := context.Background()
			if attempted {
				if err := h.step(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := h.store.CancelDelivery(ctx, deliveryOwner, h.d.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "cancelled"
			if attempted {
				wantState = "accepted"
			}
			if got.State != wantState {
				t.Fatal("local cancellation claimed remote rollback", got.State)
			}
			h.reopen(t)
			if err = h.step(); err != nil {
				t.Fatal(err)
			}
			wantPosts := int64(0)
			if attempted {
				wantPosts = 1
			}
			if h.posts.Load() != wantPosts {
				t.Fatal("cancel changed remote attempt count", h.posts.Load())
			}
		})
	}
}

func TestDeliveryBytesExpireThroughRealCleanup(t *testing.T) {
	_, store, db, root := fixtureWithLifetime(t, 2*time.Second)
	ctx := context.Background()
	pdf := []byte("%PDF-1.4\nsynthetic expiry\n%%EOF")
	hash := sha256.Sum256(pdf)
	if err := os.WriteFile(filepath.Join(root, "delivery.pdf"), pdf, 0600); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `UPDATE documents SET state='ready' WHERE id=?`, document)
	execute(t, db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('delivery_expiry_output',?,?,'output','ready','delivery.pdf','application/pdf',?,?,1,?)`, document, owner, len(pdf), hash[:], time.Now().UnixMilli())
	execute(t, db, `INSERT INTO published_outputs VALUES(?,?,1,'delivery_expiry_output')`, document, owner)
	d, err := store.AdmitDelivery(ctx, owner, documents.DeliveryRequest{DocumentID: document, Revision: 1, IdempotencyKey: "delivery_expiry_request", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ClaimDeliveryAttempt(ctx, owner, d.ID, "delivery_expiry_runner"); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkDeliveryUnknown(ctx, owner, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RequestDelete(ctx, owner, documents.DeleteRequest{DocumentID: document, Revision: 1, IdempotencyKey: "delivery_expiry_delete"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	check := func(context.Context) error { return nil }
	if handled, dispatchErr := dispatchCleanup(ctx, store, root, "delivery_cleanup_runner", 128<<20, time.Minute, check); dispatchErr != nil || handled {
		t.Fatal("cleanup must skip a live delivery without stalling worker", handled, dispatchErr)
	}
	if _, err = os.Stat(filepath.Join(root, "delivery.pdf")); err != nil {
		t.Fatal("active bytes removed", err)
	}
	if wait := time.Until(time.UnixMilli(d.ExpiresAtMS)) + 10*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if handled, err := dispatchCleanup(ctx, store, root, "delivery_cleanup_runner", 128<<20, time.Minute, check); err != nil || !handled {
		t.Fatal("expired bytes not reclaimed", handled, err)
	}
	for _, file := range []string{"delivery.pdf", "original.png"} {
		if _, err = os.Stat(filepath.Join(root, file)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("expired private bytes retained", file, err)
		}
	}
	var state string
	var expiry int64
	if err = db.QueryRow(`SELECT state,expires_at_ms FROM deliveries WHERE id=?`, d.ID).Scan(&state, &expiry); err != nil || state != "retired" || expiry != d.ExpiresAtMS {
		t.Fatal("TTL/fence changed", state, expiry, err)
	}
}

func testDeliveryLifecycle(t *testing.T, db *sql.DB, path string) {
	ctx := context.Background()
	owner, documentID := "principal_delivery_01", "document_delivery_01"
	state, err := storage.Open(filepath.Join(t.TempDir(), "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	exec := func(db *sql.DB, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(state, `INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',1000)`, owner)
	exec(db, `INSERT INTO owners(id) VALUES(?)`, owner)
	exec(db, `INSERT INTO documents(id,owner_id,purpose,state,revision,created_at_ms,expires_at_ms) VALUES(?,?,'document','ready',1,1000,100000)`, documentID, owner)
	// Public synthetic test bytes only; immutable source and output have separate identities.
	pdf := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF\n")
	hash := sha256.Sum256(pdf)
	for _, a := range []struct{ id, kind, path string }{{"original_delivery_01", "original", "original.pdf"}, {"artifact_delivery_01", "output", "output.pdf"}} {
		exec(db, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,?,'ready',?,'application/pdf',?,?,1,1000)`, a.id, documentID, owner, a.kind, a.path, len(pdf), hash[:])
	}
	exec(db, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,'artifact_delivery_01')`, documentID, owner)
	rootPath := t.TempDir()
	if err = os.Chmod(rootPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(rootPath, "output.pdf"), pdf, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := artifactread.OpenRoot(rootPath, 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	store, err := documents.New(state, db, func() time.Time { return time.UnixMilli(2000) }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	d, err := store.AdmitDelivery(ctx, owner, documents.DeliveryRequest{DocumentID: documentID, Revision: 1, IdempotencyKey: "delivery_request_001", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != "queued" || d.Artifact.SHA256 != hash || d.Revision != 1 || d.ExpiresAtMS != 100000 {
		t.Fatal("snapshot not frozen", d)
	}
	taskID := "12345678-1234-4234-8234-123456789abc"
	var posts, gets atomic.Int64
	var phase atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != paperless.APIVersion || r.Header.Get("Authorization") != "Token synthetic-token" {
			t.Error("API9 authentication contract missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/documents/post_document/":
			posts.Add(1)
			reopened, e := storage.Open(path, storage.Documents)
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			defer reopened.Close()
			var durableState, filename string
			var attempt int64
			if e = reopened.QueryRow(`SELECT state,attempted_at_ms,filename FROM deliveries WHERE id=?`, d.ID).Scan(&durableState, &attempt, &filename); e != nil || durableState != "attempted" || attempt != 2000 || filename != d.Filename {
				t.Error("HTTP preceded durable attempted marker", durableState, attempt, e)
			}
			reader, e := r.MultipartReader()
			if e != nil {
				t.Error(e)
				return
			}
			part, e := reader.NextPart()
			if e != nil {
				t.Error(e)
				return
			}
			body, e := io.ReadAll(part)
			if e != nil || part.FormName() != "document" || part.FileName() != d.Filename || part.Header.Get("Content-Type") != "application/pdf" || sha256.Sum256(body) != hash {
				t.Error("multipart changed frozen PDF", e)
			}
			if _, e = reader.NextPart(); e != io.EOF {
				t.Error("unexpected multipart field", e)
			}
			fmt.Fprintf(w, "%q", taskID)
		case r.Method == "GET" && r.URL.Path == "/api/tasks/":
			gets.Add(1)
			if r.URL.RawQuery != "task_id="+taskID {
				t.Error("non-exact task query")
			}
			status := []string{"PENDING", "STARTED", "SUCCESS"}[phase.Load()]
			fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":%q,"related_document":42}]`, taskID, d.Filename, status)
		case r.Method == "GET" && r.URL.Path == "/api/documents/42/metadata/":
			gets.Add(1)
			fmt.Fprintf(w, `{"original_checksum":%q}`, hex.EncodeToString(hash[:]))
		default:
			t.Error("unexpected remote request", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := paperless.New(paperless.Options{BaseURL: server.URL, Token: "synthetic-token", DestinationID: d.DestinationID, ConfigFingerprint: d.ConfigFingerprint, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	step := func() {
		t.Helper()
		if err = ProcessDelivery(ctx, store, root, client, owner, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	step()
	current, err := store.ReadDelivery(ctx, owner, d.ID)
	if err != nil || current.State != "accepted" || current.TaskID != taskID {
		t.Fatal("acknowledgement confused with completion", current, err)
	}
	for i := int64(0); i < 3; i++ {
		phase.Store(i)
		step()
		current, err = store.ReadDelivery(ctx, owner, d.ID)
		expected := "accepted"
		if i == 2 {
			expected = "confirmed"
		}
		if err != nil || current.State != expected {
			t.Fatal("wrong asynchronous state", current.State, expected, err)
		}
	}
	step()
	if posts.Load() != 1 || gets.Load() != 4 {
		t.Fatal("unexpected request counts", posts.Load(), gets.Load())
	}
	ref, err := store.ReadCurrentOutput(ctx, owner, documentID)
	if err != nil || ref.Artifact.ID != "artifact_delivery_01" || ref.Artifact.SHA256 != hash {
		t.Fatal("delivery changed saved output", err)
	}
	var revision, artifacts, jobs, receipts int
	for query, target := range map[string]*int{`SELECT revision FROM documents`: &revision, `SELECT count(*) FROM artifacts`: &artifacts, `SELECT count(*) FROM jobs`: &jobs, `SELECT count(*) FROM request_receipts`: &receipts} {
		if err = db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if revision != 1 || artifacts != 2 || jobs != 0 || receipts != 0 {
		t.Fatal("delivery mutated ordinary document/jobs", revision, artifacts, jobs, receipts)
	}
	t.Log("real SQLite/API9 lifecycle: POST=1 GET=4, immutable source/output and ordinary job namespace preserved")
}
