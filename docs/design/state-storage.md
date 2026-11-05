# Server state and storage design

Status: local SQLite schema/runtime and logical identity recovery are implemented in the documented fixture scopes; production filesystem/retention/device acceptance remains pending.

## Persistence boundaries

Use two local SQLite databases and two mounts, with no external database service:

1. Durable application state: stable OIDC/simple principal mappings, local credential verifiers and disabled flags, plus live sessions and authorization protocol records. Logical identity export includes only the durable identity subset, excluding live sessions/protocol state; deployment configuration and secrets are recovered separately.
2. Temporary document state: documents, pages, artifacts, jobs, leases, request deduplication, alongside originals, thumbnails, OCR text, intermediate files, and completed exports. This entire mount is excluded from snapshots, replication, offsite backup, and application logs.

This separation prevents backups of durable settings from retaining expired scans. Database sessions do not contain images, OCR text, filenames, API tokens, access tokens, or document payloads. OIDC authorization attempts contain only short-lived protocol state and expire independently of document TTL.

SQLite WAL is local to one replica/host with a supported filesystem. The application and worker share that filesystem. An NFS-mounted database or replicas with independent locks are not supported by the initial design. Use foreign keys, bounded busy timeouts, short transactions, and atomic conditional updates. Do not hold a transaction while scanning, processing files, or calling external APIs.

Cross-store operations first ensure a durable principal exists, then create temporary records with that opaque principal ID. A failed document transaction must not create a new authorization identity. No cross-database atomic transaction is assumed. Principal deletion first prevents access and schedules temporary cleanup; it does not reassign ownership.

## Documents and revisions

A document is created by the first scan request, not by rendering Home. Its owner and creation/expiry instants are immutable. Each captured page has a stable ID and capture settings snapshot. Reordering, rotation, deletion, and output settings increase an expected revision. Editing an old revision returns a conflict; it never silently overwrites another tab.

Processing input records the exact page IDs/order, transforms, OCR plugin/model version, languages, compression, deadline, and input revision. Output is first written to an isolated staging directory. After file closure and validation, publish only if the owner/document is still active, unexpired, and at the expected revision. A late result never replaces a newer output. Failed publication is cleanup work, not a usable download.

## Job, scanner lease, and crash recovery

API requests commit a job and deduplication result before returning acceptance. Repeating the same idempotency key and request digest returns the same job; a different body with the same key conflicts. A refreshed browser queries the job rather than reading the original again.

A physical device has at most one active scanner lease. The lease is separate from the multi-page document and is released after a completed/failed/cancelled physical scan, not after the user finishes exchanging pages. Each lease has a fencing epoch. Heartbeat expiry alone never proves that a scanner or child process has stopped. Recovery first reconciles/kills the old process tree and checks the device adapter, then changes lease ownership. An unreconciled lease blocks a new scan instead of creating a second writer.

The worker owns child process groups. Cancellation and deadline first record the request, signal the process group, escalate after a bounded grace period, wait for exit, close handles, then clean files and release the lease. Cancelling is not displayed as cancelled before completion. No physical scan is automatically retried after uncertain completion or restart.

## Retention, downloads, and deletion

Expiry is creation + configured TTL, initially 24 hours. Editing, exporting, and downloading never extend it. All API/list/file/thumbnail/job/event requests check owner and expiry on the server. Browser time is display-only. Authorized download streams are cancelled when their document expires or is deleted; new ranges and reconnects are rejected.

Delete/expiry first prevent new references and cancel jobs/transfers. Cleanup waits for owned worker and stream handles to close, then removes originals, derived artifacts, intermediate files, and temporary metadata. Target physical cleanup lag is five minutes. Failure is persisted for retry and startup reconciliation. Secure deletion on SSD or storage snapshots is not promised; excluded backup placement must be verified before production.

The storage root is administrator-controlled. User input never becomes a filesystem path. Each document/job uses opaque IDs under the configured root, restrictive permissions, and bounded space reservations. Symlinks and traversal cannot escape the root. Disk-full is rejected before scan admission when capacity cannot be reserved; other users' files are never evicted.

## Limits and deployment validation

