# Physical scan admission and claim

`documents.Store.AdmitScan` implements database admission for one flatbed capture.
It sends no device request. Capability probing, ready-worker coordination, native
execution, completed-page validation and physical stop/idle evidence remain with
the unfinished scanner/worker coordinator.

## First scan and next page

The first command creates a new opaque document only after principal, capacity,
document count and device checks pass. The transaction records initial creation
and expiry, purpose, capture settings/photo format, queued scan job, byte reservation,
physical lease epoch and hashed idempotency receipt. It creates only a temporary
owner mirror of an existing enabled durable principal. No document is created by
opening Home, a failed request or a device-busy result.

A subsequent explicit command checks owner, expiry, expected revision, compatible
state and absence of active work. It preserves every existing page and the original
creation/expiry, advances document revision and invalidates the old publication.
A document can append pages within its limit. A photo with a captured page cannot
append; an empty failed photo may be retried explicitly, preserving photo-format
intent. Each scan snapshot freezes this page's paper size/color/DPI independently.
1200dpi is rejected; actual advertised/resource-approved capability intersection
still belongs to the coordinator before admission.

The same principal/key/canonical command returns the original document/job/epoch,
regardless of current worker boot or reservation policy. A changed command conflicts;
a gone/expired result never creates another capture. Replay never reacquires a
lease, increments its epoch, releases another job's lease, or extends content TTL.
Object keys are canonicalized for stable hashes across struct-field refactoring;
integer precision and array ordering are preserved. Raw keys are never stored.

## Device and ready-worker fencing

Admission requires a pre-existing idle lease with no job. Missing, held and
reconciling devices are unavailable. Browser requests cannot create an idle row.
A trusted reconciler must establish that row only after exclusive device ownership
has been handed off and hardware activity is confirmed stopped/idle. In particular,
an eSCL idle response alone does not fence the old scanservjs writer. This reconciler
and its production handoff are not implemented yet.

The coordinator supplies the ready worker's fresh boot nonce as internal metadata,
never as a browser field or a stable environment setting. Admission pins it in
both the immutable scan snapshot and job runner identity. The worker/boot nonce
readiness source still needs implementation at executable startup.

A worker boot can reserve bounded scan slots across different devices using
trusted `ScanLimits.MaxConcurrentScans` (1–32; zero retains one). Queued and
running scans consume slots. Each device lease admits only one job, and each
document still permits only one active mutation. Slot sharing requires healthy,
exact-current-boot scan work with intact reservation/start/deadline/lease fences.
Foreign/unknown/expired/cancelling/reconciling work and terminal held leases block
new work conservatively. Running render work still blocks scans; independent
processing/resource scheduling is unfinished. Ordinary unassigned queued render
work may wait while a capture is accepted first.

`ClaimScanWithLimit` and `ClaimNextScanWithLimit` enforce the trusted slot cap;
legacy entry points use one. Claim excludes only its own reserved slot and never
queues/retries acquisition. Both admission and claim recheck peer deadlines after
capacity probes. A mismatched smaller claim policy refuses work rather than
exceeding its limit. The daemon uses its effective worker-only SCANNER_SCAN_CONCURRENCY policy for
bounded scan claims; the future HTTP admission coordinator must read this same
capacity from live worker authority. Initial deployment still
uses one replica and one shared coordinator guard.

`ClaimScan` requires the same boot nonce, snapshot version, current document revision,
owner, deadline, reservation, held device ID/epoch and free worker slot. A new worker
boot cannot execute an old queued physical request, even if a generic recovery
routine overwrites its runner column. Startup must reconcile and interrupt old
work, never automatically rescan it. Claim is not physical execution or evidence
that the scanner is idle. An epoch cannot mechanically fence an already-running
device; proven process/hardware reconciliation remains necessary.

## Capacity and completion boundary

