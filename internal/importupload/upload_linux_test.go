package importupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/storage"
)

const owner = "owner_import_file_001"

func fixture(t *testing.T) (*Service, *documents.Store, *sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	docsRoot := filepath.Join(root, "files")
	if err := os.Mkdir(docsRoot, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := storage.Open(filepath.Join(root, "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	docs, err := storage.Open(filepath.Join(root, "documents.db"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docs.Close() })
	if _, err = state.Exec("INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',1)", owner); err != nil {
		t.Fatal(err)
	}
	records, err := documents.New(state, docs, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(docsRoot, records, documents.ImportLimits{AdmissionLimits: documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}, MaxDocuments: 20, DocumentLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	return service, records, docs, docsRoot
}
func request(raw []byte) documents.ImportRequest {
	return documents.ImportRequest{IdempotencyKey: "request_file_import1", InputBytes: int64(len(raw)), SHA256: sha256.Sum256(raw)}
}
func TestReceiveSyncsSealsAndReplaysWithoutReadingBody(t *testing.T) {
	service, _, db, root := fixture(t)
	raw := bytes.Repeat([]byte("PDF bytes"), 12000)
	a, err := service.Receive(context.Background(), owner, request(raw), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var path, state string
	var size int64
	if err = db.QueryRow("SELECT a.relative_path,i.state,a.byte_size FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id WHERE i.job_id=?", a.JobID).Scan(&path, &state, &size); err != nil || state != "ready" || size != int64(len(raw)) {
		t.Fatal("intake not sealed", state, size, err)
	}
	actual, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("saved bytes", err)
	}
	info, err := os.Stat(filepath.Join(root, path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("not private", err)
	}
	replay, err := service.Receive(context.Background(), owner, request(raw), forbiddenReader{t})
	if err != nil || !replay.Replay || replay.JobID != a.JobID {
		t.Fatal("replay", err)
	}
}

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("replay read upload body")
	return 0, io.EOF
}
func TestReceiveRejectsWrongDigestShortOrLongBody(t *testing.T) {
	for _, mode := range []string{"digest", "short", "long", "read-error"} {
		t.Run(mode, func(t *testing.T) {
			service, _, db, root := fixture(t)
			raw := []byte("expected PDF bytes")
			intent := request(raw)
			var body io.Reader = bytes.NewReader(raw)
			want := raw
			switch mode {
			case "digest":
				intent.SHA256 = sha256.Sum256([]byte("wrong"))
			case "short":
				want = raw[:4]
				body = bytes.NewReader(want)
			case "long":
				body = bytes.NewReader(append(append([]byte{}, raw...), 1))
			case "read-error":
				want = raw[:4]
				body = io.MultiReader(bytes.NewReader(want), errorReader{})
			}
			if _, err := service.Receive(context.Background(), owner, intent, body); err == nil {
				t.Fatal("invalid body accepted")
			}
			var reserved, count int
			var path, state string
			if err := db.QueryRow("SELECT count(*) FROM reservations").Scan(&reserved); err != nil || reserved != 0 {
				t.Fatal("stopped producer reservation retained", reserved, err)
			}
			if err := db.QueryRow("SELECT a.relative_path,j.state FROM artifacts a JOIN import_intakes i ON i.artifact_id=a.id JOIN jobs j ON j.id=i.job_id WHERE i.state='aborted'").Scan(&path, &state); err != nil || state != "failed" {
				t.Fatal("failed intake state", state, err)
			}
			actual, err := os.ReadFile(filepath.Join(root, path))
			if err != nil || !bytes.Equal(actual, want) {
				t.Fatal("partial bytes not retained exactly", err)
			}
			if err = db.QueryRow("SELECT count(*) FROM pages").Scan(&count); err != nil || count != 0 {
				t.Fatal("unparsed bytes became a page")
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("broken source") }

type lostSeal struct{ *documents.Store }

func (r lostSeal) SealStoppedImportUpload(ctx context.Context, owner, job, producer string, a documents.ArtifactRecord, closed int64) error {
	if err := r.Store.SealStoppedImportUpload(ctx, owner, job, producer, a, closed); err != nil {
		return err
	}
	return documents.ErrCommitUncertain
}
func TestReceiveReconcilesLostSealAcknowledgement(t *testing.T) {
	service, records, _, _ := fixture(t)
	service.records = lostSeal{records}
	raw := []byte("PDF source")
	if _, err := service.Receive(context.Background(), owner, request(raw), bytes.NewReader(raw)); err != nil {
		t.Fatal("committed source not reconciled", err)
	}
}

type existingSource struct {
	*documents.Store
	root string
	path string
}

func (r *existingSource) AdmitImport(ctx context.Context, owner string, request documents.ImportRequest, limits documents.ImportLimits) (documents.ImportAdmission, error) {
	a, err := r.Store.AdmitImport(ctx, owner, request, limits)
	if err != nil {
		return a, err
	}
	r.path = filepath.Join(r.root, a.Input.RelativePath)
	if err = os.Mkdir(filepath.Dir(r.path), 0700); err != nil {
		return documents.ImportAdmission{}, err
	}
	if err = os.WriteFile(r.path, []byte("preserve existing"), 0600); err != nil {
		return documents.ImportAdmission{}, err
	}
	return a, nil
}
func TestReceiveNeverAdoptsOrOverwritesExistingInput(t *testing.T) {
	service, records, db, root := fixture(t)
	injected := &existingSource{Store: records, root: root}
	service.records = injected
	if _, err := service.Receive(context.Background(), owner, request([]byte("PDF input")), bytes.NewReader([]byte("PDF input"))); !errors.Is(err, ErrReconciliation) {
		t.Fatal("existing path adopted", err)
	}
	raw, err := os.ReadFile(injected.path)
	if err != nil || string(raw) != "preserve existing" {
		t.Fatal("existing bytes modified", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM reservations").Scan(&count); err != nil || count != 1 {
		t.Fatal("uncertain file lost reservation", count, err)
	}
}

type cancelReader struct{ cancel context.CancelFunc }

func (r cancelReader) Read(p []byte) (int, error) { r.cancel(); return copy(p, []byte("stop")), nil }
func TestReceiveClosesProducerBeforeFinalizingCancellation(t *testing.T) {
	service, _, db, root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := service.Receive(ctx, owner, request([]byte("expected bytes")), cancelReader{cancel}); err == nil {
		t.Fatal("cancelled upload succeeded")
	}
	var path, state string
	var size int64
	if err := db.QueryRow("SELECT a.relative_path,i.state,a.byte_size FROM import_intakes i JOIN artifacts a ON a.id=i.artifact_id").Scan(&path, &state, &size); err != nil || state != "aborted" || size != 0 {
		t.Fatal("cancelled producer not finalized", state, size, err)
	}
	info, err := os.Stat(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	descriptors, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		current, err := os.Stat(filepath.Join("/proc/self/fd", descriptor.Name()))
		if err == nil && os.SameFile(info, current) {
			t.Fatal("cancelled producer kept its input descriptor open")
		}
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM reservations").Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled reservation", count, err)
	}
}