Initial limits remain provisional: 20 documents/500 MiB per principal; 50 pages/200 MiB per job; one physical scan per device; bounded distinct-device dispatch and single render concurrency. Actual 300/600 dpi and OCR measurements must determine final defaults. Advertised 1200 dpi remains unavailable until resource acceptance.

The current NAS recursive snapshots include the old scanservjs volume. A new document volume must be outside that protected subtree or explicitly excluded with evidence. No existing persistent volume or backup policy is reused merely because it is available. Provisioning and live exclusion checks are still pending.

Initial-release scope update: no Paperless connection settings, API tokens, delivery jobs, or connector schema. External delivery is deferred. LLM OCR has a separate implemented provider port and local fixture coverage; real provider, quality, resource and deployment acceptance remain pending.

## Verified schema and remaining service enforcement

The embedded SQL migrations and initializer live in `internal/storage`. State/document kind is recorded in each database, migrations run transactionally with checksums, newer or altered migrations are rejected, and SQLite lock contention during concurrent startup is retried within a bounded deadline. Private file permissions, WAL, per-connection foreign keys, and IMMEDIATE transactions are enabled.

Composite foreign keys bind pages, artifacts, jobs, and published outputs to both document and owner. Published outputs additionally reference the current revision; the service must remove the old publication and advance the document revision in the same edit transaction. Capture settings and job input snapshots are immutable. A physical device has one lease row and a monotonic fencing epoch.

These are structural checks, not row-level security. HTTP services must still scope every query to the authenticated principal, check expiry and capability, validate exact page-order sets, enforce quota reservations, and implement the job state machine. Database tests cannot establish those runtime properties.

After process/stream barriers and verified manifest file removal, FinishCleanup atomically removes publication pointers, pages, derived metadata, non-cleanup jobs and artifacts, then marks the cleanup job succeeded. The content-free document/cleanup status remains until a separate bounded retirement pass confirms completion retention, job deadlines, receipt deadlines and the absence of work/reservations/leases. Receipt hashes survive status retirement; aged structured request keys have their own clock-fenced retirement contract. See [cleanup worker](cleanup-worker.md) and [request keys](request-keys.md). An active lease prevents deleting its job. Releasing a lease is not proof that hardware or a worker stopped: the adapter must provide that evidence first. The lease row/epoch remains after release.

Idempotency receipts contain request/key hashes, operation, and an optional job ID. Deleting the job nulls the result rather than deleting its receipt, so retries cannot turn a deleted result into a fresh scan. The initial receipt window is 72 hours, separate from 24-hour content retention. Receipts contain no filenames, text, thumbnails, or document payloads. Expired/deleted results return gone; automatic retries of physical scan commands are forbidden even beyond the receipt window.

Configuration recovery must not restore live sessions or authorization attempts. Use a defined logical export of durable identity/configuration records, excluding sessions/attempts; a raw database recovery procedure must clear both before serving requests. This does not apply to normal process restart, where valid server sessions are expected to survive. The executable logical identity export/fresh restore and its limits are documented in [state recovery](state-recovery.md). Full volume-loss, deployment-secret and physical scanner recovery remain unaccepted.

## Executable document transaction boundary

`internal/documents` now implements owned/expiry-filtered record and export metadata
reads, conditional admitted-render claim, cancellation request and export publication.
See [document transactions](document-transactions.md) for principal pinning, lock
order, revision/runner/deadline checks, reservation release and uncertainty read-back.
Document migration 0002 introduced immutable producing-job artifact links.
Admission, authenticated owner-scoped HTTP/stream cancellation, revision edits,
delete/TTL cleanup and bounded startup metadata maintenance are now connected
and covered by local API/worker/browser fixtures. Unknown native acquisition,
full volume-loss recovery and production retention acceptance remain pending;
local metadata recovery is not physical stop evidence.


Document migration 0006 adds nullable, paired `pages.pixel_width`/`pixel_height` with positive bounded integer and immutability constraints. The scan executor forwards actual dimensions from validated image output; CommitScanPage stores them in the same publication transaction. Existing pages retain NULL instead of inferring dimensions from paper/DPI. A trusted legacy caller may publish with both dimensions unknown, but new OCR HTTP admission refuses such pages with 422. Plain output remains possible, and actual worker decoding still enforces execution limits. Tests bind real PNG validation to stored dimensions, reject incomplete/changed metadata, and check model-bound admission rollback and replay. No image decoding or file I/O is added to HTTP admission transactions.


