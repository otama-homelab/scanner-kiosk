# Worker startup and maintenance records

The runtime composition opens SQLite and obtains the private-volume worker guard.
It first calls the catalog adapter's `WithdrawPrior` with the successor's active
guard and a five-second context, then startup control flow consumes
`startupRecords`, not SQL or a concrete store:

1. Atomically mark the predecessor coordinator stopped and remove OCR/scanner
   metadata and readiness before inspecting old work. This does not publish a
   successor generation, stop processes or release job/device resources. Failure
   prevents maintenance, native configuration and dispatch.
2. Drain bounded metadata maintenance batches under the still-owned guard.
3. Read `RecoveryRequired` through the document adapter with a five-second context.
4. Sweep known legacy empty producer/quarantine directories under the guard,
   with a streaming 64-entry cursor and the configured cleanup budget. Unknown
   prior work, lost authority or sweep failure prevents new producers. See
   [artifact GC](artifact-gc.md) for the deliberately bounded namespace.
5. Continue native configuration/catalog/dispatch only after a confirmed clear
   read and an uncancelled context. An error never means clear.

Each batch queues up to 20 expired-document cleanups, aborts up to 100 withdrawn
unstarted scans, aborts up to 100 unstarted renders, retires up to 100 completed
statuses, and purges up to 100 proven-aged request receipts in that order. Atomic owner/revision/expiry/resource checks remain inside
each backend command. A full count in any stage requires another batch before
startup inspection. Negative or over-limit counts are backend errors, never an
empty queue. Idle maintenance performs one batch. `--once` still performs at most
one physical job; metadata draining does not change that meaning.

A batch uses a five-second context detached from caller cancellation but retaining
context values. Once started, its confirmed commands can complete after a stop;
all subsequent stages remain bounded by that same deadline. Commit uncertainty or
any other command error stops the batch immediately. Parent cancellation is checked
before each batch and after a completed batch, preventing native work or another
batch after cancellation. The startup read has its own bounded cancellable context
and checks cancellation again after backend return.

`documents.Store.RecoveryRequired` implements the SQLite read. It includes any
non-cleanup job in running/cancelling/reconciling, queued jobs with runner or start
markers, and every held/reconciling/nonempty device lease. Disabled or expired
owners do not remove uncertain work from this gate. Never-started queued render
work and idle devices do not require reconciliation. Cleanup jobs use their own
manifest/attempt recovery instead of blocking the entire startup. Inspection does
not update jobs, reservations or leases, and supplies no process-stop or scanner
Idle evidence. A guard acquired after a crash does not prove the old hardware
writer stopped. Unknown native work still stops startup; automatic native recovery
and real physical handoff/readiness production remain incomplete.

Cleanup dispatch consumes `cleanupDispatchRecords`, extending cleanupjob's atomic
manifest/progress/completion port with candidate selection and attempt claim. An
uncertain claim is read back for the exact job/new runner once before file access;
no claim is retried. Unreadable/rolled-back outcomes or lost runtime authority
preserve files. Progress/completion uncertainty remains subject to the existing
exact acknowledgement checks. Rooted quarantine/unlink/fsync stays outside SQL.
The OCR/render wrapper embeds execution/pulse/stop/thumbnail publication ports,
keeping provider initialization and bitmap policy outside the database adapter.

## Evidence and limits

Non-SQL startup fixtures check all five full-batch conditions, exact command order,
errors at every stage including inspection, invalid counts, cancellation between
confirmed commands/batches, bounded context propagation and cancellation after a
clear inspection. SQLite fixtures check job markers/states, disabled/expired
owners, independent held/reconciling leases, eligible cleanup and no job/quota
mutation. Runtime regressions retain refusal to resume uncertain prior boot work
and metadata draining beyond 100 records.

A non-SQL cleanup dispatcher uses real private files and verifies normal removal,
confirmed claim/progress/completion after lost acknowledgement, no repeated claim,
rolled-back/unreadable claim preservation and authority loss before/after claim.
Existing SQLite cleanup/runtime tests cover backend atomicity separately. These
fixtures prove consumer injection and fail-closed orchestration; they are not an
alternative production database, full non-SQL runtime, native crash recovery,
scanner handoff, real-device acceptance or deployment.


## Predecessor admission withdrawal

A released flock does not expire DB publications immediately. Previously the old
admission metadata survived until the successor reached catalog `Begin`, after
maintenance, recovery inspection and native configuration. An API request could
therefore commit against the predecessor while the successor was starting.
`WithdrawPrior` closes that window before maintenance and the recovery barrier.
A concurrent ready admission either commits first and retains its old job/lease
for subsequent maintenance/recovery, or observes withdrawn authority and fails.
Exact existing receipt replay remains available without acquiring a device.

The transaction stops the old coordinator and deletes all three publication
sets. It preserves coordinator boot/generation/timestamps, jobs, reservations and
leases; `Begin` increments generation only after recovery succeeds. Repeating
withdrawal before Begin is safe; trying it with a guard whose boot is already the
current coordinator is rejected. Missing/closed guards are rejected. A failed
transaction does not leave a partially withdrawn publication. Guard/document
volume binding remains the composition root's responsibility, as for Begin.
This is local metadata fencing, not proof of global physical writer ownership.

SQLite tests cover live predecessor publication, new admission rejection, exact
replay, held-resource preservation, successor generation, own/closed/nil guard,
rollback after a late delete error, and independent-connection withdrawal versus
admission. Runtime tests verify withdrawal persists even when recovery blocks on
an old running job, and withdrawal failure prevents queued work from starting.
