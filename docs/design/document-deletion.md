# Document deletion and expiry

DELETE /api/documents/{document_id} now records deletion intent. The authenticated
owner supplies an empty body, one opaque Idempotency-Key and the exact current
quoted If-Match revision. Foreign Origin, duplicate headers, query parameters,
weak/noncanonical revisions, foreign owners and disabled owners are rejected.

In the same state→documents guarded transaction, RequestDelete marks the document
deleting, changes queued/running non-cleanup jobs to cancelling and creates one
queued cleanup job. Reconciling/cancelling jobs remain unresolved. It does not
change document identity, revision or creation-based TTL, remove pages/artifacts
or published-output records, release reservations/device leases, stop native work,
or claim a device is idle. Existing worker authorization pulses/publication checks
observe the deletion state; content readers and active streams use the same guard.

The response is minimal cleanup Job metadata plus Location. Migration 0011 enforces
one cleanup identity per document, including terminal jobs and eventual recovery.
Exact receipt replay resolves its latest status, including after the content TTL,
until receipt expiry. A different key for the same retained deleting document uses
the same cleanup job. Conflicting key contents fail; gone receipts never enqueue
new work. After an uncertain commit, resubmitting the same intent resolves the
receipt rather than issuing another job or acquisition. ReadJob permits only
minimal owned cleanup status after content withdrawal. The client status view
withholds Cancel and document Open for a cleanup job.

QueueExpiredCleanup is a worker-internal bounded discovery operation. It selects
expired documents without an existing cleanup identity, including disabled owners,
marks them expired, requests cancellation and creates the same cleanup job shape.
It follows state→documents lock order and never unlinks content or releases native
resources. Repeated sweeps discover no duplicate work. The startup and idle worker loops now use it for bounded discovery, followed by
never-started render recovery and eligible cleanup dispatch; see [cleanup worker](cleanup-worker.md).

Tests cover owner/revision/expiry/disabled checks, lost commit acknowledgements,
transaction rollback and exact-key/different-key/concurrent replay. Deletion hides
history/detail/original/thumbnail/current output while retaining files/records.
An actively claimed scan remains cancelling with its held lease and reservation;
late publication fails. Expiry discovery is bounded, handles disabled owners and
preserves reservations. HTTP tests exercise strict headers/body/Origin and owned
cleanup status. Browser coverage issues real deletion and verifies that status
cannot propose a cleanup cancellation or reopen content.

Pending: native stop reconciliation/started scan recovery, legacy/orphan directory
sweeping, safe receipt-hash retirement and actual production filesystem/resource
acceptance. Current manifest directory and completed-status retirement are described
below. Normal eligible cleanup now collects
files through the [artifact GC](artifact-gc.md) adapter. No production content or
device acquisition is used by the synthetic-store tests.

## Cleanup claim and immutable manifest

ClaimCleanup/LoadCleanupWork now provide a worker-only preparation boundary. A
known queued cleanup job may be claimed under the local worker guard even for a
disabled owner. The target must already be deleting/expired. Any active non-cleanup
job, any reservation (including terminal-job reservations) or any linked device
lease blocks the claim. Staging/unhashed artifacts also require reconciliation.
The claim does not infer physical stop from process exit or a clock timeout.

Migration 0012 freezes every artifact path, size and SHA256 into cleanup_files in
the same transaction that marks artifacts deleting and starts the cleanup job.
The manifest is owner/job bound and immutable; records start pending and cannot
be inserted as already deleted. Files and their quota charges remain intact.
After an uncertain claim commit, the caller loads the same known job and runner;
no file operation follows an unconfirmed claim. Loads recheck the producer barrier,
complete artifact/manifest correspondence, runner, target state and deadline.
Pending files are returned in batches of at most 100, with a remaining count.
This is private worker metadata, never an HTTP response.

AbortUnstartedRenders can terminalize withdrawn render jobs only when start,
runner and heartbeat are all null and neither produced-artifact links nor device
leases exist. This immutable pre-claim evidence makes releasing their reservations
safe; claim and abort serialize transactionally. Started/reconciling work stays
blocked. The existing scan-specific AbortUnstartedScan has a separate pinned-boot
and lease-epoch contract and has not been bypassed or replaced by this helper.

