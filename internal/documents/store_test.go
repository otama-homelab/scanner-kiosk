package documents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
)

const alice = "principal_alice_001"
const bob = "principal_bob_00001"
const docA = "document_alice_001"
const docB = "document_bob_00001"
const jobA = "render_job_alice_01"
const runner = "worker_boot_000001"

func must(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	store               *Store
	state, docs         *sql.DB
	statePath, docsPath string
	now                 int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{statePath: filepath.Join(root, "state.sqlite3"), docsPath: filepath.Join(root, "documents.sqlite3"), now: 2000}
	seedFixtureSchemas(t, f.statePath, f.docsPath)
	var err error
	f.state, err = storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.state.Close() })
	f.docs, err = storage.Open(f.docsPath, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.docs.Close() })
	f.store, err = New(f.state, f.docs, func() time.Time { return time.UnixMilli(f.now) }, fixtureSpace)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{alice, bob} {
		must(t, f.state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',1000)", owner)
		must(t, f.docs, "INSERT INTO owners(id) VALUES(?)", owner)
	}
	must(t, f.docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','processing',1000,10000)", docA, alice)
	must(t, f.docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','processing',1000,10000)", docB, bob)
	must(t, f.docs, "INSERT INTO jobs(id,document_id,owner_id,kind,state,input_revision,configuration_json,created_at_ms,deadline_at_ms) VALUES(?,?,?,'render','queued',1,'{}',1500,9000)", jobA, docA, alice)
	must(t, f.docs, "INSERT INTO reservations(job_id,owner_id,byte_size) VALUES(?,?,100)", jobA, alice)
	return f
}
func output() Export {
	return Export{ID: "artifact_export_0001", RelativePath: "work_000001/output_0001", MediaType: "application/pdf", Size: 3, SHA256: sha256.Sum256([]byte("pdf"))}
}
func claim(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.store.ClaimExport(context.Background(), alice, jobA, runner); err != nil {
		t.Fatal(err)
	}
}
func scalar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestOwnerAndExactExpiryFiltering(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.store.List(ctx, alice, 10)
	if err != nil || len(list) != 1 || list[0].ID != docA {
		t.Fatal("cross-owner list")
	}
	if _, err = f.store.Get(ctx, bob, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner read")
	}
	f.now = 10000
	if _, err = f.store.Get(ctx, alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expiry boundary accepted")
	}
	list, err = f.store.List(ctx, alice, 10)
	if err != nil || len(list) != 0 {
		t.Fatal("expired document listed")
	}
}
func TestDisabledPrincipalDenied(t *testing.T) {
	f := setup(t)
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	if _, err := f.store.Get(context.Background(), alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled principal read")
	}
	if _, err := f.store.ClaimExport(context.Background(), alice, jobA, runner); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled principal claimed work")
	}
}
func TestClaimPublishAndDurableReadback(t *testing.T) {
	f := setup(t)
	claim(t, f)
	if err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); err != nil {
		t.Fatal(err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM published_outputs WHERE document_id=? AND artifact_id=?", docA, output().ID) != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='succeeded'", jobA) != 1 {
		t.Fatal("publication not atomic")
	}
	d, err := f.store.Get(context.Background(), alice, docA)
	if err != nil || d.State != "ready" || d.ExpiresAtMS != 10000 {
		t.Fatal("publication changed TTL")
	}
	recorded, err := f.store.ExportRecorded(context.Background(), alice, jobA, output().ID)
	if err != nil || !recorded {
		t.Fatal("missing durable job-output identity")
	}
	recorded, err = f.store.ExportRecorded(context.Background(), bob, jobA, output().ID)
	if err != nil || recorded {
		t.Fatal("cross-owner readback")
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	recorded, err = f.store.ExportRecorded(context.Background(), alice, jobA, output().ID)
	if err != nil || !recorded {
		t.Fatal("worker cannot reconcile disabled owner")
	}
	if err = f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); err == nil {
		t.Fatal("repeated publication accepted")
	}
}
func TestLatePublicationRejectedWithoutSideEffects(t *testing.T) {
	for _, mode := range []string{"owner", "runner", "revision", "cancel", "expiry", "deadline", "disabled", "budget", "format"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			claim(t, f)
			owner, runnerID, revision := alice, runner, int64(1)
			artifact := output()
			switch mode {
			case "owner":
				owner = bob
			case "runner":
				runnerID = "worker_other_000001"
			case "revision":
				must(t, f.docs, "UPDATE documents SET revision=2 WHERE id=?", docA)
			case "cancel":
				if err := f.store.RequestCancel(context.Background(), alice, jobA); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				f.now = 10000
			case "deadline":
				f.now = 9000
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "budget":
				artifact.Size = 101
			case "format":
				artifact.MediaType = "text/plain"
			}
			if err := f.store.PublishExport(context.Background(), owner, jobA, runnerID, revision, artifact); err == nil {
				t.Fatal("late or invalid publication accepted")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM published_outputs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM job_artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 1 {
				t.Fatal("failed publication partially committed")
			}
		})
	}
}
func TestPublicationRollsBackIfClockExpiresBeforeCommit(t *testing.T) {
	f := setup(t)
	claim(t, f)
	calls := 0
	f.store.now = func() time.Time {
		calls++
		if calls == 1 {
			return time.UnixMilli(2000)
		}
		return time.UnixMilli(10000)
	}
	if err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); !errors.Is(err, ErrConflict) {
		t.Fatalf("late commit accepted: %v", err)
	}
	if scalar(t, f.docs, "SELECT count(*) FROM artifacts") != 0 || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE state='succeeded'") != 0 {
		t.Fatal("late publication not rolled back")
	}
}
func TestTwoWorkersCannotClaimSameJob(t *testing.T) {
	f := setup(t)
	state2, err := storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer state2.Close()
	docs2, err := storage.Open(f.docsPath, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer docs2.Close()
	other, err := New(state2, docs2, func() time.Time { return time.UnixMilli(2000) }, fixtureSpace)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*Store{f.store, other} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			_, e := s.ClaimExport(context.Background(), alice, jobA, runner)
			results <- e
		}(store)
	}
	wg.Wait()
	close(results)
	success := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("multiple worker claims succeeded")
	}
}
func TestPrincipalDisableCannotInterleaveGuard(t *testing.T) {
	f := setup(t)
	other, err := storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	must(t, other, "PRAGMA busy_timeout=20")
	err = f.store.withEnabled(context.Background(), alice, func(tx *sql.Tx) error {
		if _, e := other.Exec("UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice); e == nil {
			t.Fatal("principal disable interleaved enabled guard")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	must(t, other, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	if _, err = f.store.Get(context.Background(), alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled owner remains readable")
	}
}
func TestCrossOwnerCancellationRejected(t *testing.T) {
	f := setup(t)
	if err := f.store.RequestCancel(context.Background(), bob, jobA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner cancellation accepted")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE id=? AND state='queued'", jobA) != 1 {
		t.Fatal("job changed")
	}
}

func TestExportMetadataChecksOwnerExpiryAndDisable(t *testing.T) {
	f := setup(t)
	claim(t, f)
	if err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output()); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.GetExport(context.Background(), alice, docA)
	if err != nil || got != output() {
		t.Fatalf("incorrect authorized export: %v", err)
	}
	if _, err = f.store.GetExport(context.Background(), bob, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner file metadata leaked")
	}
	f.now = 10000
	if _, err = f.store.GetExport(context.Background(), alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("expired file metadata leaked")
	}
	f.now = 2000
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	if _, err = f.store.GetExport(context.Background(), alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled principal got file metadata")
	}
}
func TestCancelledContextDoesNotLeakPrincipalGuard(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.store.Get(ctx, alice, docA); err == nil {
		t.Fatal("cancelled request accepted")
	}
	if _, err := f.store.Get(context.Background(), alice, docA); err != nil {
		t.Fatalf("state connection was not recovered: %v", err)
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
}

func TestCommitOutcomeIsReadBackBeforeCleanupDecision(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rolled-back", true: "committed-but-error"}[committed], func(t *testing.T) {
			f := setup(t)
			claim(t, f)
			f.store.commit = func(tx *sql.Tx) error {
				if committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				return errors.New("injected uncertain commit")
			}
			err := f.store.PublishExport(context.Background(), alice, jobA, runner, 1, output())
			if !errors.Is(err, ErrCommitUncertain) {
				t.Fatalf("uncertain outcome not surfaced: %v", err)
			}
			recorded, err := f.store.ExportRecorded(context.Background(), alice, jobA, output().ID)
			if err != nil || recorded != committed {
				t.Fatal("incorrect durable readback")
			}
			if committed && scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 0 {
				t.Fatal("committed reservation retained")
			}
			if !committed && scalar(t, f.docs, "SELECT count(*) FROM reservations WHERE job_id=?", jobA) != 1 {
				t.Fatal("failed commit lost reservation")
			}
		})
	}
}

func TestCancellationInsideGuardReleasesBothConnections(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := f.store.withEnabled(ctx, alice, func(*sql.Tx) error { cancel(); return ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
	if _, err = f.store.Get(context.Background(), alice, docA); err != nil {
		t.Fatalf("cancelled transaction left a lock: %v", err)
	}
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
}

func fixtureSpace(context.Context) (int64, error) { return 1 << 50, nil }
