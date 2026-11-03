package documents_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"scanner-kiosk/internal/documenthttp"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/renderadmission"
	"scanner-kiosk/internal/scanadmission"
	scannerport "scanner-kiosk/internal/scanner"
	"scanner-kiosk/internal/scannermetadata"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/testsupport/documentcontract"
)

func newSQLiteContractFixture(t *testing.T) (documentcontract.Fixture, *documents.Store, *sql.DB, func() *documents.Store) {
	t.Helper()
	directory := t.TempDir()
	var handles []*sql.DB
	open := func(kind storage.Kind, name string) *sql.DB {
		db, err := storage.Open(filepath.Join(directory, name), kind)
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, db)
		t.Cleanup(func() { db.Close() })
		return db
	}
	state, docs := open(storage.State, "state.db"), open(storage.Documents, "documents.db")
	exec := func(db *sql.DB, query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var now atomic.Int64
	now.Store(2000)
	clock := func() time.Time { return time.UnixMilli(now.Load()) }
	space := func(context.Context) (int64, error) { return 1 << 30, nil }
	store, err := documents.New(state, docs, clock, space)
	if err != nil {
		t.Fatal(err)
	}
	f := documentcontract.Fixture{Records: store, Owner: "principal_contract_001", OtherOwner: "principal_contract_002", Document: "document_contract_001", Pages: [2]string{"page_contract_001", "page_contract_002"}}
	for _, owner := range []string{f.Owner, f.OtherOwner} {
		exec(state, "INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'guest',1000)", owner)
		exec(docs, "INSERT INTO owners(id) VALUES(?)", owner)
	}
	exec(docs, "INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,10000)", f.Document, f.Owner)
	for i, page := range f.Pages {
		artifact := page + "_original"
		exec(docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',20,zeroblob(32),1,1000)`, artifact, f.Document, f.Owner, artifact+".png")
		exec(docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,capture_size,capture_color,capture_dpi) VALUES(?,?,?,?,?,'a4','gray',300)`, page, f.Document, f.Owner, i, artifact)
	}
	f.Expire = func() { now.Store(10000) }
	f.Disable = func() { exec(state, "UPDATE principals SET disabled_at_ms=? WHERE id=?", now.Load(), f.Owner) }
	peer := func() *documents.Store {
		t.Helper()
		other, err := documents.New(open(storage.State, "state.db"), open(storage.Documents, "documents.db"), clock, space)
		if err != nil {
			t.Fatal(err)
		}
		return other
	}
	f.OpenPeerRecords = func() documenthttp.Records { return peer() }
	f.ReopenRecords = func() documenthttp.Records {
		for _, db := range handles {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}
		handles = nil
		state, docs = open(storage.State, "state.db"), open(storage.Documents, "documents.db")
		store, err = documents.New(state, docs, clock, space)
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	f.FailCommit = func(committed bool) func() {
		restore := documents.InjectContractCommitFailure(store, committed)
		t.Cleanup(restore)
		return restore
	}
	return f, store, docs, peer
}

func TestSQLitePublicEditContract(t *testing.T) {
	documentcontract.RunEdits(t, func(t *testing.T) documentcontract.Fixture { f, _, _, _ := newSQLiteContractFixture(t); return f })
}

func newSQLiteLifecycleContractFixture(t *testing.T) documentcontract.LifecycleFixture {
	t.Helper()
	f, store, _, _ := newSQLiteContractFixture(t)
	admission, err := store.AdmitRender(context.Background(), f.Owner, documents.RenderAdmission{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_render_001", Settings: documents.RenderSettings{Format: "pdf", PDFEncoding: "lossless"}, ReserveBytes: 1 << 20}, documents.AdmissionLimits{UserMaxBytes: 2 << 20, GlobalMaxBytes: 4 << 20, JobMaxBytes: 1 << 20, MaxPages: 10, JobTimeout: time.Minute, ReceiptRetention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return documentcontract.LifecycleFixture{Fixture: f, Job: admission.JobID, Revision: admission.Revision}
}

func TestSQLitePublicLifecycleContract(t *testing.T) {
	documentcontract.RunLifecycle(t, newSQLiteLifecycleContractFixture)
}

func TestSQLiteUncertainLifecycleContract(t *testing.T) {
	documentcontract.RunUncertainLifecycle(t, newSQLiteLifecycleContractFixture)
}

func newSQLiteRenderContractFixture(t *testing.T) documentcontract.RenderFixture {
	t.Helper()
	f, store, _, peer := newSQLiteContractFixture(t)
	service, err := documents.NewRenderAdmissionService(store)
	if err != nil {
		t.Fatal(err)
	}
	return documentcontract.RenderFixture{Fixture: f, Renders: service, Limits: renderServiceLimits(), OpenPeer: func() renderadmission.Records {
		other, err := documents.NewRenderAdmissionService(peer())
		if err != nil {
			t.Fatal(err)
		}
		return other
	}, Reopen: func() (documenthttp.Records, renderadmission.Records) {
		records := f.ReopenRecords()
		service, err := documents.NewRenderAdmissionService(records.(*documents.Store))
		if err != nil {
			t.Fatal(err)
		}
		return records, service
	}}
}

func TestSQLitePublicRenderContract(t *testing.T) {
	documentcontract.RunRenderAdmission(t, newSQLiteRenderContractFixture)
}

func TestSQLiteUncertainEditContract(t *testing.T) {
	documentcontract.RunUncertainEdits(t, func(t *testing.T) documentcontract.Fixture { f, _, _, _ := newSQLiteContractFixture(t); return f })
}

func TestSQLiteUncertainRenderContract(t *testing.T) {
	documentcontract.RunUncertainRender(t, newSQLiteRenderContractFixture)
}

func newSQLiteScanContractFixture(t *testing.T) documentcontract.ScanFixture {
	t.Helper()
	f, store, db, peer := newSQLiteContractFixture(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	const device = "scanner_contract_001"
	const boot = "worker_contract_001"
	capture := scannerport.Capture{Size: "a4", Color: "gray", DPI: 300}
	metadata := scannermetadata.Snapshot{PublishedAtMS: 1500, ExpiresAtMS: 11000, Devices: []scannermetadata.Device{{ID: device, Profiles: []scannerport.Profile{{Capture: capture, Width: 80, Height: 100, CheckedAtMS: 1500}}}}}
	payload, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO processing_coordinator(singleton,boot_id,generation,state,created_at_ms,heartbeat_at_ms,valid_until_ms) VALUES(1,?,1,'active',1000,1000,11000)", boot)
	exec("INSERT INTO device_leases(device_id,epoch,state) VALUES(?,1,'idle')", device)
	exec(`INSERT INTO scanner_readiness(device_id,boot_id,generation,lease_epoch,physical_id,handoff_id,scan_concurrency,ownership_confirmed_at_ms,local_stopped_at_ms,idle_observed_at_ms,published_at_ms,expires_at_ms) VALUES(?,?,1,1,'physical_contract_001','handoff_contract_001',1,1000,1000,1500,1500,11000)`, device, boot)
	exec("INSERT INTO scanner_metadata_snapshot(singleton,boot_id,generation,published_at_ms,expires_at_ms,payload) VALUES(1,?,1,1500,11000,?)", boot, string(payload))
	service, err := documents.NewReadyScanService(store)
	if err != nil {
		t.Fatal(err)
	}
	limits := readyServiceLimits()
	limits.DocumentLifetime = 8 * time.Second
	limits.RegisteredDevices = []string{device}
	return documentcontract.ScanFixture{Fixture: f, Scans: service, Limits: limits,
		Reopen: func() (documenthttp.Records, scanadmission.Records) {
			records := f.ReopenRecords()
			service, err := documents.NewReadyScanService(records.(*documents.Store))
			if err != nil {
				t.Fatal(err)
			}
			return records, service
		},
		OpenPeer: func() scanadmission.Records {
			service, err := documents.NewReadyScanService(peer())
			if err != nil {
				t.Fatal(err)
			}
			return service
		},
		Request:  documents.ScanRequest{Purpose: "document", DeviceID: device, Capture: documents.Capture{Size: capture.Size, Color: capture.Color, DPI: capture.DPI}, IdempotencyKey: "contract_scan_001", ReserveBytes: 100},
		Withdraw: func() { exec("DELETE FROM scanner_readiness") }}
}

func TestSQLitePublicScanContract(t *testing.T) {
	documentcontract.RunScanAdmission(t, newSQLiteScanContractFixture)
}

func TestSQLiteUncertainScanContract(t *testing.T) {
	documentcontract.RunUncertainScan(t, newSQLiteScanContractFixture)
}

func newSQLiteDispatchContractFixture(t *testing.T, kind string) documentcontract.DispatchFixture {
	t.Helper()
	var base documentcontract.Fixture
	var admission documents.Admission
	const boot = "worker_contract_001"
	if kind == "scan" {
		f := newSQLiteScanContractFixture(t)
		a, err := f.Scans.AdmitReadyScan(context.Background(), f.Owner, f.Request, f.Limits)
		if err != nil {
			t.Fatal(err)
		}
		base, admission = f.Fixture, a.Admission
	} else {
		f := newSQLiteRenderContractFixture(t)
		a, err := f.Renders.AdmitRender(context.Background(), f.Owner, documents.RenderAdmission{DocumentID: f.Document, Revision: 1, IdempotencyKey: "contract_dispatch_render_001", Settings: documents.RenderSettings{Format: "pdf"}, ReserveBytes: 1 << 20}, f.Limits)
		if err != nil {
			t.Fatal(err)
		}
		base, admission = f.Fixture, a
	}
	base.Document = admission.DocumentID
	return documentcontract.DispatchFixture{Fixture: base, Job: admission.JobID, Boot: boot, Claims: base.Records.(*documents.Store), OpenPeerClaims: func() documentcontract.DispatchRecords { return base.OpenPeerRecords().(*documents.Store) }}
}

func TestSQLiteUncertainDispatchContract(t *testing.T) {
	documentcontract.RunUncertainDispatch(t, newSQLiteDispatchContractFixture)
}

func newSQLiteRenderPublicationContractFixture(t *testing.T) documentcontract.RenderPublicationFixture {
	t.Helper()
	f := newSQLiteDispatchContractFixture(t, "render")
	claimed, err := f.Claims.ClaimNextRender(context.Background(), f.Boot)
	if err != nil {
		t.Fatal(err)
	}
	artifact := documents.Export{ID: "artifact_contract_export_001", RelativePath: "contract/result.pdf", MediaType: "application/pdf", Size: 20, SHA256: sha256.Sum256([]byte("synthetic publication metadata"))}
	return documentcontract.RenderPublicationFixture{DispatchFixture: f, Revision: claimed.Job.Revision, Artifact: artifact, Publications: f.Records.(*documents.Store), OpenPeerPublications: func() documentcontract.RenderPublicationRecords { return f.OpenPeerRecords().(*documents.Store) }}
}

func TestSQLiteUncertainRenderPublicationContract(t *testing.T) {
	documentcontract.RunUncertainRenderPublication(t, newSQLiteRenderPublicationContractFixture)
}

func newSQLiteScanPublicationContractFixture(t *testing.T) documentcontract.ScanPublicationFixture {
	t.Helper()
	f := newSQLiteDispatchContractFixture(t, "scan")
	claimed, err := f.Claims.ClaimNextScanWithLimit(context.Background(), f.Boot, 1)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot documents.ScanSnapshot
	if err := json.Unmarshal([]byte(claimed.Job.Configuration), &snapshot); err != nil {
		t.Fatal(err)
	}
	stop := documents.ScanStop{DeviceID: snapshot.DeviceID, WorkerBootID: f.Boot, LeaseEpoch: snapshot.LeaseEpoch, ProcessStoppedAtMS: 2000, IdleObservedAtMS: 2000}
	artifact := documents.ArtifactRecord{ID: "artifact_contract_capture_001", RelativePath: "contract/original.png", MediaType: "image/png", Size: 20, SHA256: sha256.Sum256([]byte("synthetic capture metadata"))}
	return documentcontract.ScanPublicationFixture{DispatchFixture: f, Revision: claimed.Job.Revision, Artifact: artifact, Stop: stop, Publications: f.Records.(*documents.Store), OpenPeerPublications: func() documentcontract.ScanPublicationRecords { return f.OpenPeerRecords().(*documents.Store) }}
}

func TestSQLiteUncertainScanPublicationContract(t *testing.T) {
	documentcontract.RunUncertainScanPublication(t, newSQLiteScanPublicationContractFixture)
}

func contractThumbnail(original string) documents.ThumbnailRecord {
	return documents.ThumbnailRecord{OriginalID: original, Artifact: documents.ArtifactRecord{ID: "thumbnail_contract_001", RelativePath: "contract/thumbnail.jpg", MediaType: "image/jpeg", Size: 20, PixelWidth: 32, PixelHeight: 40, SHA256: sha256.Sum256([]byte("synthetic thumbnail metadata"))}}
}

func TestSQLiteUncertainRenderThumbnailContract(t *testing.T) {
	documentcontract.RunUncertainRenderPublication(t, func(t *testing.T) documentcontract.RenderPublicationFixture {
		f := newSQLiteRenderPublicationContractFixture(t)
		f.Thumbnails = []documents.ThumbnailRecord{contractThumbnail(f.Pages[0] + "_original")}
		return f
	})
}

func TestSQLiteUncertainScanThumbnailContract(t *testing.T) {
	documentcontract.RunUncertainScanPublication(t, func(t *testing.T) documentcontract.ScanPublicationFixture {
		f := newSQLiteScanPublicationContractFixture(t)
		f.Thumbnails = []documents.ThumbnailRecord{contractThumbnail(f.Artifact.ID)}
		return f
	})
}

func TestSQLiteUncertainStopContract(t *testing.T) {
	documentcontract.RunUncertainStop(t, func(t *testing.T, kind string) documentcontract.StopFixture {
		var f documentcontract.DispatchFixture
		var scanStop documents.ScanStop
		if kind == "scan" {
			running := newSQLiteScanPublicationContractFixture(t)
			f, scanStop = running.DispatchFixture, running.Stop
		} else {
			running := newSQLiteRenderPublicationContractFixture(t)
			f = running.DispatchFixture
		}
		return documentcontract.StopFixture{DispatchFixture: f, ScanStop: scanStop, WorkspaceCleanedAtMS: 2000, RenderStop: documents.RenderStop{RunnerID: f.Boot, ProcessStoppedAtMS: 2000, WorkspaceCleanedAtMS: 2000}, Stops: f.Records.(*documents.Store), OpenPeerStops: func() documentcontract.StopRecords { return f.OpenPeerRecords().(*documents.Store) }}
	})
}

func TestSQLiteReopenEditContract(t *testing.T) {
	documentcontract.RunReopenEdits(t, func(t *testing.T) documentcontract.Fixture { f, _, _, _ := newSQLiteContractFixture(t); return f })
}

func TestSQLiteReopenLifecycleContract(t *testing.T) {
	documentcontract.RunReopenLifecycle(t, newSQLiteLifecycleContractFixture)
}

func TestSQLiteReopenRenderContract(t *testing.T) {
	documentcontract.RunReopenRenderAdmission(t, newSQLiteRenderContractFixture)
}

func TestSQLiteReopenScanContract(t *testing.T) {
	documentcontract.RunReopenScanAdmission(t, newSQLiteScanContractFixture)
}
