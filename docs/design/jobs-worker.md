# Job admission, transitions and recovery

Status: executable DB constraints and OCR port exist; application coordinator/worker is not implemented yet.

## Admission transaction

1. Authenticate the principal and check that it is enabled. Guest mode uses an independent principal.
2. Validate the registered scanner/provider, requested capability, body bounds, current document revision and creation-based expiry.
3. Start an IMMEDIATE transaction in the temporary store. Check the scoped request-key receipt and request digest before creating work. Return the original result or gone; different input with the same key conflicts.
4. Check document/page counts, byte reservations, global free space and existing document work. For a physical scan, claim only an idle/reconciled device lease with a new epoch. Busy means a manual retry, not a reservation queue that later scans someone else’s paper.
5. Commit the job, immutable configuration, resource reservation, lease and receipt together. Return acceptance only after this commit. A browser timeout does not imply failure or permission to rescan.

The first scan creates the document and its immutable expiry. Subsequent scans use the same document ID and a new job. Independent documents may exist, while physical scanning remains serialized per device. Editing during an incompatible active operation returns conflict.

## Job state transitions

| Current | Event/evidence | Next | Required action |
|---|---|---|---|
| queued | worker wins conditional claim | running | Set runner boot ID, heartbeat and owned process identity; recheck expiry/principal/revision |
| queued | cancel before execution | cancelled | No child was launched; release reservation/lease conditionally |
| running | validated complete artifact, current revision, not expired | succeeded | Atomically publish metadata/output and release owned resources |
| running | error with confirmed child exit | failed | Keep originals; remove staging output; release lease only after device reconciliation |
| queued/running | cancel, expiry or deletion | cancelling | Persist request and stop new publication; signal owned work |
| cancelling | process group reaped, handles closed, device reconciled | cancelled | Clean temporary output; preserve or remove originals according to deletion reason |
| running/cancelling | worker lost or startup finds uncertain work | reconciling | Hold the device lease, stop/reconcile old work; never relaunch a physical scan |
| reconciling | work confirmed stopped, captured data incomplete | interrupted | Keep validated completed pages; require explicit user retry |
| terminal | explicit new user request | new job | Retain original expiry and record a new request key/revision as appropriate |

A heartbeat timeout alone is not proof of exit. PID reuse is handled with runner boot identity and process start identity; do not kill an unrelated reused PID. A lease epoch protects DB updates but cannot physically fence an external scanner. Unknown device activity remains unavailable until reconciled. Rolling updates and node failover must not create two scanner writers.

## Processing and publication

Resolve internal input IDs into read-only job inputs. Generate thumbnails/working images in an isolated workspace; do not modify captured originals. Work page by page where possible. Heavy PDF/OCR operations run separately from the HTTP request process.

A child receives only the environment it needs (PATH/locale/temp settings and its selected plugin credential). Do not inherit OIDC/session secrets into an OCR/PDF subprocess. No shell command is built from user strings. Program paths and protocol endpoints are administrator configuration, and each argument is constructed from validated types.

Output starts as an unpublished artifact under the job root. Close, sync and validate it, then atomically rename it. The final DB transaction checks owner, enabled principal, expected document revision, deadline/expiry and cancellation before creating the publication pointer. On any mismatch, cleanup the unreferenced output; do not replace a newer export. A startup reconciler removes abandoned stage files and rechecks expiry before accepting work.

## Stop, cleanup and downloads

On cancellation, signal the owned process group, wait a bounded grace period, escalate if necessary, reap, close all readers/writers, then reconcile the device. Native context cancellation and killing only the immediate child are insufficient evidence by themselves. Required tests include a grandchild that holds a pipe open, a stalled child, an interrupted scanner request, and a late completed result.

Delete/expiry first deny new file references and cancel active download streams as well as jobs. A cleanup job can outlive content expiry, but only a minimal owned cleanup status may be shown; it cannot expose document contents. Physical cleanup target is five minutes, with persistent retries on failure.

After confirmed resource release, remove publication pointers/pages/artifacts/jobs/document metadata in a transaction. Keep content-free request receipts within their documented 72-hour window; null result means gone. Never restart a scan from a receipt that no longer points to a job.

## Browser behavior

Active screens initially poll the owned job every two seconds with backoff on transport failure. Returning to the tab refetches session/document/job projections. Cancelling a fetch does not cancel the job. No scan command is automatically replayed after reload, lock, reconnect or uncertain response. Theme/locale may be local preferences; document/job data is not browser persistence.

## Local Linux process supervisor evidence (2026-11-02)

`internal/processrun` now starts trusted executables in their own process group,
with explicit environment and file-backed output. Cancellation sends TERM, waits
for the configured grace, escalates to KILL and waits for the direct child. Normal
parent exit also triggers descendant cleanup. Only ESRCH marks the group stopped;
a surviving group (including an orphan zombie awaiting init) requires reconciliation.
Race-enabled tests cover an uncooperative child, normal parent exit with a surviving
child inheriting output, pre-launch cancellation, and environment-secret isolation.

This is a worker building block, not durable cancellation or hardware acceptance.
The coordinator still needs persisted cancellation, publication fencing, restart
identity checks and scanner reconciliation. A tool that deliberately escapes the
process group is outside this helper's containment: deployment cgroup termination
and the required init process remain part of the worker boundary. Output quota is
also a caller/container responsibility; this helper never captures output in RAM.

The concrete OCR artifact access boundary is now implemented in
`internal/artifactscope`; see [artifact scope](artifact-scope.md) for the handoff,
limits, seal/release/abort lifecycle and unresolved coordinator requirements.
A DB commit with uncertain outcome must be reconciled before removing staged files.

The record-side render claim/publication and cancellation request are now executable
in `internal/documents`; [transaction predicates](document-transactions.md) define
what is checked and what remains with admission, coordinator and HTTP. A successful
cancel request is still only persisted intent, not process termination.

Existing-document output admission is now implemented in `internal/documents`: see
[render admission](render-admission.md) for canonical receipts, revision advance,
immutable snapshots and logical quota reservation. Physical scan admission and
filesystem free-space gates remain separate unfinished work.

First/next scan admission, document limits and boot/epoch-bound claim now exist in
`internal/documents`; see [scan admission](scan-admission.md). They require trusted
ready-worker/idle initialization, and never replace stop/physical-idle evidence.

`CommitScanPage` and worker-internal `FinishStoppedScan` now implement the database
side of page publication and confirmed-stop resource release. See
[scan completion](scan-completion.md) for their report provenance and remaining
native-coordinator requirements. Heartbeats never overwrite immutable start time.
