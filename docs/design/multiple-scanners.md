# Multiple scanners and bounded execution

User requirement, 2026-11-02: independently registered scanners must support
simultaneous captures. This supersedes the initial single-job worker design.
Single deployment replica and one coordinator boot do not imply one active device.
The implementation is incomplete. Driver operations, database scan capacity and
the worker daemon now support bounded concurrent capture on different devices.
Readiness repository contracts, authenticated scan admission and explicit scanner
selection UI are also implemented. Physical readiness production/handoff, independent
render scheduling and real-device/concurrent-resource acceptance remain unfinished.

## Ownership and scheduling contract

Each physical scanner has a stable registered ID, independent capability tuples,
readiness/physical handoff evidence, lease epoch, current job and native gate.
A user explicitly chooses the device. Busy/offline/unsupported devices stay visible
with their state; the application must not silently route a retry to another device.
One configured scanner needs no extra selection step. Persisted document/page
settings and server job state remain authoritative across tabs and reconnects.

One scanner may execute at most one native operation at a time, including metadata
inspection and idle observation. Different scanners may acquire concurrently up
to a bounded deployment scan limit. Capture capacity and OCR/PDF concurrency are
separate budgets. Device ownership is released only after proven local termination,
fresh device idle and publication/cleanup; it is not held while waiting for the next
original or running a later export. Scan slots reserve execution/storage capacity
at admission. Reservations across all active jobs must fit user/global/disk limits;
a per-job limit multiplied by the number of scanners is not a global memory limit.

Coordinator boot/generation and lease epochs fence claims transactionally. The
admission transaction must validate current readiness, the exact registered tuple,
worker boot, device lease and aggregate scan capacity together. Same-device
contention accepts one job; different-device admissions can accept both. Same
document operations remain revision/ownership serialized even across devices.
Idempotent replay returns its original job without taking another device or slot.

The scheduler needs independent capture tasks keyed by device plus a separately
bounded render queue, while heartbeat/publication remains independent. Failures
cancel and join owned tasks before releasing the shared guard/configuration.
Boot recovery must identify all active jobs, native outputs and affected devices;
never retry a physical acquisition automatically. An unresolved operation retains
its device lease/reservation. Current runtime still conservatively stops the whole
worker on reconciliation failure; allowing healthy-device continuity requires its
own recovery/fault-domain implementation and evidence, not just a smaller lock.

## Implemented boundary

`scannerdriver.Driver` now has one native gate and retained inspection/unknown-stop
state per registered device. Resolve, capture, metadata and idle operations share
that device's gate. The job-use fence remains global and mutex protected: changing
device cannot re-execute the same job. Close refuses new work across all devices
before joining them, attempts cleanup for every device even after one fails, and
preserves unresolved stop/cleanup evidence. Configuration ownership remains with
the guarded Session until successful joined close.

Configuration rejects duplicate known endpoint identities even with different
registered IDs (host case/trailing dot, default ports, equivalent IP spellings,
URL escaping and trailing slashes). This is syntactic deduplication only. DNS
aliases, reverse proxies and HTTP/HTTPS can expose one physical scanner under
multiple endpoints. Deployment must register each physical device once; future
readiness/handoff must compare physical identity where available. No discovery,
network request or old-writer shutdown is performed by this check.

Race tests prove two blocked native captures on separate devices enter before
either is released; same-device metadata/status waits and cancellation invokes no
native operation; job replay across devices is refused; one unresolved device
retains its evidence without blocking a healthy driver's operation; Close drains
in-flight capture and closes all devices. These fixtures do not prove concurrent
HTTP admission, scheduler execution, hardware throughput or memory bounds.

## Database scan capacity

`ScanLimits.MaxConcurrentScans` is trusted internal admission policy (1–32;
zero retains the old one-slot default). `ClaimScanWithLimit` and
`ClaimNextScanWithLimit` use the same policy under principal/document transactions;
the existing claim entry points retain one slot. The browser does not control this policy. The daemon now uses the explicit
worker-only environment policy described below; future HTTP admission must read
the same live authority/capacity instead of trusting a browser or a stale setting.

Queued scans reserve slots immediately. Other healthy queued/running scans may
share slots only with the same boot, exact version/boot/device/epoch snapshot,
held lease, reservation, unexpired deadline and consistent start marker. A
foreign/missing runner, unknown snapshot, cancellation/reconciliation, missing
reservation, expired operation, mismatched lease or terminal held lease remains a
worker-wide recovery barrier. Running render work also remains a barrier until
separate processing/resource scheduling is implemented. Ordinary unassigned queued
renders do not consume a scan slot. Sharing a slot does not establish physical
idle or fence any legacy writer.