Tests cover disabled owners, bounded source-bound manifests, duplicate/foreign
claims, lost commit acknowledgements with committed/rolled-back read-back,
producer reservations/leases/staging state, late manifest/artifact mismatches,
deadlines, manifest identity constraints and never-started versus claimed renders.
The [artifact GC](artifact-gc.md) adapter and [cleanup coordinator](cleanup-worker.md)
now connect physical removal, per-file acknowledgement, terminal quota release
and ordinary worker dispatch. Interrupted/new-boot cleanup and expired attempt
recovery are now explicit fenced operations described in the cleanup worker design.
Native recovery is not inferred from lease or clock expiry.


## Browser controls

Document detail and each history row offer Delete document / 書類を削除. One
confirmation explains removal of all pages, cancellation intent and downloaded
copies remaining on the user's device. Cancel has initial focus, Escape/backdrop
closes before submission, and pending requests disable dismissal/duplicate submits.
Destructive controls are separated from the main review/save action. The standalone
presentation module retains only request state and one opaque command key; the
server remains authoritative for document, cancellation and cleanup state.

The command sends DELETE with an empty body, same-origin credentials, a stable
Idempotency-Key and exact projected revision. Definite revision conflict offers
reload with focus moved to that action. Timeout, server failure and invalid success
metadata show uncertainty and permit checking with the same key/revision. Closing
and reopening within the same rendered document retains that key. No automatic
mutation occurs on reload/reconnect. Navigation/session/language changes dispose
and abort the request, and a late result cannot redirect or update a new view.
No document/request state is persisted into browser storage. If a response never
arrives and the user leaves, a subsequent content read may simply find the document
unavailable; no unacknowledged deletion job ID is invented or persisted.

Accepted responses must identify a cleanup job for the same document. The view
clears previews and navigates to the existing owned job URL, which survives reload.
Deletion status shows pending removal until the worker completes; only succeeded
is labelled Deleted. It offers neither Cancel nor Open/download. It explains that
copies downloaded earlier remain on the device. History/content refusal is immediate
at delete admission, independently of when pending physical cleanup can finish.
Unresolved native resources can keep deletion pending and are not falsely marked
stopped by this UI.

Browser tests use the real API/worker and isolated private stores, including actual
file absence checked after successful cleanup. They cover confirmation cancel and
focus, 320/390/768/1440 widths, Japanese/English display, pending Escape refusal,
real acceptance with a deliberately lost response followed by same-key recovery,
real intervening page edit/revision conflict and reload, job reload, and navigation
while an accepted response is delayed. This is headless Chromium under fixture TLS;
actual mobile/screen reader and production storage acceptance remain outstanding.


## Completed status retention

Schema 15 adds indexed, bounded retirement of content-free completed cleanup
status. PurgeCompletedCleanup runs under the exclusive worker guard in startup
and idle metadata maintenance (100 candidates per batch; full startup batches
continue). It requires the same completed-cleanup predicate used by document-slot
reconciliation, including no artifacts/pages/manifests/attempts/edit projections,
other processing jobs, reservations or linked device leases. A success flag alone
is insufficient. Completion must have a runner, immutable start and sane heartbeat.

A status can retire only when the original immutable job deadline has passed,
all result-linked request receipts have expired, and a full
SCANNER_RECEIPT_RETENTION_SECONDS (default 72 hours) has passed since the completion
heartbeat. This reuses the configured retention period, without extending document
TTL or receipt expiry. Late physical recovery therefore still leaves a full status
window. Invalid/disabled owners do not need a browser session for internal cleanup.
Deleting the completed document cascades its job; expired receipts retain their
hashes, operation and deadline, with result_job_id set NULL by the FK. No payload
files are touched by status retirement. Missing/retired status uses the existing
owned 404 response, and expired exact receipt replay remains gone.

The receipt hashes deliberately remain. With arbitrary opaque client keys, deleting
an expired receipt would make an old physical scan key indistinguishable from an
unseen command and permit duplicate acquisition. Expired/deleted commands remain
non-reexecutable even after the receipt window under the current admission contract.
Bounding these minimal replay tombstones requires an age/fence-validatable command
key protocol (and legacy-key migration), not a simple expiry DELETE. That protocol
and receipt-hash/owner retirement are still pending; status retirement is not claimed
as complete receipt storage reclamation.

The SQLite adapter uses the dedicated cleanup-retirement partial index explicitly,
with indexed result-receipt expiry and edit-result document lookups. The actual
EXPLAIN plan is tested. One transaction rechecks/selects a bounded batch and retires
it; commit uncertainty remains an error with no automatic write retry. Tests cover
all exact retention boundaries, future linked receipts, incomplete/inconsistent
state, disabled owners, real scan-key replay after status purge, batch bounds,
committed/rolled-back lost acknowledgements and ordinary compiled-worker startup.