State migration 0004 adds local credential records and extends the immutable principal kind constraint via an atomic, foreign-key-enabled rebuild of the three dependent identity/guest/cleanup tables. Version-3 upgrade preservation and rollback on failure are covered. The connected service, provisioning commands and password HTTP/session integration are documented in [local authentication](simple-authentication.md). Existing guest and OIDC identities are not relinked by username.


Logical identity recovery is now executable through [state export/fresh restore](state-recovery.md). It retains stable OIDC/simple identities and disabled flags, excludes all sessions/protocol/document state, and never overwrites a live database. This does not restore deployment secrets, pending physical work or full volume-loss state.


## Portable document edit contract

`internal/testsupport/documentcontract.RunEdits` accepts a fresh fixture exposing
`documenthttp.Records`, identities and backend clock/disable controls. The shared
assertions use no SQL handles or SQL fixture writes. SQLite supplies its own seed
and controls in `internal/documents/backend_contract_test.go`.

The suite checks owner isolation across reads/history/edits, exact historical
replay after a later edit, changed-intent rejection, stale revisions, concurrent
revision CAS, creation-based expiry, and revoked reads/new edits/replays after
expiry or principal disable. Concurrent edits use `Fixture.OpenPeerRecords` to
open independent backend connections to the same authority. It is test support and is not part of the scanner
command dependency graph.

This is one reusable subset of the backend acceptance contract. It does not
separate the remaining service/SQL implementation, implement another production
backend, or prove worker claim/fencing, cancellation races, uncertain commits,
file durability, query counts or migrations. Existing specialized tests remain
required; a future adapter must provide real authority/clock controls rather than
fabricate rejection in a Records wrapper.


## Edit service and atomic adapter

`documents.Editor` validates edit shape and retention before invoking
`EditRecords.ApplyEdit` with an opaque `EditCommand`. The command snapshots page
order and preserves the existing canonical v1 key/request hashes. Its accessor
returns detached request values; a zero command is rejected before SQL access.
`Store.Edit` remains the compatible entry point and routes through this service.

SQLite's `Store.ApplyEdit` retains the existing state-to-documents locking,
owner/expiry/revision/active-work checks, exact replay, output invalidation and
single transaction. Authorization is not inferred from command validation.
Adapters receive the authenticated owner separately and must enforce all these
atomic conditions themselves. Neither CRUD operations nor SQL handles escape.

SQL-free service tests verify invalid requests do not call a backend, a valid
control does, backend errors/context are preserved, request slices are detached,
and the established v1 receipt digest remains stable. The portable edit contract
runs against the same SQLite adapter through the compatibility entry point.
Other document commands still mix policy and SQL, and another production backend
has not been implemented or accepted. This separation does not prove migration,
physical fencing or uncertain-commit behavior of any future backend.


## Cancellation/deletion service and atomic adapters

`documents.Lifecycle` validates cancellation/deletion shape and receipt retention,
then calls `LifecycleRecords.ApplyCancel` or `ApplyDelete` with a prepared opaque
command. The established cancel-v1 byte identity and canonical delete v1 JSON
hash remain unchanged. Zero commands are rejected before SQL access; request and
hash accessors return values. Existing `Store.CancelJob` and `RequestDelete`
remain compatible facades into this service.

The SQLite adapters retain the original transaction bodies and state-to-documents
lock order. They recheck enabled owner, expiry, applicable revision/job state,
receipts and deduplication. They only record intent: deletion revokes content and
requests cleanup/cancellation; cancellation does not release original files,
reservations or physical device leases. Workers still establish stop/publication
outcome before releasing resources. Commit uncertainty is not converted into a
successful command or an automatic retry.

`documentcontract.RunLifecycle` is shared test support using the public Records
port and backend-owned fixture controls, with no SQL handles in the assertions.
It checks foreign-owner rejection, cancellation replay/content retention,
deleted original/old-job invisibility, cleanup deduplication, retained cleanup
replay after content TTL and disabled-owner denial. The fixture uses the actual
revision returned by render admission. Unlike edit replay, a retained delete
receipt can expose content-free cleanup status after content expiry; new delete
intent remains unavailable. Specialized SQLite tests still check actual held
leases/reservations, cancellation races and uncertain commits.

