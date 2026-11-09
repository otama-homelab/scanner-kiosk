//go:build linux

package documents_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/paperless"
	"scanner-kiosk/internal/workerruntime"
)

func TestDeliveryWorkerCommitUncertainty(t *testing.T) {
	ctx := context.Background()
	const taskID = "12345678-1234-4234-8234-123456789abc"
	for _, stage := range []string{"queued", "attempted", "accepted", "confirmed"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/committed=%t", stage, committed), func(t *testing.T) {
				f, store, db, _ := newSQLiteContractFixture(t)
				pdf := []byte("%PDF-1.4\npublic commit fault test\n%%EOF")
				hash := sha256.Sum256(pdf)
				for q, args := range map[string][]any{
					`UPDATE documents SET state='ready' WHERE id=?`: {f.Document},
					`INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES('delivery_commit_output',?,?,'output','ready','output.pdf','application/pdf',?,?,1,1000)`: {f.Document, f.Owner, len(pdf), hash[:]},
				} {
					if _, err := db.Exec(q, args...); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := db.Exec(`INSERT INTO published_outputs VALUES(?,?,1,'delivery_commit_output')`, f.Document, f.Owner); err != nil {
					t.Fatal(err)
				}
				rootPath := t.TempDir()
				if err := os.Chmod(rootPath, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(rootPath, "output.pdf"), pdf, 0600); err != nil {
					t.Fatal(err)
				}
				root, err := artifactread.OpenRoot(rootPath, 128<<20)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				r := documents.DeliveryRequest{DocumentID: f.Document, Revision: 1, IdempotencyKey: "delivery_fault_request", DestinationID: "paperless-local", ConfigFingerprint: strings.Repeat("a", 64)}
				var d documents.Delivery
				if stage != "queued" {
					d, err = store.AdmitDelivery(ctx, f.Owner, r)
					if err != nil {
						t.Fatal(err)
					}
				}
				var posts, gets atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == "POST" {
						posts.Add(1)
						fmt.Fprintf(w, "%q", taskID)
						return
					}
					gets.Add(1)
					if r.URL.Path == "/api/tasks/" {
						fmt.Fprintf(w, `[{"task_id":%q,"task_file_name":%q,"status":"SUCCESS","related_document":42}]`, taskID, d.Filename)
						return
					}
					fmt.Fprintf(w, `{"original_checksum":%q}`, hex.EncodeToString(hash[:]))
				}))
				defer server.Close()
				client, err := paperless.New(paperless.Options{BaseURL: server.URL, Token: "synthetic-token", DestinationID: r.DestinationID, ConfigFingerprint: r.ConfigFingerprint, AllowHTTP: true})
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				if stage == "confirmed" {
					if err = workerruntime.ProcessDelivery(ctx, store, root, client, f.Owner, d.ID); err != nil {
						t.Fatal(err)
					}
				}
				at := 3
				if stage == "queued" {
					at = 1
				}
				if stage == "attempted" {
					at = 2
				}
				restore := documents.InjectContractCommitFailure(store, committed, at)
				if stage == "queued" {
					_, err = store.AdmitDelivery(ctx, f.Owner, r)
				} else {
					err = workerruntime.ProcessDelivery(ctx, store, root, client, f.Owner, d.ID)
				}
				restore()
				if !errors.Is(err, documents.ErrCommitUncertain) {
					t.Fatal("commit uncertainty guessed", err)
				}
				if stage == "queued" || stage == "attempted" {
					if posts.Load() != 0 {
						t.Fatal("POST before known durable attempt", posts.Load())
					}
				}
				store = f.ReopenRecords().(*documents.Store)
				if stage == "queued" {
					known, err := store.FindDelivery(ctx, f.Owner, r.IdempotencyKey)
					if committed {
						if err != nil || known.State != "queued" {
							t.Fatal(known, err)
						}
					} else if !errors.Is(err, documents.ErrUnavailable) {
						t.Fatal("rolled-back admission found", err)
					}
					return
				}
				if stage == "attempted" && !committed {
					known, err := store.ReadDelivery(ctx, f.Owner, d.ID)
					if err != nil || known.State != "queued" {
						t.Fatal(known, err)
					}
					return
				}
				if err = workerruntime.ProcessDelivery(ctx, store, root, client, f.Owner, d.ID); err != nil {
					t.Fatal(err)
				}
				known, err := store.ReadDelivery(ctx, f.Owner, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if known.TaskID == "" && posts.Load() == 1 {
					if err = workerruntime.RecoverDeliveryTask(ctx, store, client, f.Owner, d.ID, taskID); err != nil {
						t.Fatal(err)
					}
					if err = workerruntime.ProcessDelivery(ctx, store, root, client, f.Owner, d.ID); err != nil {
						t.Fatal(err)
					}
				}
				want := int64(1)
				if stage == "attempted" {
					want = 0
				}
				if posts.Load() != want {
					t.Fatal("restart resent", posts.Load(), want)
				}
				t.Logf("actual worker fault + reopen: POST=%d GET=%d", posts.Load(), gets.Load())
			})
		}
	}
}