Admission counts all reserved scan slots and claims exclude only their own
already reserved slot. Device acquisition still uses conditional epoch updates;
same-document revision/state/active-work checks remain unchanged. Exact replay
precedes slot checks and creates no extra job, lease or reservation. Shared
logical and physical byte checks apply across devices. Both admission and claim
recheck peer deadlines after their capacity probe so a peer that becomes
unresolved during that probe prevents commit. Uncertain claim commit returns no
owner/job authority, even if the DB committed; it never triggers capture or retry.

Independent SQLite connections test simultaneous admissions at limits 1/2/3,
concurrent claims of two devices exactly once, same-device exclusion, full-slot
replay, no cross-device replay, one-slot policy mismatch refusal, unknown/foreign
work, aggregate storage, invalid bounds, expiry during capacity probes and
committed-but-uncertain claims. Fixtures seed trusted idle leases and do not prove
production readiness, admission HTTP, runtime parallel execution or hardware.

## Daemon integration

Worker-only `SCANNER_SCAN_CONCURRENCY` accepts 1–32, default 1. Its effective cap
is the minimum of that value, registered devices and
`floor(SCANNER_GLOBAL_MAX_BYTES / SCANNER_JOB_MAX_BYTES)`. No-scanner rendering
retains a one-slot default without invoking a scanner. This is a conservative
logical reservation cap, not evidence of bounded RSS; actual concurrent memory,
CPU, native-process and disk acceptance is still required before deployment.
Compose and Helm pass the variable only to the worker; API config does not read it.

The daemon serially claims each exact-boot scan, then runs its complete monitored
executor as a bounded task. Task count is held until stop/idle/publication or
failure handling returns. Completions wake the loop immediately. Tasks on separate
devices overlap; a device's driver gate and durable lease still serialize native
work. Render runs only when no scan task is active and its existing DB dispatch
predicates pass. OCR/PDF processing concurrency remains one and must not be
presented as an independently concurrent render queue yet. Metadata heartbeat
continues independently. A four-worker inspection pool and single merge publisher
now publish completed per-device settings without waiting for slow batches; old
device observations are dropped independently before shared snapshot expiry.
Devices can still queue behind four occupied inspection slots.

Task failures cancel peer scan tasks and prevent additional claims; claim errors
never start a task with unproven authority. Shutdown cancels and joins every task
before publisher, native Session, stores and coordinator guard close. Errors found
while joining are returned alongside cancellation; a clean exact cancellation is
not allowed to hide reconciliation/uncertain-commit errors. `--once` still handles
at most one job total and waits for its executor without cancelling a healthy job.

Real daemon tests use two separate registered synthetic devices, one process per
capture, isolated configuration, a local eSCL fixture and fixture-created idle
leases. Both capture markers must appear before either process is allowed to
finish. Success publishes one page per document; global cancellation and durable
coordinator authority loss join both processes without publication/replay, release
only proven stopped/idle resources, and retain the guard until shutdown. Task-level
tests cover a failure discovered during join and cancellation of a peer without
losing the failure cause. These are not real physical handoff/idle/throughput proofs.

## Remaining integration and acceptance

- Connect the implemented scan-slot policy to the runtime and future admission
  coordinator, preserve foreign-boot/start/reservation/commit-uncertainty fences,
  and integrate independent render scheduling/resource bounds.
- Publish effective scan capacity with live boot/generation readiness so future
  HTTP admission uses the same cap as daemon claim. Extend independent render
  scheduling only with aggregate runtime memory/resource enforcement.
- Accept real-device metadata refresh cost/fairness under capture, including the
  bounded four-worker queue and retained unresolved native evidence.
- Publish/read readiness and statuses per device; connect explicit scanner choice,
  capability-dependent settings and authenticated scan HTTP commands.
- Verify two-user/two-device real API/daemon overlap, same-device contention,
  same-document conflict, idempotency, one-device cancellation, restart/uncertain
  outcomes and worst-case concurrent RSS/disk/native-process limits.
- Accept real hardware and physical writer handoff before deployment/cutover.
  Existing scanservjs and the powered-on Canon are unchanged by this work.