SQL-free service tests cover valid controls/invalid preflight, one backend call,
context/error propagation, command values, zero-command refusal and golden
existing receipt hashes. This does not finish other document policy/SQL separation
or implement/accept another production database. Schema, public API and deployment
configuration are unchanged.


## Render admission policy and atomic adapters

`documents.RenderAdmissionService` prepares normalized detached intents for
`RenderRecords.LookupRender` and commands for `ApplyRender`. Existing
`Store.FindRender`/`AdmitRender` are compatible facades. The opaque intent binds
the existing render-v1 key/request digest; server model version, resolved default
quality and reservation are not added to the client identity. A command also
captures deployment limits by value. Accessors detach language slices and reject
zero values before SQL access.

The SQLite transaction suffixes remain unchanged: lookup retains enabled-owner
and fresh-key/receipt handling; admission reconciles an exact receipt before
checking new-work model/reservation/page/quota conditions, then atomically records
the immutable job snapshot, reservation and receipt. Owner/expiry/revision and
capacity are still checked inside the adapter. Input-shape and deployment-limit
validation remains before admission as in the prior direct Store method. No
provider call or processing transaction is introduced in SQL.

The outer `renderadmission.Service` snapshots caller languages and gives detached
language selections to receipt lookup and OCR resolution. A port mutating its
input cannot silently change the later admitted selection. This is value
isolation after snapshotting, not permission for callers to race writes during
the initial snapshot.

SQL-free service tests cover invalid preflight, context/error propagation,
detached accessors, frozen server decisions, zero-command refusal and golden v1
hashes. A deterministic orchestration regression mutates caller/store/resolver
slices at phase boundaries. `documentcontract.RunRenderAdmission` is a reusable
public contract with an actual SQLite adapter fixture: it checks foreign-owner
rejection, queued admission, normalized default replay despite smaller quota/page
policy or changed reservation, changed-intent rejection, expiry and disable.
The older pre-recorded v1 receipt test remains required. Specialized SQL tests
still prove quota/reservation counts, races and uncertain commits; no alternate
production backend or actual OCR/device acceptance is claimed.


## Readiness-gated scan service and adapter

`documents.ReadyScanService` validates/normalizes public scan shape and policy,
then hands an opaque `ReadyScanCommand` to `ReadyScanRecords.ApplyReadyScan`.
`Store.AdmitReadyScan` remains a compatible facade. The command contains no
selector for the trusted non-readiness path; the SQLite adapter always enters
its readiness-gated transaction. Zero commands fail before SQL access. Trusted
internal `AdmitScan` remains distinct and must not be wired to public HTTP.

The existing transaction suffix is unchanged apart from its following separator
blank line. Exact receipt replay still precedes registered-device policy and live
readiness checks. Fresh work resolves live worker boot/capacity, matching metadata,
physical/handoff identity and idle epoch in that same transaction, commits the
job/reservation/receipt with the consumed epoch, then rechecks authority before
commit. Validation does not create readiness or prove mechanical fencing.

Registry slices are captured in prepared commands and detached by accessors.
Nil remains the legacy unconstrained internal policy; a nonnil empty registry
still permits no new device. The outer `scanadmission.Service` always constructs
a nonnil deployment registry and now gives each records call a separate copy,
so a records adapter cannot modify later requests' registered-device policy.
Request hashes retain the established normalized scan-new/scan-next v1 framing;
server boot/reservation and registry are not added to the client identity.

SQL-free tests cover invalid preflight, server boot rejection, normalized photo
format/concurrency, detached policy, nil/empty semantics, zero-command refusal and
golden first/next-page hashes. Outer-service tests cover caller/backend mutation
across admissions and client authority rejection. Existing SQLite readiness tests
remain responsible for lost/stale authority, one-epoch contention, post-scan
renewal, live capacity and the late authority recheck. They use fixture proofs;
production readiness/handoff and other-backend transaction/fencing acceptance
remain unfinished. Schema, public API and deployment settings are unchanged.


