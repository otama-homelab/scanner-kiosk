# Claimed scan executor

`internal/scanjob` connects an already-confirmed `ClaimScan` to one capture,
conditional original/page publication, and failed-work cleanup. It neither
claims jobs nor retries a physical acquisition. The executable worker still
needs scan dispatch and a production implementation of the driver ports.

`Records` is independent of SANE and provides claimed metadata, cancellation
pulses, publication/read-back and internal stopped-work cleanup. `Driver`
resolves the registered device and backend geometry, and supplies a fresh
physical-idle observation. No browser supplies stop or idle evidence.

`PrepareClaimed` loads the exact claimed snapshot, bounds execution by its
original deadline, and uses `workwatch` to stop on persisted authorization loss.
The resolved device, size, mode, DPI and byte budget must match the snapshot and
reservation. Capture returns an owned attempt even on partial failure. A missing
attempt after invocation or unknown local process stop retains resources for
reconciliation. A failure before invocation has no local process to stop, but
still needs a fresh hardware-idle observation before releasing its held lease.

`Prepared.Publish` checks validated output metadata and requires physical idle
observed after local stop and the observation call began. The repository repeats
owner/revision/boot/epoch/deadline predicates. Successful publication transfers
file ownership with `Attempt.Release`. A commit acknowledgment failure uses
immutable producing-job read-back; unreadable outcomes prevent deletion. Only
read-back is retried, never capture or the publication write.

`Prepared.Finish` refuses unknown execution and unresolved publication. It
requires fresh physical idle, deletes only owned unpublished files, and calls
the repository with local-stop, idle and cleanup timestamps. Cleanup failure
retains the reservation. An uncertain cleanup commit is read back before another
transition. Disabled/expired owners use the internal cleanup port; the monitor
never frees resources on their behalf.

`ExecuteClaimed` composes these operations and returns a nonnil recovery object
when anything remains unresolved. The runtime must retain that object and must
not advertise the worker/device ready. Durable recovery after worker restart is
still pending; this in-memory object alone does not solve restart reconciliation.

## Evidence and limits

Tests use real SQLite admission/claim, the product SANE adapter with a synthetic
command, persisted cancellation, publication/cleanup read-back failures,
missing attempts, unknown local stop and unknown hardware idle. The shell
cancellation fixture replaces itself with a single process; a shell which leaves
a descendant correctly gives unknown group-stop evidence and must keep its lease.

The opt-in `TestInstalledSaneScanJob` uses the unmodified official SANE `test:0`
binary, generates A4 gray 300-DPI pixels, and publishes the original/page into the
admitted database job. `scripts/check-scanner.py` includes this test and rejects
skipped tests. Its idle observer is explicitly a fixture, not real hardware proof.
No Canon capture, production capability/idle probe, worker-command scan dispatch,
HTTP, crash recovery or deployment acceptance follows from these tests.


## Registered driver and unresolved resolution

The [registered driver component](registered-scanner-driver.md) now connects SANE selected-option inspection, native parameter read-back, one-use capture and the configured idle observer. Tagged integration uses actual official test-backend acquisition and real document transactions; status is a local HTTP fixture. The production environment/runtime composition remains pending.

Resolve may allocate native inspection work before any capture call. Errors carrying scanner.ErrUnresolved now keep the executor's launch/stop state unknown, so it cannot fabricate a fresh local-stop timestamp, invoke idle as a substitute or release reservations/leases. A regression exercises failed Finish with no idle/terminalization calls and resources held. Duplicate capture invocations likewise provide no stop proof because a first invocation may be active. The concrete SANE adapter's documented pre-allocation refusal can be represented as explicit no-start evidence; that is separate from an unknown native result. These protections do not implement durable recovery.