Scan and render share the same logical capacity calculation. New documents also
count against the principal document limit, including metadata awaiting cleanup.
Completed cleanup tombstones no longer occupy this limit: admission shares the
completion-reconciliation predicate and requires succeeded cleanup, withdrawn
state and absence of artifacts/pages/manifests/other jobs/reservations/leases.
A status flag, expiry or file-progress acknowledgement alone is insufficient.
The tombstone and delete receipt remain available independently of the slot;
receipt replay still precedes quota checking. Migration 0013 indexes artifact
lookups by document so each completed-record check avoids a full artifact scan.
The scan deadline is clamped to document expiry; receipt retention covers remaining
content lifetime. Failure before commit rolls back document, revision, job,
reservation, receipt and lease changes together. Uncertain commit is resolved by
repeating the same command/key, which reads back its original receipt.

Physical free-space admission now has a required probe; see [disk capacity](disk-capacity.md).
Execution-time capacity checks, registered native resolution and monitored
publication/stop orchestration now exist. Still pending are the physical readiness
evidence producer, complete startup recovery, TTL GC and authenticated scan HTTP/UI.
Nothing here marks cancellation, expiry or a dead heartbeat as proof of termination.
No tests in this component drive Canon hardware or stop the existing scanner service.

## Evidence

Race tests use independent SQLite connections for two-user/device contention and
scan-versus-render worker contention. They cover busy/unknown/reconciling devices,
owner and revision, document/page/photo constraints, disabled principals, capacity,
1200dpi rejection, immutable capture/photo intent, exact boot/epoch/deadline claim,
commit-then-error replay, expired receipts and replay while another job holds the
device. Fixture-only SQL simulates reconciliation; it is not physical stop evidence.

Record-side successful-page publication and conditional lease/reservation cleanup
now exist; see [scan completion](scan-completion.md). Their trusted stop report does
not replace the unfinished native process/device evidence producer.

The fresh nonce/local coordinator lock is now implemented in
[worker identity](worker-identity.md), with actual process-crash and DB claim-fencing
integration tests. Executable startup wiring, durable readiness and physical
reconciliation remain pending; acquiring that lock is never an idle proof.

## Claimed work and cancellation monitoring

`LoadScanWork` exposes worker-internal metadata only for an enabled owner's
current running scan, pinned to its exact worker boot and held lease generation.
It validates the strict snapshot version, capture settings, document/photo output
encoding, reservation, start and deadlines. It returns zero work on transaction
commit uncertainty. It never claims, retries or contacts the device.

`PulseScan` applies the same predicates under the principal/document transaction
lock order and renews only the heartbeat. `workwatch.Run` can use this pulse to
cancel and join running acquisition after persisted cancellation or authorization
loss. Neither pulse failure nor joined local work proves the scanner is idle;
the device lease and storage reservation remain held until the separate stop,
fresh physical-idle and file cleanup/publication contracts succeed.

Tests cover cancellation propagation, disabled/foreign owners, wrong boot,
expired work, changed revision, lease state/epoch mismatch, absent reservation,
corrupt snapshots, photo JPEG/PNG settings and commit uncertainty. Corruption
fixtures explicitly remove the immutable-snapshot trigger; ordinary writes remain
trigger-protected. Native capture dispatch and real hardware observation are
still pending.

The user now requires concurrent captures on different scanners. The database and daemon now support bounded per-device scan slots, while
readiness/HTTP admission and independent render scheduling remain unfinished; see
[multiple scanners](multiple-scanners.md). Driver concurrency alone does not
change admission or establish readiness.

The transactional readiness boundary is now implemented separately from its
unfinished physical evidence producer. See [scanner readiness](scanner-readiness.md)
for PublishScannerReadiness and AdmitReadyScan. No runtime/HTTP wiring or physical
handoff is implied; the low-level AdmitScan must not become the public HTTP entry.

Authenticated new/next scan routes now call AdmitReadyScan through the
scanadmission records port; see [scan HTTP](scan-http.md). Revision mismatch now
returns ErrRevision/HTTP 412, while exact receipt replay precedes revision and
registered-device/readiness checks. Physical readiness production remains pending.
