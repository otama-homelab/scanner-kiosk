# Cleanup coordinator and worker dispatch

`cleanupjob.ExecuteClaimed` operates only on one confirmed running cleanup job.
It does not claim work, acquire devices or fabricate stop evidence. Its Records,
Remover and CheckAuthority ports separate DB progress, private filesystem removal
and runtime authority. Caller retains and joins the worker guard/root until return.

The coordinator reloads one pending manifest file at a time and checks the job,
canonical v1 configuration, revision/document identity and current IO deadline.
A joined workwatch monitor repeats guard/manifest checks during file work. No DB
transaction or guard mutex spans hashing/unlinking. Returned removal evidence is
bound to job, artifact, size and hash. Already completed IO is reported through a
bounded noncancelled context even when cancellation/authority loss arrives just
as unlink finishes; DB runner/manifest fences still apply. No further file starts
after loss. Record/finish commit uncertainty performs read-back only. Unavailable
or negative read-back returns reconciliation, never another automatic write or
quota release. A stopped invocation can be explicitly resumed under the same
confirmed job/runner while its deadline remains valid.

Normal worker startup discovers up to 20 expired documents and aborts up to 100
withdrawn never-started renders before the conservative uncertain-native check.
Idle dispatch repeats bounded maintenance, selects a presently eligible cleanup
candidate, then rechecks it transactionally with ClaimCleanupAttempt. A lost claim
acknowledgement is read back for the same known job/runner before any IO. Runtime
authority checks bind the live guard/root inode and publisher status. Cleanup runs
only with no owned active scan tasks, before the next render. `--once` executes
at most one cleanup or scan/render; the daemon proceeds to the next iteration.
Startup and normal idle loops therefore collect ready deleted/expired documents,
including disabled owners. Other active/uncertain document resources are not
cleared to make cleanup runnable.

Bounded metadata maintenance finishes under a five-second noncancelled context
while the worker guard remains owned. Confirmed success then returns an arriving
stop request as exact cancellation. Its storage/timeout/commit uncertainty remains
a failure; it is never downgraded. A driver's explicit precommit context cancellation remains exact cancellation;
independent/combined storage failures and uncertain commits remain failures.
Render/scan claim paths avoid joining the same cancellation twice. This preserves
the CLI rule that only exact context.Canceled permits a clean interrupted exit.
Interrupted claimed cleanup still returns reconciliation because durable work
remains running; it is not converted to a successful exit.

Coordinator tests use real private files with a small Records test double. They
cover committed/rolled-back/unreadable progress acknowledgements, finish loss,
same-job resume, joined authority-loss cancellation, misbound reports, file hash
mismatch and cancellation immediately after unlink. The compiled worker checker
adds real SQLite/file removal/status/quota completion from synthetic delete intent,
repeat fresh boot without redoing completed cleanup, and startup TTL collection
for a disabled owner. Existing runtime cancellation and native scan fixtures are
also rerun. None of these tests invokes real scanner acquisition or production GC.

Remaining: live/unresolved scans and unknown native recovery, legacy/orphan
directory sweeping, safe receipt-hash retirement and real filesystem/resource
acceptance. Browser
deletion controls are described in document-deletion.md. Running cleanup uses explicit fenced takeover below; no completion is fabricated. Foreign/unknown native
jobs still block worker startup conservatively. No replacement of scanservjs or
production deployment has occurred.

Document slots are now reclaimed only after the same completed-cleanup predicate
used for commit reconciliation succeeds. Pending/failed/expired/inconsistent
cleanup still counts. The minimal completed tombstone and delete receipt remain
for independent status retention. An indexed artifact lookup prevents repeated
full artifact scans. Tests cover slot reuse with MaxDocuments=1, unfinished and
inconsistent states, concurrent admission on separate scanners into one reclaimed
slot, unchanged delete/scan replay and the actual SQLite query plan. Tombstone
retention purge remains separate pending work.

Withdrawn never-started scan maintenance now precedes the startup uncertain-work
barrier and runs in normal idle maintenance. Discovery is bounded to 100 jobs,
requires a deleting/expired document, null immutable start and heartbeat, absent
produced-artifact links and an exact held device/epoch lease. Each candidate is
rechecked by AbortUnstartedScan with its original runner/boot identity. This
terminalizes metadata and releases its reservation/lease without executing or
adopting the old capture; no physical stop report is generated. The individual
abort also rejects produced-artifact links. Disabled owners need no enabled-user
lookup for this internal maintenance.

The batch is not one transaction: earlier confirmed aborts remain committed if a
later one conflicts or fails. Conflict and commit uncertainty stop maintenance
and remain errors, including when stop arrives; a subsequent boot observes the
persisted state. Started jobs, heartbeat-bearing jobs, unknown/reconciling leases,
and live documents are left untouched. Startup now repeats full metadata batches before inspecting the native barrier;
this is not general native recovery. The worker guard remains required. Tests
exercise deletion/expiry/disabled owner, preserved boot/null-start identity,
cleanup eligibility, no-op repeat, invalid limits and lost commit acknowledgement.
A runtime test boots from synthetic prior-boot metadata and removes a real
private original through cleanup, repeats boot, and confirms a started counterpart
continues to block startup while preserving its file. This is no hardware test.


## Restart and expired cleanup recovery