## Explicit precommit cancellation during dispatch

Dispatch query/iteration errors, row-error mapping, scan-slot reads and render
execution-capacity checks use the existing `storageOperationError` classification.
Only a driver's exact `context.Canceled` or `DeadlineExceeded` is preserved as
that cancellation. Independent errors and joined cancellation/storage errors
remain storage failures, even when the caller context is canceled concurrently.
A commit error still follows the separate uncertain-commit path.

This addresses an observed catalog-shutdown failure consistent with dispatch
losing explicit cancellation information. A real SQLite driver wrapper reproduced
four pre-fix failures; after correction, eight injected render/scan cases preserve
the expected classification with zero commit calls and unchanged queued work and
reservations. The original strict catalog-shutdown test also passed 20 repeated
runs. Its original log alone does not uniquely identify the underlying SQL phase.
No broad `errors.Is(context.Canceled)` suppression or shutdown-test relaxation
was introduced. Earlier eSCL invalid-operation/render rollback flakes remain
separate unresolved observations.


## Scan dispatch and a regressed wall clock

A scan claim writes its immutable `started_at_ms` from the backend clock. SQLite
rejects a value earlier than the job's `created_at_ms`; it returns `ErrStorage`
and rolls back the claim. `TestClaimNextScanClockRollbackRetainsAdmission` injects
a 1ms regression and checks that no claimed identity escapes, the job remains
queued and unstarted, and its reservation and held device lease remain intact.
Restoring the clock permits claiming that same admitted job without readmission.
This test does not initiate native work or prove physical recovery.

A previous full-suite failure in the scanjob fixture returned this same public
error, but its underlying SQL error and clock samples were not recorded. Clock
regression is a reproducible candidate, not an established cause of that failure.
The fixture now logs a bounded clock ring and the synthetic job timestamps only
when dispatch fails. It retains real wall-clock use and the original assertion;
no retry, time clamp or expected-error relaxation hides the failure.


## Portable uncertain lifecycle commits

`documentcontract.RunUncertainLifecycle` exercises cancellation and deletion with
backend-level commit faults: rollback before commit, and successful commit with
lost acknowledgement. Both must return `ErrCommitUncertain` with a zero success
projection. An independent Records connection checks the durable cancellation or
original-access revocation outcome. Recovery uses that peer with the same key;
the original connection must then return the identical retained receipt result.
Cancellation must keep the original job identity. No native stop, reservation
release or file removal is inferred from recording these intents.

The SQLite external fixture uses `InjectContractCommitFailure` from a `_test.go`
bridge to the existing private transaction commit seam. It commits or leaves the
real transaction for rollback, then loses acknowledgement. This bridge is absent
from production builds; no production fault option or DB handle enters the shared
assertions. A replacement adapter must supply an equivalent transaction-boundary
fault, rather than fabricate ErrCommitUncertain around a successful public call.
The suite does not simulate a process crash, lost volume, unreadable storage or
uncertain worker publication; their specialized recovery tests remain required.


`RunUncertainEdits` and `RunUncertainRender` extend that same transaction fault
fixture to page edits and render admission. Editing checks the durable revision,
rotation and unchanged TTL from an independent Records connection; same-key
recovery/replay must advance the revision only once. Render admission first
looks up the receipt through an independent service connection: only a committed
transaction may have one. Recovery must preserve that job ID and indicate replay
for a lost acknowledgement, or freshly admit after rollback. Subsequent replay
must keep the same revision and queued render job. No PDF execution, native scan,
publication, process crash or volume-loss guarantee follows from admission tests.
The commit-fault callback lives in the shared base fixture and remains backed by
the existing test-only SQLite transaction seam.


`RunUncertainScan` adds first-page and next-page admission to these portable
uncertainty tests. After rollback, the original document must be unchanged and
no extra document may be visible. After a committed acknowledgement loss, an
independent history projection identifies the durable document/job/revision.
Same-key recovery through another service connection must return that identity
as replay; after rollback it must freshly admit once. Exact replay must preserve
the device epoch, and next-page recovery must preserve existing pages and the
creation-based TTL. The resulting scan job remains queued. These assertions
neither claim the job nor launch/retry a physical capture, and fixture readiness
is not a production writer fence. SQL-specific reservation and lease checks
remain required alongside the shared public contract.


