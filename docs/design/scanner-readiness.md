# Scanner readiness and transactional admission

The runtime publishes readiness for the owner's selected scope: one application
writer sharing the document root, with detection of another application's busy
device. It does not fence LAN clients. See the accepted scope in
[physical writer handoff](physical-writer-fencing.md#accepted-application-scope-2026-11-04-owner-decision).

With acquisition enabled, the worker uses its existing guard and coordinator boot.
Each per-device renewal withdraws old readiness, reads an advisory idle epoch,
then serializes UUID/Idle/UUID observations with native activity. It publishes
only against the same still-idle epoch and active guard. Missing/held/reconciling
work is never cleared by a refresh. Failed observations keep the device
unavailable; unresolved local termination or changed identity stops the worker.
Renewal also runs after completed jobs, using the new idle epoch.

Admitted work retains physical and guard IDs in its immutable snapshot. The
production Session checks identity before native work and around Idle. External
clients can race after an observation; actual native busy/error handling remains
required. No option or browser input supplies readiness evidence.

## Trust and evidence

`scannerreadiness.Evidence` is worker-internal: registered device ID, canonical
verified physical ID, ongoing application guard reference, expected idle lease epoch,
effective scan concurrency, and ownership-confirmed/local-stopped/physical-idle
times. Epoch zero is an explicit request to initialize an absent device lease.
The shared-root guard covers application writers only. The session must verify
that local operations have joined and physical Idle follows that check. UUID
identity is pinned for the session. Historical LAN fencing proposals are not
claimed by this application scope.

`workercatalog.PublishScannerReadiness` requires the current live document-root
guard and exact coordinator boot/generation. It validates ID/time/epoch/capacity
bounds in a short transaction. Times must come from this boot, follow ownership →
local stop → idle, and not be future or 30 seconds old. Expiry is bounded by both
the coordinator and idle age. A heartbeat does not renew this evidence.

Migration 0008 adds `scanner_readiness`, independent of native capability metadata.
A unique physical ID prevents two logical devices from publishing readiness for
one hardware identity. Physical identity and capacity cannot change within the
same boot's publications; a producer must supply the same canonical physical ID
for aliases. This is a repository uniqueness check, not implemented hardware
identity discovery. The current endpoint deduplication remains syntactic only.

A missing lease can be initialized only with epoch zero and no outstanding or
reserved scan work for that device. An existing lease must be idle, unowned and
match the expected epoch. Held/reconciling devices cannot be overwritten. Evidence
for a completed epoch cannot predate that scan's creation/start/heartbeat or bypass
remaining work/reservations. Publication never clears a job, reservation or held
lease. Begin/Stop withdraw readiness without releasing device resources.

## Admission transaction

`documents.Store.AdmitReadyScan` is the entry point used by authenticated
scan HTTP composition. It refuses a caller-supplied boot and requires a deployment
pixel bound. Under the existing enabled-principal → document transaction lock order
it validates all of the following before taking a device:

- Current live coordinator, matching readiness and metadata boot/generation.
- Fresh ownership/stop/idle evidence and idle lease at its exact observed epoch.
- Consistent live worker scan capacity across readiness rows.
- Strictly decoded fresh metadata, same-boot native observations, the complete
  requested size/color/DPI tuple and resource-approved native pixel geometry.
- Existing scan-slot, ownership, revision, document/page/photo, quota and disk
  reservation constraints.

The worker boot and scan capacity come from those records. They are not supplied
by a browser or copied from a previously read readiness snapshot. Job snapshot,
reservation, receipt, document and conditional held-epoch update commit together.
Readiness/metadata/authority are checked again after capacity probing and writes,
using the newly held job and its incremented epoch; expiry during the probe rolls
back the whole operation. No native or network IO belongs in the transaction.

Exact idempotent replay precedes live readiness checks, returning its original
job even after readiness withdrawal without reacquiring a device. A different
command/key conflict never changes devices implicitly. The previous low-level
`AdmitScan` remains for trusted internal composition/fixtures; it must not be
wired directly to public scan HTTP because its coordinator checks are external.

The readiness row's epoch is consumed by admission: advancing the device epoch
makes the old row unusable. Completion returning the lease to idle does not restore
readiness. The worker readiness loop publishes fresh post-scan evidence for
the new epoch. Existing executor resolution still rechecks actual native settings
and final captured geometry; readiness never permits automatic rescan.

## Evidence and remaining work

Tests cover successful boot-pinned claim and replay after withdrawal; missing,
expired, stopped, foreign boot/generation/epoch, future, unsupported and over-budget
settings; strict payload and before-boot observations; caller boot rejection;
proof ordering, identity, capacity and held-work rejection; physical aliases with
transaction rollback; stop/new-boot invalidation without resource release; two
independent connections competing for one epoch; two devices using the live cap;
post-scan renewal; and authority expiring during capacity probing. Migration tests
upgrade a v7 database while preserving held lease/job/reservation rows and creating
no readiness authority. Proofs and physical idle in these tests are fixtures.

Still required: actual HTTP/browser/device acceptance of readiness renewal,
cancel/recovery, mobile/OIDC, resource and deployment acceptance, release and
legacy-service cutover. Unit tests and fresh Idle do not establish these outcomes.

See [scan HTTP](scan-http.md) for the implemented strict command handling,
server policy and fixture/compiled HTTP validation.

The separate [scanner status](scanner-status.md) projection now uses this proof
and lease data without publishing private worker/job information.
