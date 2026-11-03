# Document record and export publication transactions

`internal/documents` implements a database service boundary. Callers supply an
already-authenticated opaque principal ID; this is not HTTP/session authentication.
It currently provides owned document listing/read, export metadata read, conditional
claim of an admitted render job, persisted cancellation request, export publication
and worker-only boolean publication reconciliation.

## Principal guard and lock order

Every public-facing record operation pins an enabled principal with a short
`BEGIN IMMEDIATE` on a dedicated durable-state connection before opening the
document transaction. Principal-disable writes cannot interleave that operation.
The state guard only reads and always rolls back; the document store alone commits.
This is not a cross-database atomic commit. Lock order is state → documents.
No remote request, OCR, file hashing, image processing, download stream or wait for
hardware belongs inside these transactions. Cleanup uses a cancellation-independent
bounded context and discards a connection if rollback cannot be confirmed.

All future cross-store code must preserve that order. Disabled-owner cleanup and
recovery have separate internal authority; they must not turn worker metadata
helpers into public endpoints. A later principal disable can deny subsequent
requests after an operation completes. Active HTTP stream cancellation still
needs implementation; the database guard does not protect a response already sent.

## Implemented predicates

Document/list/export metadata reads scope owner and deny `expires_at <= now`,
expired and deleting documents. Output metadata additionally joins current revision,
ready artifact and publication pointer. Relative file paths remain internal.

Render claim is a conditional queued → running update. It requires a matching
owner, unexpired job/document, current input revision, processing document and
positive existing byte reservation. It sets a runner boot identity. It does not
admit a new job, acquire hardware or retry scanning. Two independent DB connection
sets cannot both claim the same queued job.

Publication requires the same runner, running render job, owner, input/current
revision, deadline and document expiry. Output size must fit its existing
reservation; document exports are PDF and photo exports JPEG/PNG. The coordinator
must already have validated actual file content, page set, dimensions, encoding
and artifact provenance. This service consumes internal sealed metadata, never
browser-provided paths/digests. It does not perform PDF validation. Existing-document render admission now performs
logical quota/reservation checks; see [render admission](render-admission.md).

The transaction inserts the ready artifact and producing-job link, replaces the
publication pointer, marks the job succeeded/document ready, and releases the
reservation. It rechecks time before commit and never extends creation-based TTL.
A stale revision, cancellation, wrong runner, disabled principal, expiry or deadline
rejects the output without committing partial metadata. RequestCancel persists
`cancelling`; it is not process/hardware stop evidence or reservation release.

## Uncertain commit and file handoff

Document migration 0002 adds immutable `job_artifacts` identities, constrained to
the same owner/document/input revision and ready artifact. This identifies the
producing job even after another export replaces the current publication pointer.
It is required for read-back, not a new document payload store.

A commit error returns `ErrCommitUncertain`; callers must not infer that nothing
was saved. Worker-only `ExportRecorded(owner, job, artifact)` returns a boolean
without content or paths and can reconcile disabled/expired owners. On confirmed
publication, transfer sealed-file cleanup responsibility with Scope.Release. On
confirmed failure, Scope.Close removes only derived files. If the database itself
is unavailable, retain files under recovery ownership until read-back is possible.
Fault-injection tests cover both commit-then-error and error-without-commit.

## Evidence and remaining application work

Race-enabled tests use real SQLite migrations, independent connections and fake
clock boundaries. They cover Alice/Bob isolation, exact expiry, principal disable,
competing workers, runner/revision/cancellation rejection, output budget/media type,
late-clock rollback, durable publication read-back, and lock cleanup on cancellation.
A filesystem integration test connects the actual artifact scope to publication,
retention on success and cleanup after stale/cancelled rejection, preserving inputs.
Its content fixture is opaque: it proves file/record lifecycle, not valid PDF content.

Still pending: authenticated HTTP/Range/thumbnail routes and streaming cancellation;
physical free-space checks and native scan/reconciliation orchestration; concrete edit operations;
OCR intermediate result coordination; worker loop/deadline/restart recovery;
principal/guest-end services; deletion/TTL GC; and actual scanner/PDF acceptance.
These database operations do not establish full user isolation or product readiness.

Existing-document render admission, receipts and logical capacity reservation are
now implemented; their predicates and limits are in [render admission](render-admission.md).

First/next scan admission and conditional boot/lease-bound scan claim are also
implemented; [scan admission](scan-admission.md) distinguishes these records from
unimplemented device execution, initialization and stop/cleanup evidence.

[Scan completion](scan-completion.md) now commits original/page publication and
conditional stop cleanup. Migration 0003 records immutable start time separately
from mutable heartbeats; native evidence production remains pending.

[Physical capacity admission](disk-capacity.md) now requires a probe in Store.New.
The short admission transaction permits only its bounded filesystem metadata
observation; actual startup construction and native execution checks remain pending.