## Portable worker claim uncertainty

`documentcontract.RunUncertainDispatch` exercises the existing scan/render claim
methods through a minimal worker interface. Its backend fixture admits one real
reserved job, with a held device lease for scan, and supplies independent claim
and public reader connections. A rollback or committed acknowledgement loss must
return `ErrCommitUncertain` without an owner or executable Job. The independent
reader distinguishes queued rollback from running committed work. After rollback,
claiming the original job succeeds once. Once running, either connection and both
the original and another boot must receive `ErrNoWork` with zero result, and the
job stays running. No executor or native process is called.

SQLite uses the same test-only transaction fault seam as public command tests.
The shared assertions use no SQL handles and are reusable by a replacement
adapter. Reservation/lease contents, guard ownership, native stop, publication,
process crashes, multiple processes and physical device fencing remain separate
specialized contracts; running metadata alone proves none of those barriers.


## Portable render publication read-back

`documentcontract.RunUncertainRenderPublication` checks a running render job's
publication transaction with rollback and successful commit acknowledgement loss.
An independent worker read-back must report exactly whether the commit occurred;
public output metadata and job completion must agree with it. Another owner's
job/artifact tuple must not report a publication. Read-back from the original
connection must match the peer. The SQLite fixture supplies synthetic artifact
metadata, not a rendered file, and uses the existing test-only commit seam.

This reusable database contract also covers one source-bound thumbnail when
provided by the fixture. It does not test file sync/removal, hidden orphan rows,
execution cancellation or lost storage.
Those remain covered by separate SQLite and renderjob reconciliation tests;
a read-back error must not be interpreted as a negative publication result or
permission to delete output. No recovery republish is introduced by this suite.


`RunUncertainScanPublication` applies that metadata contract to a running
first-page scan. Its trusted stop/idle report and artifact descriptor are
synthetic fixture inputs, not observed hardware evidence. Real commit faults
must return no successful ScanPage. An independent CaptureRecorded result must
agree with job completion, page count/state, original ID/hash/size and unchanged
revision/creation/TTL. Rollback leaves the original scanning document empty;
committed publication exposes exactly one page. Both connections must agree,
and another owner's tuple must report no publication. This does not prove
physical stop, original bytes on disk, thumbnail bytes on disk, reservation/device
lease details or process-crash durability. Specialized scanjob reconciliation
and SQLite resource-fence tests remain required alongside the shared assertions.


The plain and thumbnail publication fixtures run the same uncertainty assertions.
The thumbnail variant uses the existing WithThumbnails transaction methods, with
one derivative bound to the source original. A committed receipt must agree with
a readable thumbnail ID/hash/size from a separate connection. Render rollback
must leave its existing page without a thumbnail; scan rollback must expose no
new page. This public surface cannot count inaccessible orphan artifact rows or
prove disk cleanup; specialized SQLite bundle/resource tests and actual worker
file reconciliation remain separate evidence. The fixture supplies descriptors,
not generated images or physical idle observations.

Scan thumbnail fixtures use the worker's `CaptureBundleRecorded` read-back, not
only the original-artifact boolean. A mismatched committed thumbnail descriptor
must return `ErrConflict`, never a safe negative result authorizing cleanup. The
plain original fixture continues to use `CaptureRecorded`. Render publication
read-back remains the existing atomic export receipt plus public thumbnail
metadata verification.


## Portable stopped-worker uncertainty

`documentcontract.RunUncertainStop` starts with a claimed scan/render job and
covers failure and requested cancellation under rollback/commit acknowledgement
loss. Missing local stop evidence must be rejected. Cancellation alone and a
rejected finish must not yield a positive stopped receipt. Independent stop
read-back must agree with the durable running/cancelling versus failed/cancelled
job state, with existing pages, revision and creation-based TTL preserved.
Another owner's tuple must report no stopped receipt, and both connections must
agree. The SQLite fixture uses the same actual transaction fault seam.

