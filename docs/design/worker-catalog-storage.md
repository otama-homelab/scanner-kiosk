# Worker catalog storage

The worker publication consumer now receives `workercatalog.Publisher`, a bound
backend authority with Pulse/Publish/PublishScanners/Stop commands. Its starter
returns that authority after registration. The SQLite adapter's BeginPublisher
keeps its private Lease and delegates to the existing guarded atomic operations;
the worker publisher no longer retains a concrete Store or constructs lease
tokens. Other backends must enforce the same guard/boot/generation contract.
Backend-neutral lifecycle tests cover begin/nil-authority refusal, independent
heartbeat, optional provider failure, publication/authority failure cancellation,
withdrawal errors and joined shutdown. They do not implement another production
database or prove its transaction/fencing semantics. Existing SQLite metadata,
authority-loss and device-concurrency tests remain separate evidence.

Document migration 0005 adds `processing_coordinator` and `ocr_catalog_snapshot`. These records are short-lived OCR/processing metadata authority, not scanner readiness, physical-idle proof, job reconciliation, or permission to clear device leases.

`workercatalog.Store.Begin` requires an active `workeridentity.Guard` acquired on the same private document volume. It assigns a monotonically increasing generation and the guard's fresh boot nonce, and atomically removes the previous catalog. The same guard cannot register twice or revive its expired/stopped generation. `Guard.WithActive` holds the guard alive across short local metadata transactions; callbacks must not call guard methods or perform process/network work. Correct document-volume binding and recovery-gate ordering remain the composition root's responsibility.

Pulse updates only the exact active boot/generation before its deadline, with nondecreasing heartbeat time. It cannot revive expired authority. Publication assigns its timestamps from the current authority, validates the shared catalog contract and stores at most 1 MiB of JSON. Caller-supplied timestamps are refused. A pulse extends authority but does not extend an already published snapshot; fresh publication is required. Stop marks the same authority stopped and removes its catalog. Old-generation writes cannot modify or remove a successor's information.

Readers join catalog and coordinator on boot/generation and enforce active state, creation/heartbeat/publication time bounds, both deadlines and matching payload timestamps. JSON decoding rejects unknown fields; metadata is validated before returning, and failed reads return no snapshot. Each read produces independent data. Reads use a transaction so a generation change cannot interleave catalog decoding. The caller receives model metadata only, without provider clients, credentials or execution paths.

These are leased records. Abrupt worker death can leave information readable until its short expiry; freshness is not instantaneous liveness detection. A new guard/generation clears it immediately on registration. SQLite commit failures return no authority/result; this component does not claim cross-process acknowledgement recovery. The worker integrates registration after its uncertain-prior-job gate, and uses the same document database and guarded volume as render dispatch.

Tests use real private SQLite and flock guards. They cover publish/read/resolve, detached reads, heartbeat versus snapshot expiry, graceful stop, new-guard takeover, stale-generation CAS even with an old guard still open, expired-authority refusal, duplicate boot registration and malformed/caller-timestamp rejection. The generation-CAS fixture directly changes the authoritative row to isolate the database fence; it is not evidence of safe physical scanner takeover.

The worker registers before dispatch, then runs heartbeat and catalog construction/publication independently at one third of `SCANNER_OCR_CATALOG_LEASE_SECONDS` (worker-only, default 30 seconds, range 15–300). Slow optional provider initialization does not block heartbeat. Provider initialization failure leaves OCR unavailable and retries; ordinary PDF/image processing remains available. Initialization is shared with execution under a mutex. No endpoint health check or document upload is performed for catalog publication. Loss of publication authority cancels worker execution through its existing stop/reconciliation path. Shutdown cancels and joins publication, withdraws the catalog, then closes providers/database/guard; withdrawal failure is returned rather than hidden by normal cancellation.

The API receives a read-only `workercatalog.Reader`, never the writer/provider factory. Its resolver accepts only current validated publications, pins the worker model version/default preset, returns 422 for unsupported selections and 503 for missing/stale metadata. Receipt replay precedes current resolution and survives catalog withdrawal. HTTP tests publish fixture metadata; they do not establish live OCR accuracy or device readiness. Runtime tests verify actual daemon publication with OCR explicitly disabled and withdrawal on cancellation, independent heartbeat during a blocked provider initializer, and cancellation after authority loss.

The [public capability endpoint](capability-http.md) and transactional per-model page/pixel admission checks are connected. The worker publishes its configured preferred model/languages/preset with the catalog, validated before publication/read. Browser OCR selection and scanner metadata publication are connected. Publication describes configured capabilities, not remote model health or physical scanner readiness. Production readiness generation, unknown native recovery and physical scanner acceptance remain incomplete gates.


Before startup maintenance and recovery inspection, the successor now calls
`WithdrawPrior` under its active guard to atomically stop the predecessor and
withdraw OCR/scanner metadata/readiness. It publishes no successor authority,
changes no jobs/leases/reservations and rejects its own already-begun boot.
This prevents admission against a still-fresh predecessor during startup. Failed
withdrawal stops startup; Begin still follows the recovery gate. See
[worker startup](worker-startup.md) for ordering, concurrency evidence and limits.


## Portable publisher persistence contract

`internal/testsupport/catalogcontract.Run` uses the bound `Publisher` and existing
read-only OCR/scanner source interfaces. Its fixture starts at 1000ms with a
10-second authority and controls the backend clock and real guard takeover.
The shared assertions receive no SQL handle or private lease. SQLite provides
an external-package fixture in `internal/workercatalog/backend_contract_linux_test.go`,
with a separate reader connection to the same database.

The suite checks backend-assigned timestamps, detached reads, snapshot expiry
that heartbeat cannot renew, fresh publication under renewed authority, refusal
after expiry/stop/guard replacement, and protection of a successor's OCR and
scanner metadata from the predecessor's publish/pulse/stop. This is metadata
publication evidence within one process. It does not prove native process stop,
physical fencing, readiness, job claims, uncertain commits or another backend's
durability. Existing specialized SQLite and runtime tests remain required.


## Startup failure diagnostics

The scan Session runtime fixture records the coordinator state, creation,
heartbeat and deadline timestamps, and OCR/scanner metadata row counts only if
the worker exits early. These are synthetic test records; boot values, raw
scanner identity and document content are not added to the diagnostic. A missing
scan job alone cannot distinguish registration failure from initial metadata
publication failure. The coordinator diagnostic narrows that distinction without
changing startup ordering, authority validation, deadlines or automatic retry.

The previous full-suite `busy` case exited before scan admission with
`worker catalog authority unavailable`. Its coordinator details were not captured;
its cause remains unknown. Passing isolated repetitions do not clear that failure
or establish physical scanner readiness.