Documents schema 14 adds one bounded `cleanup_attempts` row per active cleanup.
Its start/deadline belong to filesystem IO, independently of immutable document
TTL, job creation/start/deadline and request receipt retention. Environment variable
`SCANNER_CLEANUP_TIMEOUT_SECONDS` is worker-only (default 300, range 1–900), and
is exposed in Compose/Helm/environment examples. Starting or retrying GC never
extends content access or resurrects an expired receipt. Ordinary claim/load APIs
retain their old deadline semantics for legacy callers without an attempt.

Under a fresh exclusive worker guard and the usual rooted publisher authority,
NextCleanup can select either never-started queued cleanup or interrupted running
cleanup. The exact previously observed runner is passed to ClaimCleanupAttempt;
a transaction rechecks document withdrawal, all producer/reservation/device lease
barriers, canonical configuration, and complete immutable manifest including
already deleted entries. A queued claim freezes a new manifest once. Takeover
keeps the original manifest/progress and job start/deadline, changes the job runner,
and records a new generation and finite attempt deadline. Same-runner renewal and
stale observed-runner takeover are rejected. Even an expired original job/attempt
can be retried under a fresh guard; elapsed time alone does not prove exclusivity.
None of these methods adopts native scanner/render jobs or clears their resources.

LoadCleanupWork uses the current attempt's runner/deadline (legacy fallback is
the immutable job deadline). Old runners cannot load, report removals or finish;
new removal evidence must be no earlier than the current attempt start. Already
acknowledged files retain their evidence and are not reprocessed. Pending files
reuse the same job/artifact quarantine identity. The rooted remover re-verifies
quarantined files, or confirms missing source/target leaves and syncs directories
after a pre-acknowledgement unlink. Hash/path conflicts remain errors. The worker
keeps guard/root ownership and joins all file/monitor/report work before exit.
Uncertain takeover commits use the same known job/new runner read-back and never
repeat the write automatically. Finish removes the attempt with manifest/payload
metadata and releases quota/document slots atomically. Reports of completed IO
remain allowed after the attempt deadline; no new file IO starts after it.

DB tests cover partial progress, expiry, disabled owners, old reports/loads/finish,
stale candidates, competing successor transactions, deadline bounds, same-runner
renewal rejection, malformed/incomplete state and committed/rolled-back lost
acknowledgements. Migration tests preserve a v13 running manifest with one pending
and one acknowledged file and create no new authority by upgrading. Runtime and
compiled CLI tests use real private files at pre-quarantine, post-quarantine,
post-unlink and post-acknowledgement states; expired job/attempt recovery and repeat
boot are checked. Synthetic restart fixtures are not physical/native scanner
recovery or production volume/resource acceptance. Current manifest empty directories and completed status retirement are connected
below; legacy/orphan directory sweeping and receipt-hash purge remain outstanding.


Startup metadata draining repeats while any discovery/recovery batch is full
(TTL20, withdrawn unstarted scan100, unstarted render100). Each batch retains the
five-second noncancelled metadata bound and returns a stop request only after
confirmed commit; uncertainty/conflict stops rather than retrying a write. Between
batches caller cancellation is checked. Idle dispatch still does one bounded batch
per iteration. --once drains startup metadata but executes at most one physical
cleanup/scan/render. A fixture with 101 expired old-boot unstarted scans and exact
held leases/reservations proves all TTL intents and pre-launch aborts are processed
before startup's unknown-native query, and only one physical cleanup is dispatched.
Completed cleanup removes its old terminal scan record; the other 100 remain as
cancelled/not_started, with no held lease/reservation. No physical scanner runs.


Startup/idle metadata maintenance also retires at most 100 completed content-free
cleanup statuses, with full startup batches repeated. Completion, original job
and all result-receipt retention gates apply; see document-deletion.md. The receipt
hashes stay as replay tombstones. Physical removal now prunes only empty pinned
manifest ancestors and quarantine directories, with sync-before-proof and recovery
from missing parents; see artifact-gc.md. A bounded startup sweep now retires known flat empty producer/quarantine
directories left by legacy acknowledged cleanup; see artifact-gc.md. Recursive
untracked/native file recovery and a safe key-age protocol for receipt-hash purge
remain separate outstanding work.


Schema0016 now adds bounded aged document-request receipt retirement as a fifth
metadata command. Only proven rq1 keys retire after both deadlines; legacy NULL
rows remain tombstones. The atomic monotonic clock fence prevents re-execution
after purge and clock rollback. See [request keys](request-keys.md). This does not
retire state/auth receipts or complete legacy opaque-key migration.


## Failure stages

The cleanup executor retains `ErrPending` and the underlying error chain while
adding a static stage to internal worker diagnostics:

| Stage | Meaning |
|---|---|
| `load cleanup manifest` | Loading the currently fenced cleanup work failed. |
| `supervise cleanup removal` | Guard/manifest monitoring or file removal failed. |
| `record cleanup removal` | Reporting an observed unlink/sync failed; physical removal may already have happened. |
| `finish cleanup` | Final metadata completion or its uncertain-commit read-back failed. |

Stages add no document IDs, content, paths or credentials. They change no retry,
claim, stop or resource-release decision. Operators must retain the durable
manifest and reservations until the existing reconciliation protocol confirms
progress; a stage is not permission to clear records or repeat native work.
The interrupted-cleanup runtime fixture additionally records synthetic job and
attempt timestamps, manifest pending/deleted counts and original-file absence
only on failure. Those records narrow a failure without proving its cause.
