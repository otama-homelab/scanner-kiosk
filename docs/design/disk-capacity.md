# Physical free-space admission gate

`internal/diskspace` pins the configured document filesystem with a directory
file descriptor and measures `f_bavail` with `fstatfs`. It counts bytes available
to the unprivileged worker, deducts configured headroom and saturates overflow
rather than wrapping. Root symlinks/non-directory paths are rejected. Closing the
meter invalidates future probes; descriptor access and close are synchronized.
Cancellation is checked before/after the syscall, but cannot interrupt a hung
kernel filesystem. Supported stable local mounts remain an operating requirement.

The document store constructor now requires a capacity probe. Application startup
must construct the meter for the actual document volume and pass `meter.Available`;
there is no nil/unknown-capacity fallback in admission. Startup wiring remains
pending. Tests use explicit fake probes except for the live-meter integration.

Both scan and render admission first apply logical principal/global limits, then
observe physical free bytes while the document transaction serializes reservations.
Already stored files are accounted for by the filesystem and are not subtracted
again. All outstanding reservations plus the new reservation must fit the observed
free space after headroom. Partially written work may be charged twice, deliberately
conservatively. Unreleased reservations remain charged until confirmed cleanup.

A failed/negative probe denies new work with a payload-free storage error. Known
insufficient space returns capacity unavailable. Failure rolls back document,
revision, job, receipt and lease changes. Idempotent receipt replay does not request
new space; owned metadata/download lookup also does not depend on a healthy probe.
`SCANNER_MIN_FREE_BYTES` supplies provisional 64MiB headroom via the environment,
forwarded explicitly in Compose/Helm examples. Actual provisioning measurements
must determine production budgets.

This is an observation plus logical reservation, not a filesystem quota. Other
writers, allocation overhead and growth beyond a native scratch budget can still
cause ENOSPC. Execution-time checks, bounded native writers, late write failures,
actual shared-volume mount identity, durable readiness and recovery remain required.
The metadata syscall is the only filesystem operation allowed in this short
admission transaction; image decode/hash, file processing and network calls stay
outside it.

Tests cover actual local measurement, cancellation/close, headroom arithmetic and
overflow, unsafe paths, probe failures, transaction rollback, two independent DB
connections competing for remaining physical capacity, no re-probe on replay and
continued access to an existing export after probe failure. They do not fill a
filesystem or establish native scanner/worker or production ENOSPC acceptance.

Admission capacity should be a separate availability projection. Low capacity
must not by itself remove a healthy read-serving API from service while existing
owned exports can still be retrieved; database/file access health is distinct.

## Execution-time recheck

Scan and render claim now re-observe capacity against all existing reservations,
without adding the claiming job's budget a second time. They recheck the clock
again afterward. Failure rolls back the tentative running/start/heartbeat update;
no native work may start until claim is confirmed committed. Reservations and
physical leases remain held for the controller to reconcile, never freed just
because a capacity observation failed.

For a scan rejected before launch, `AbortUnstartedScan` provides internal terminal
cleanup conditional on immutable null start/heartbeat, original boot and exact held
lease. It races atomically with claim; a started job or reconciling device cannot
use this path. No stop report is fabricated. Executors must not allocate job files
or start children before claim. A controller must interrupt that request after a
preflight failure, not wait for free space and execute it later. If cleanup metadata
cannot be committed, retain the job under reconciliation; do not retry physical
execution. The executable controller and native ENOSPC handling are still pending.
