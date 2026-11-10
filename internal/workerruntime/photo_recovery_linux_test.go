//go:build linux

package workerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
)

func TestPhotoImportRecoveryMatrix(t *testing.T) {
	for _, mode := range []string{"pending", "receiving-partial", "worker-stopped"} {
		t.Run(mode, func(t *testing.T) {
			c, store, db, root := fixture(t)
			raw := photoJPEG(t)
			request := photoRequest(t, raw)
			a, err := store.AdmitImport(context.Background(), owner, request, photoLimits())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "pending" {
				if err = store.StartImportUpload(context.Background(), owner, a.JobID, a.ProducerID); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(filepath.Join(root, filepath.Dir(a.Input.RelativePath)), 0700); err != nil {
					t.Fatal(err)
				}
				data := raw
				if mode == "receiving-partial" {
					data = raw[:31]
				}
				if err = os.WriteFile(filepath.Join(root, a.Input.RelativePath), data, 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "worker-stopped" {
					artifact := a.Input
					artifact.Size = int64(len(raw))
					artifact.SHA256 = sha256.Sum256(raw)
					if err = store.SealStoppedImportUpload(context.Background(), owner, a.JobID, a.ProducerID, artifact, time.Now().UnixMilli()); err != nil {
						t.Fatal(err)
					}
					if _, err = store.ClaimNextImport(context.Background(), "worker_photo_oldboot01"); err != nil {
						t.Fatal(err)
					}
					if err = os.Mkdir(filepath.Join(root, "work_unpublished_photo1"), 0700); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(filepath.Join(root, "work_unpublished_photo1", "pixels"), []byte("unpublished child frame"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err = Recover(context.Background(), c, true); err != nil {
				t.Fatal("stopped photo recovery", err)
			}
			if err = Recover(context.Background(), c, true); err != nil {
				t.Fatal("repeated photo recovery", err)
			}
			job, err := store.ReadJob(context.Background(), owner, a.JobID)
			if err != nil || job.State != "interrupted" {
				t.Fatal("photo not stopped", job, err)
			}
			for query, want := range map[string]int{"SELECT count(*) FROM reservations WHERE job_id=?": 0, "SELECT count(*) FROM photo_sources WHERE document_id=?": 0, "SELECT count(*) FROM pages WHERE document_id=?": 0} {
				target := a.DocumentID
				if query == "SELECT count(*) FROM reservations WHERE job_id=?" {
					target = a.JobID
				}
				var count int
				if err = db.QueryRow(query, target).Scan(&count); err != nil || count != want {
					t.Fatal("recovery leaked photo", query, count, err)
				}
			}
			if mode != "pending" {
				data, err := os.ReadFile(filepath.Join(root, a.Input.RelativePath))
				want := raw
				if mode == "receiving-partial" {
					want = raw[:31]
				}
				if err != nil || sha256.Sum256(data) != sha256.Sum256(want) {
					t.Fatal("recovery changed retained raw", err)
				}
			}
			receipt, err := store.ReadImportReceipt(context.Background(), owner, request)
			if err != nil || receipt.JobID != a.JobID {
				t.Fatal("recovery duplicated receipt", err)
			}
		})
	}
}

func TestPhotoRoutesOwnerRevisionTTL(t *testing.T) {
	c, _, db, root := fixture(t)
	state, err := storage.Open(filepath.Join(c.StateDir, "state.sqlite3"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	var offset atomic.Int64
	store, err := documents.New(state, db, func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	server, cookie, principal := photoHTTP(t, c, store, root)
	raw := photoJPEG(t)
	digest := sha256.Sum256(raw)
	key := "request_photo_route001"
	body, _ := json.Marshal(map[string]any{"media_type": "image/jpeg", "input_bytes": len(raw), "sha256": fmt.Sprintf("%x", digest)})
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
	code, _ := photoCall(t, server, cookie, "POST", "/api/documents/import/reconcile", body, headers)
	if code != 404 {
		t.Fatal("unknown lookup admitted work", code)
	}
	code, data := photoCall(t, server, cookie, "POST", "/api/documents/import", raw, map[string]string{"Content-Type": "image/jpeg", "Idempotency-Key": key, "X-Content-SHA256": fmt.Sprintf("%x", digest)})
	if code != 202 {
		t.Fatalf("HTTP admission %d %s", code, data)
	}
	var job documents.JobProjection
	if err = json.Unmarshal(data, &job); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		code, data = photoCall(t, server, cookie, "POST", "/api/documents/import/reconcile", body, headers)
		if code != 200 {
			t.Fatalf("lost acknowledgement readback %d %s", code, data)
		}
	}
	for _, mode := range []string{"media", "hash", "size", "unknown-field", "duplicate-field", "stale-revision", "anonymous"} {
		t.Run(mode, func(t *testing.T) {
			changed := map[string]any{"media_type": "image/jpeg", "input_bytes": len(raw), "sha256": fmt.Sprintf("%x", digest)}
			h := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
			want := 409
			who := cookie
			switch mode {
			case "media":
				changed["media_type"] = "image/png"
			case "hash":
				changed["sha256"] = fmt.Sprintf("%064x", 1)
			case "size":
				changed["input_bytes"] = len(raw) + 1
			case "unknown-field":
				changed["path"] = "private/path"
				want = 400
			case "duplicate-field":
				want = 400
			case "stale-revision":
				h["If-Match"] = "\"0\""
				want = 400
			case "anonymous":
				who = nil
				want = 401
			}
			request, _ := json.Marshal(changed)
			if mode == "duplicate-field" {
				request = append([]byte(`{"media_type":"image/png",`), request[1:]...)
			}
			code, data := photoCall(t, server, who, "POST", "/api/documents/import/reconcile", request, h)
			if code != want {
				t.Fatalf("route boundary %s %d %s", mode, code, data)
			}
		})
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM import_intakes").Scan(&count); err != nil || count != 1 {
		t.Fatal("lookup created a producer", count, err)
	}
	if err = Run(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	p, err := store.ReadDocument(context.Background(), principal, job.DocumentID)
	if err != nil || len(p.Pages) != 1 {
		t.Fatal(err)
	}
	offset.Store(int64(2 * time.Hour))
	code, _ = photoCall(t, server, cookie, "POST", "/api/documents/import/reconcile", body, headers)
	if code != 404 {
		t.Fatal("expired document receipt exposed", code)
	}
	code, _ = photoCall(t, server, cookie, "GET", "/api/documents/"+p.ID+"/pages/"+p.Pages[0].ID+"/preview", nil, nil)
	if code != 404 {
		t.Fatal("expired pixels exposed", code)
	}
	offset.Store(0)
	execute(t, state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), principal)
	code, _ = photoCall(t, server, cookie, "POST", "/api/documents/import/reconcile", body, headers)
	if code != 401 {
		t.Fatal("disabled session exposed receipt", code)
	}
}