Stop, idle and workspace cleanup timestamps are synthetic inputs to this DB
contract. The suite does not stop native work or remove files. A negative receipt
alone cannot prove that every resource row is intact; existing specialized
SQLite tests inspect lease/reservation preservation and stale-stop rejection.
Worker executor/file reconciliation, process crashes, lost storage and physical
fencing remain separate acceptance requirements.


## Portable clean-reopen persistence

`RunReopenEdits` and `RunReopenLifecycle` use a fixture callback that closes all
backend connections, then recreates the adapter through normal initialization
against the same persisted data and clock. SQLite closes both state/documents
DB handles, opens both files with `storage.Open` (including migration checks),
and constructs a new Store; it does not reseed or reuse the old Store.

The shared assertions cover unchanged current page/revision/TTL projections,
exact historical edit receipts after later edits, changed-intent conflicts,
owner isolation, persisted principal disable, retained cancel/delete receipts,
continued original-access revocation, and reuse of the existing cleanup job.
These are clean connection-reopen tests in one process. They do not prove old
schema upgrades, downgrade safety, crash/WAL recovery, power loss, missing
volumes, physical fencing or another backend's migration semantics. Existing
backend-specific migration/upgrade tests remain separate evidence.


`RunReopenRenderAdmission` and `RunReopenScanAdmission` rebuild both the public
reader and admission service after closing all fixture DB handles. Render receipt
lookup and exact replay must retain the original queued job/revision. First-page
and next-page scan replay must retain document/job/revision/epoch and unchanged
pages/TTL even after readiness withdrawal before closing the connections. Job
owner isolation remains enforced. Fresh scan requests stay blocked after reopen;
that assertion also involves the existing held lease and is not independent
proof of the absence of readiness rows. No worker boot change, process restart,
physical fence or native capture is performed by these adapter reopen tests.

## Clock regression after a scan claim

`LoadScanWork` also rejects a backend time earlier than immutable
`started_at_ms` with `ErrInvalid`. This occurs before returning executable work;
`PulseScan` uses the same authorization read. The deterministic
`TestScanWorkRejectsLostAuthority/clock-rollback` now covers this boundary and
confirms that rejected load/pulse calls retain the device lease and reservation.
A 2026-11-03 runtime fixture failure showed its observed current timestamp 11ms
before the recorded start. A fresh WSL observation recorded four backwards wall
steps over two minutes. This supports the temporal rejection diagnosis without
attributing the actual clock writer or loosening stop/idle/TTL checks.

A separate network-isolated run of the same production source passed the full
race suite, and the updated onefile passed synthetic OCR-worker/PDF/served-asset
regressions. Its concurrent clock observer nevertheless recorded 24 backwards
wall steps. Passing tests are therefore not a stable-clock acceptance. Raw WSL
failures and the collector's failed clock assertion remain retained under the
parent task `stateless-full-regression-20261103/`. No clock service was changed.

## PDF page sources (documents migration 0017)

The page table now distinguishes `scan` and `pdf` sources. Scan rows retain the
existing capture tuple and optional known pixel dimensions. PDF rows have no
capture tuple or raster dimensions and carry positive bounded page dimensions
in points. The source identity and geometry are immutable; rotation and ordinal
remain editable. PDF originals require an application/pdf artifact and document
purpose. Imported PDF bytes must still pass the bounded preparation worker before
any future publication; these constraints do not parse or validate PDF content.

Migration 0017 replaces only the leaf pages table, copies existing rows as scans,
and reinstalls source/owner/original/ordinal/photo constraints. No current foreign
key points to pages. Jobs, reservations, device leases, request receipts and
artifact rows are not dropped or rebuilt. Checksummed prior migrations remain
unchanged. Upgrade testing retains existing scan settings and that complete job
graph and checks foreign-key integrity.

The public projection includes `source_type`; `capture` is null for PDF pages and
`pdf` supplies width/height in points. Existing scan projections keep their capture
object. Render snapshots retain the old serialized scan representation (new fields
omitted) and explicitly identify PDF sources and geometry. OCR admission counts
only scanned images against image page/pixel limits; imported text is preserved.

Older executables reject schema 0017 as newer than their known migrations. There
is no automatic down migration: rollback requires the compatible pre-upgrade
database/document backup and the old executable together. Source-model support
does not yet provide PDF upload, safe preview or a bounded parsing-child runtime.
