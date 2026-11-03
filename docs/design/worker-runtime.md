# Initial executable worker

`go build ./cmd/scanner` now builds a Linux command with `scanner worker` and `scanner worker --once`. The worker command reads common worker environment settings and never reads HTTP/OIDC configuration. It requires existing private state/document directories and opens `state.sqlite3` and `documents.sqlite3` under their respective roots. It acquires the document-root worker lock with a fresh boot nonce for its whole lifetime and uses the filesystem headroom probe.

`documents.ClaimNextRender` holds the principal snapshot and document IMMEDIATE transaction while selecting and claiming the oldest eligible enabled-owner render. It checks current revision/state, deadlines, reservation and physical capacity. Disabled owners are skipped. Running/cancelling/reconciling work from any boot, or a queued job pinned to a worker/with a non-null immutable start marker, blocks new dispatch. Commit uncertainty returns no claimed work to execute. Tests race two dispatchers and verify one claim.

At startup, persisted uncertain/active work or any non-idle device lease causes an explicit recovery-required failure. Lease state is checked independently of job state: even a terminal scan job does not establish native stop or justify releasing its lease/reservation. The startup check preserves those records for reconciliation. The initial worker does not pretend that acquiring the local lock proves old work or hardware stopped. After dispatch, `ExecuteClaimed` performs watched generation/publication/cleanup. Fully terminalized processing failures allow later jobs; an unresolved claim/publication/cleanup outcome stops dispatch and preserves data/reservations. A recovery object is not blindly closed. Once mode handles at most one job; continuous mode polls every second until SIGINT/SIGTERM. No scanner is marked ready by this runtime. With acquisition enabled, it can execute newly admitted scans pinned to its current boot; old queued scans still fail the startup gate.

This is an implementation stage of the full worker. It currently dispatches render jobs to the PDF/single-image pipelines and compatible native OCR text-layer providers. Scanner readiness and HTTP scan admission, positioned LLM rendering, durable restart reconciliation, TTL GC, deployment/image and device acceptance remain pending. Do not deploy it as a scanner replacement yet. The existing service is unchanged.

With worker-only `SCANNER_ACQUISITION_ENABLED=true` (default false), Run constructs the guarded scanner Session after the recovery gate and before metadata publication/dispatch. Failure is explicit; any partial Session is closed. Shutdown joins publication and processing, drains/closes the Session with a fresh bounded cleanup context, then releases stores and the boot guard. Cleanup failure preserves the inputs and returns ErrReconciliation even on cancellation. This opt-in establishes neither ready leases nor physical exclusivity; HTTP scan admission/readiness remain pending. Interrupted claim errors retain both the storage failure and context cancellation rather than silently becoming a clean cancellation.

## Reproduce the compiled CLI proof

Use a Python environment containing Pillow and pikepdf:

```sh
/path/to/python scripts/check-worker.py --receipt /tmp/worker-cli-check.json
```

This builds the real command into an isolated temporary directory, initializes real private stores, inserts a synthetic admitted-job fixture, runs worker once, independently inspects PDF pixels/rotation and reservation/publication state, reruns once to check non-replay, and rejects the unimplemented HTTP role. Temporary data/binary are removed. Runtime Go integration tests separately use real admission rather than the script's direct SQLite fixture. Neither proof involves a network scanner, IdP, live OCR provider or production deployment.

Photo JPEG/PNG export is now connected; see [image export](image-export.md). The compiled-command proof checks both image formats as well as PDF.

The [OCR worker pipeline](ocr-worker-pipeline.md) now has a separate compiled-command proof through `check-tesseract.py`, using an installed native provider and real admission/publication. Optional provider initialization is lazy so ordinary exports remain independent of OCR configuration failures.


## Boot-pinned scan dispatch

`documents.ClaimNextScan` selects and claims inside the same principal snapshot / document IMMEDIATE transaction used for render dispatch. It selects only enabled owners, never-started queued scans pinned to the exact current worker boot, current scanning documents/revisions/deadlines, held device/epoch and positive reservations. It reuses the direct ClaimScan transaction and execution-capacity check. Foreign active/uncertain jobs and other non-idle leases block heavy work across worker IDs. A commit uncertainty returns no claimed owner/job, and never causes driver invocation or automatic retry. Render dispatch also checks unresolved device leases independently of job state.

Run dispatches one scan before considering renders, using the configured Session and the existing monitored `scanjob.ExecuteClaimed`. A handled scan consumes the iteration/once limit even if it terminalized as failed. Confirmed successful output is registered once; fully stopped failed/interrupted work must be read back as terminalized before dispatch can continue. Unresolved Prepared work or unreadable terminal state stops the worker and preserves original staging/lease/reservation. Native acquisition is never repeated to resolve a DB/publication uncertainty. The default disabled acquisition flag creates no scanner Session and cannot dispatch scans.

Runtime tests admit a new scan only after reading the actual running worker boot from its processing record. They explicitly seed a fixture idle lease, then use a fake native executable, parameter helper and local HTTP eSCL status server. Product config/Session, native supervision, heartbeat/capture validation, publication, lease/reservation handling and worker shutdown are real. Cases cover success, Processing refusal with retained resources, and cancellation of a joined single native process with interrupted cleanup. Capture markers prove one invocation. A separate uncertainty port test proves no resolution/idle call follows a failed claim. This is local synthetic acquisition, not real Canon capture or readiness/exclusive-writer acceptance.


Enabled scanner Sessions now have a separate read-only metadata publication goroutine under the processing coordinator. It is joined before session/config cleanup and its late unresolved failures are returned. Verified combinations reach the API without initializing scanner leases/readiness. See [scanner metadata](scanner-metadata.md) for the model, bounded native checks, caching and acceptance scope.

The initial global sequential scheduler is superseded as a target by the
[multiple-scanner requirement](multiple-scanners.md). The native driver and daemon now support per-device bounded scan tasks; render
processing remains serial and exclusive with capture.

The daemon now implements bounded scan tasks configured by worker-only
`SCANNER_SCAN_CONCURRENCY` (default 1, accepted 1–32; effective cap also limited
by registered devices and the logical global/job byte ratio). Each scan is claimed
serially then executed concurrently up to that cap, with exact boot/device/epoch
fences. It joins every task before publisher/Session/store/guard disposal and
retains errors discovered during join. `--once` still handles at most one job.
Render processing remains serial and waits for scan tasks; independent simultaneous
OCR/PDF scheduling and aggregate runtime memory enforcement are unfinished.
Two-device real-Run synthetic tests prove overlapping native capture, two-page
publication to separate documents, cancellation/authority-loss drain and guard
lifetime. See multiple-scanners.md for evidence and remaining device acceptance.

Metadata refresh now uses a separate fixed pool of at most four device batches.
Completed per-device results reach the merge publisher without waiting for a slow
batch. One outstanding batch per device, 30-second batch deadline, per-device
cache/age pruning and joined shutdown bound work and preserve native evidence.
This does not initialize device readiness or prove real native resource headroom.
