//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
