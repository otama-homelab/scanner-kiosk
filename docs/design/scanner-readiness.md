# Scanner readiness and transactional admission

This stage implements the **repository boundary** for trusted worker readiness
and atomic scan admission. It does not implement physical ownership handoff or a
production evidence producer. The runtime does not call readiness publication,
Authenticated scan HTTP commands now use this boundary, while scanner status now projects the trusted records and the worker still has
no production readiness publisher.
No existing service is stopped and no scanner becomes ready automatically.

The [physical writer preflight](physical-writer-fencing.md) records actual
deployment/network-policy gaps and an unapplied, partial legacy eSCL fence
candidate. Its CRD/server dry-run validation is not readiness evidence.

Admitted work now persists the readiness physical/handoff IDs in its immutable
snapshot. Production Session checks the advertised eSCL UUID at native and idle
boundaries; this does not supply the missing ownership fence or readiness
producer. See [identity binding](scanner-identity.md).

## Trust and evidence

`scannerreadiness.Evidence` is worker-internal: registered device ID, canonical
verified physical ID, ongoing handoff/fence reference, expected idle lease epoch,
effective scan concurrency, and ownership-confirmed/local-stopped/physical-idle
times. Epoch zero is an explicit request to initialize an absent device lease.
Neither a browser field, environment flag, coordinator flock, native metadata nor
Idle by itself establishes exclusive hardware control. The future reconciler must
verify the hardware identity at the registered endpoint, that all legacy writers
are fenced, that local processes are stopped, and that physical idle was observed
subsequently. It must maintain that ownership through acquisition; timestamps or
opaque IDs are not a substitute for mechanical fencing.

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
readiness. The future worker reconciler must publish fresh post-scan evidence for
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

Still required: implement/accept real physical identity and exclusive-writer
handoff evidence, startup recovery, recurring per-device readiness renewal and
post-scan lifecycle, effective-capacity publication from actual runtime policy,
capture UI and physical HTTP/worker/status acceptance, real scanner/mobile/OIDC,
resource acceptance, release/deployment and legacy-service cutover. No deployment
setting or test proof silently authorizes any of those actions.

See [scan HTTP](scan-http.md) for the implemented strict command handling,
server policy and fixture/compiled HTTP validation.

The separate [scanner status](scanner-status.md) projection now uses this proof
and lease data without publishing private worker/job information.
