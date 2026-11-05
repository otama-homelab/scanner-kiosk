package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
)

func renderFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	must(t, f.docs, "DELETE FROM jobs WHERE id=?", jobA)
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docA)
	seedPage(t, f, docA, alice, "original_alice_0001", "page_alice_000001")
	return f
}
func seedPage(t *testing.T, f *fixture, document, owner, artifact, page string) {
	t.Helper()
	seedPageAt(t, f, document, owner, artifact, page, 0)
}
func seedPageAt(t *testing.T, f *fixture, document, owner, artifact, page string, ordinal int) {
	t.Helper()
	seedPageWithCapture(t, f, document, owner, artifact, page, ordinal, "a4", "color", 300)
}
func seedPageWithCapture(t *testing.T, f *fixture, document, owner, artifact, page string, ordinal int, size, color string, dpi int) {
	t.Helper()
	must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',20,zeroblob(32),1,1000)`, artifact, document, owner, artifact)
	must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,?,?,?,?,?)`, page, document, owner, ordinal, artifact, size, color, dpi)
}
func admissionRequest() RenderAdmission {
	return RenderAdmission{DocumentID: docA, Revision: 1, IdempotencyKey: "request_render_0001", Settings: RenderSettings{Format: "pdf"}, ReserveBytes: 50}
}
func admissionLimits() AdmissionLimits {
	return AdmissionLimits{UserMaxBytes: 1000, GlobalMaxBytes: 2000, JobMaxBytes: 100, MaxPages: 50, JobTimeout: time.Minute, ReceiptRetention: 72 * time.Hour}
}
func TestRenderAdmissionAndReplaySnapshot(t *testing.T) {
	f := renderFixture(t)
	request := admissionRequest()
	request.Settings.OCR = RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", ModelVersion: "original-model", Languages: []string{"ja", "en"}, QualityPreset: "auto"}
	first, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if first.Replay || first.JobID == "" || first.Revision != 2 {
		t.Fatal("invalid admission")
	}
	request.Settings.OCR.ModelVersion = "replacement-model"
	request.ReserveBytes = 10000
	second, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil || !second.Replay || second.JobID != first.JobID {
		t.Fatalf("replay failed: %v", err)
	}
	var raw string
	var deadline int64
	if err = f.docs.QueryRow("SELECT configuration_json,deadline_at_ms FROM jobs WHERE id=?", first.JobID).Scan(&raw, &deadline); err != nil {
		t.Fatal(err)
	}
	var snapshot RenderSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.Version != 1 || snapshot.Settings.OCR.ModelVersion != "original-model" || snapshot.Settings.PDFEncoding != "jpeg" || len(snapshot.Pages) != 1 || snapshot.Pages[0].DPI != 300 || deadline != 10000 {
		t.Fatal("incorrect immutable snapshot/deadline")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 || scalar(t, f.docs, "SELECT count(*) FROM request_receipts WHERE length(key_hash)=32 AND length(request_hash)=32") != 1 {
		t.Fatal("replay created work or invalid receipt")
	}
	if scalar(t, f.docs, "SELECT count(*) FROM documents WHERE id=? AND expires_at_ms=10000 AND state='processing'", docA) != 1 {
		t.Fatal("admission changed expiry")
	}
}
func TestSameKeyDifferentCommandConflicts(t *testing.T) {
	f := renderFixture(t)
	request := admissionRequest()
	if _, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits()); err != nil {
		t.Fatal(err)
	}
	request.Settings.PDFEncoding = "lossless"
	if _, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits()); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different command reused key: %v", err)
	}
}
func TestGoneReceiptNeverCreatesAnotherJob(t *testing.T) {
	for _, mode := range []string{"expired-document", "deleted-job", "expired-receipt"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			first, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "expired-document":
				f.now = 10000
			case "deleted-job":
				must(t, f.docs, "DELETE FROM jobs WHERE id=?", first.JobID)
			case "expired-receipt":
				f.now = 72*time.Hour.Milliseconds() + 2000
			}
			if _, err = f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits()); !errors.Is(err, ErrGone) {
				t.Fatalf("gone receipt reused: %v", err)
			}
			if scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 1 {
				t.Fatal("receipt replaced")
			}
		})
	}
}
func TestAdmissionRejectsInvalidStateAndPolicyWithoutPartialWrites(t *testing.T) {
	for _, mode := range []string{"owner", "disabled", "revision", "expired", "empty", "page-limit", "not-ready-original", "busy", "user-quota", "global-quota", "job-quota", "missing-model-version", "format"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			request := admissionRequest()
			limits := admissionLimits()
			owner := alice
			switch mode {
			case "owner":
				owner = bob
			case "disabled":
				must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
			case "revision":
				request.Revision = 2
			case "expired":
				f.now = 10000
			case "empty":
				must(t, f.docs, "DELETE FROM pages WHERE document_id=?", docA)
			case "page-limit":
				seedPageAt(t, f, docA, alice, "original_alice_0002", "page_alice_000002", 1)
				limits.MaxPages = 1
			case "not-ready-original":
				must(t, f.docs, "UPDATE artifacts SET state='deleting' WHERE document_id=?", docA)
			case "busy":
				must(t, f.docs, "UPDATE documents SET state='scanning' WHERE id=?", docA)
			case "user-quota":
				limits.UserMaxBytes = 60
				limits.JobMaxBytes = 60
			case "global-quota":
				limits.GlobalMaxBytes = 60
				limits.JobMaxBytes = 60
			case "job-quota":
				request.ReserveBytes = 101
			case "missing-model-version":
				request.Settings.OCR = RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", Languages: []string{"ja"}}
			case "format":
				request.Settings.Format = "png"
			}
			if _, err := f.store.AdmitRender(context.Background(), owner, request, limits); err == nil {
				t.Fatal("invalid admission accepted")
			}
			if scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 0 {
				t.Fatal("failed admission partially committed")
			}
		})
	}
}
func secondStore(t *testing.T, f *fixture) *Store {
	t.Helper()
	state, err := storage.Open(f.statePath, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	docs, err := storage.Open(f.docsPath, storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docs.Close() })
	store, err := New(state, docs, func() time.Time { return time.UnixMilli(2000) }, fixtureSpace)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func TestConcurrentAdmissionOfSameKeyCreatesOneJob(t *testing.T) {
	f := renderFixture(t)
	other := secondStore(t, f)
	type result struct {
		admission Admission
		err       error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, store := range []*Store{f.store, other} {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			a, e := store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
			results <- result{a, e}
		}(store)
	}
	wg.Wait()
	close(results)
	var job string
	fresh, replayed := 0, 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if job == "" {
			job = r.admission.JobID
		}
		if job != r.admission.JobID {
			t.Fatal("two job identities")
		}
		if r.admission.Replay {
			replayed++
		} else {
			fresh++
		}
	}
	if fresh != 1 || replayed != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 1 {
		t.Fatal("duplicate work admitted")
	}
}
func TestConcurrentGlobalBudgetReservation(t *testing.T) {
	f := renderFixture(t)
	must(t, f.docs, "UPDATE documents SET state='review' WHERE id=?", docB)
	seedPage(t, f, docB, bob, "original_bob_000001", "page_bob_00000001")
	other := secondStore(t, f)
	limits := admissionLimits()
	limits.GlobalMaxBytes = 100
	limits.JobMaxBytes = 100
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{f.store, other} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			request := admissionRequest()
			owner := alice
			if i == 1 {
				owner = bob
				request.DocumentID = docB
			}
			_, err := store.AdmitRender(context.Background(), owner, request, limits)
			results <- err
		}(i, store)
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrQuota) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || rejected != 1 || scalar(t, f.docs, "SELECT COALESCE(SUM(byte_size),0) FROM reservations") != 50 {
		t.Fatal("global budget oversubscribed")
	}
}
func TestUncertainAdmissionReplaysCommittedReceipt(t *testing.T) {
	f := renderFixture(t)
	f.store.commit = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errors.New("injected post-commit error")
	}
	if _, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits()); !errors.Is(err, ErrCommitUncertain) {
		t.Fatal("commit uncertainty lost")
	}
	f.store.commit = func(tx *sql.Tx) error { return tx.Commit() }
	result, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil || !result.Replay || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 1 {
		t.Fatal("uncertain retry duplicated job")
	}
}
func TestAdmissionThenClaimAndPublication(t *testing.T) {
	f := renderFixture(t)
	admitted, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.store.ClaimExport(context.Background(), alice, admitted.JobID, runner)
	if err != nil || job.Revision != 2 {
		t.Fatalf("admitted job not claimable: %v", err)
	}
	if err = f.store.PublishExport(context.Background(), alice, admitted.JobID, runner, admitted.Revision, output()); err != nil {
		t.Fatal(err)
	}
	replay, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil || !replay.Replay || replay.JobID != admitted.JobID || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 {
		t.Fatal("completed receipt created fresh work")
	}
}

func TestSnapshotPageOrderAndPerPageCaptureSettings(t *testing.T) {
	f := renderFixture(t)
	seedPageWithCapture(t, f, docA, alice, "original_alice_0002", "page_alice_000002", 1, "a5", "gray", 600)
	must(t, f.docs, "UPDATE pages SET rotation=90 WHERE id=?", "page_alice_000002")
	// Capture metadata is immutable, so create the first fixture with its original
	// values and assert that every page retains an independent snapshot.
	admitted, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", admitted.JobID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot RenderSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil || len(snapshot.Pages) != 2 || snapshot.Pages[0].ID != "page_alice_000001" || snapshot.Pages[0].Rotation != 0 || snapshot.Pages[1].ID != "page_alice_000002" || snapshot.Pages[1].Rotation != 90 || snapshot.Pages[1].DPI != 600 || snapshot.Pages[1].Size != "a5" || snapshot.Pages[1].Color != "gray" || snapshot.Pages[0].DPI != 300 {
		t.Fatal("page order/settings snapshot incorrect")
	}
	must(t, f.docs, "UPDATE pages SET rotation=180 WHERE id=?", "page_alice_000002")
	var after string
	f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", admitted.JobID).Scan(&after)
	if after != raw {
		t.Fatal("later edit changed admitted job snapshot")
	}
}
func TestResolvedDefaultPresetDoesNotChangeReplayIdentity(t *testing.T) {
	f := renderFixture(t)
	request := admissionRequest()
	request.Settings.OCR = RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", ModelVersion: "v1", Languages: []string{"ja", "en"}}
	request.ResolvedQualityPreset = "auto"
	first, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	request.ResolvedQualityPreset = "block"
	request.Settings.OCR.ModelVersion = "v2"
	again, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil || !again.Replay || again.JobID != first.JobID {
		t.Fatal("server default change invalidated original receipt")
	}
	var raw string
	f.docs.QueryRow("SELECT configuration_json FROM jobs WHERE id=?", first.JobID).Scan(&raw)
	var snapshot RenderSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil || snapshot.Settings.OCR.QualityPreset != "auto" {
		t.Fatal("resolved default was not frozen")
	}
}

func TestNewOutputAdvancesRevisionAndInvalidatesOldPublicationOnce(t *testing.T) {
	f := renderFixture(t)
	first, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, first.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PublishExport(context.Background(), alice, first.JobID, runner, first.Revision, output()); err != nil {
		t.Fatal(err)
	}
	request := admissionRequest()
	request.Revision = first.Revision
	request.IdempotencyKey = "request_render_0002"
	request.Settings.PDFEncoding = "lossless"
	second, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil || second.Revision != first.Revision+1 {
		t.Fatalf("new output did not advance revision: %v", err)
	}
	if _, err = f.store.GetExport(context.Background(), alice, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("old output remained current")
	}
	again, err := f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil || !again.Replay || again.Revision != second.Revision {
		t.Fatal("replay advanced revision again")
	}
	d, err := f.store.Get(context.Background(), alice, docA)
	if err != nil || d.Revision != second.Revision || d.ExpiresAtMS != 10000 {
		t.Fatal("incorrect revision or TTL")
	}
	recorded, err := f.store.ExportRecorded(context.Background(), alice, first.JobID, output().ID)
	if err != nil || !recorded {
		t.Fatal("older job lost durable output identity")
	}
}
func TestRejectedNewOutputKeepsPreviousPublishedVersion(t *testing.T) {
	f := renderFixture(t)
	first, err := f.store.AdmitRender(context.Background(), alice, admissionRequest(), admissionLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ClaimExport(context.Background(), alice, first.JobID, runner); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PublishExport(context.Background(), alice, first.JobID, runner, first.Revision, output()); err != nil {
		t.Fatal(err)
	}
	request := admissionRequest()
	request.Revision = first.Revision
	request.IdempotencyKey = "request_render_0002"
	limits := admissionLimits()
	limits.UserMaxBytes = 60
	limits.JobMaxBytes = 60
	if _, err = f.store.AdmitRender(context.Background(), alice, request, limits); !errors.Is(err, ErrQuota) {
		t.Fatalf("expected capacity rejection: %v", err)
	}
	got, err := f.store.GetExport(context.Background(), alice, docA)
	if err != nil || got.ID != output().ID {
		t.Fatal("rejected new output invalidated old publication")
	}
	d, err := f.store.Get(context.Background(), alice, docA)
	if err != nil || d.Revision != first.Revision || d.State != "ready" {
		t.Fatal("failed admission changed document")
	}
}

func TestCanonicalCommandSurvivesFieldOrderAndPreservesIntegers(t *testing.T) {
	first := struct {
		Revision int64  `json:"revision"`
		Name     string `json:"name"`
	}{9007199254740993, "fixture"}
	second := struct {
		Name     string `json:"name"`
		Revision int64  `json:"revision"`
	}{"fixture", 9007199254740993}
	a, err := canonicalCommand(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalCommand(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || string(a) != `{"name":"fixture","revision":9007199254740993}` {
		t.Fatal("canonical digest depends on layout or loses integer precision")
	}
}

func TestOCRModelLimitsAreCheckedBeforeMutation(t *testing.T) {
	for _, mode := range []string{"pixel-limit", "page-limit", "unknown-dimensions", "boundary", "plain", "imported-pdf"} {
		t.Run(mode, func(t *testing.T) {
			f := renderFixture(t)
			// Replace the synthetic page before admission; dimensions are immutable after insert.
			must(t, f.docs, "DELETE FROM pages WHERE document_id=?", docA)
			var width, height any = 80, 100
			if mode == "unknown-dimensions" {
				width, height = nil, nil
			}
			must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi,pixel_width,pixel_height) VALUES('page_alice_000001',?,?,0,'original_alice_0001','a4','color',300,?,?)`, docA, alice, width, height)
			request := admissionRequest()
			request.Settings.OCR = RenderOCR{Enabled: true, ProviderID: "tesseract", ModelID: "installed", ModelVersion: "version", Languages: []string{"ja"}}
			limits := admissionLimits()
			limits.OCRMaxPages = 1
			limits.OCRMaxPagePixels = 8000
			if mode == "pixel-limit" {
				limits.OCRMaxPagePixels = 7999
			}
			if mode == "page-limit" {
				seedPageAt(t, f, docA, alice, "original_alice_0002", "page_alice_000002", 1)
			}
			if mode == "imported-pdf" {
				seedImportedPDFPage(t, f)
			}
			if mode == "plain" {
				request.Settings.OCR = RenderOCR{}
				limits.OCRMaxPagePixels = 1
			}
			result, err := f.store.AdmitRender(context.Background(), alice, request, limits)
			if mode == "boundary" || mode == "plain" || mode == "imported-pdf" {
				if err != nil {
					t.Fatal(err)
				}
				limits.OCRMaxPagePixels = 1
				replay, err := f.store.AdmitRender(context.Background(), alice, request, limits)
				if err != nil || !replay.Replay || replay.JobID != result.JobID {
					t.Fatal("replay changed with model limits", err)
				}
			} else {
				if !errors.Is(err, ErrOCRLimits) {
					t.Fatal("incompatible OCR accepted", err)
				}
				if scalar(t, f.docs, "SELECT revision FROM documents WHERE id=?", docA) != 1 || scalar(t, f.docs, "SELECT count(*) FROM jobs") != 0 || scalar(t, f.docs, "SELECT count(*) FROM reservations") != 0 || scalar(t, f.docs, "SELECT count(*) FROM request_receipts") != 0 {
					t.Fatal("rejected OCR mutated durable state")
				}
			}
		})
	}
}
