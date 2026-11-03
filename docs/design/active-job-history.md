# Active work discovery and batched history

Document detail and history now expose nullable `active_job_id`, selected from
owned non-cleanup queued/running/cancelling/reconciling jobs in the same transaction
as the document projection. The partial unique active-job index limits it to one.
Terminal work and cleanup are not presented as resumable content operations.
Expired/deleting documents and disabled/foreign principals remain inaccessible.
The field contains only the owned opaque job ID, never its worker, configuration,
provider data or storage metadata.

The client offers View processing status from both document and history. It reads
the existing owned job endpoint; opening the link never replays acquisition or
rendering. Reloading the document/history rediscovers the job from server data,
without remembering the last job in browser storage. An active document's scan URL
redirects to its job instead of proposing another capture. While active work is
known, document mutation/output/next-page controls are withheld or disabled.
Home remains a separate new-document intent with editable original type.

## Query shape and retention

History previously selected root IDs then queried each document's root, pages and
output separately. It now performs three bounded document queries for a nonempty
page: root projections (including scalar indexed job metadata), all selected pages,
and all selected current outputs. The owner predicate applies to every query;
selected document IDs use placeholders and at most the validated page limit of 100.
Page order is reconstructed per document. Only sealed current-revision outputs
become downloadable; stale artifacts are not made ready.

Detail reuses the same root scan/content hydration. Both paths check retention
again after content reads; a document that expires during projection is rejected
in detail and omitted from history. Cursor ordering remains `(expiry, id)` and
pagination retains the existing content-free positional hint semantics.

Migration 0009 adds ordered `(document_id, owner_id, input_revision, created_at, id)`
job lookup for persisted photo preference; active job lookup retains the existing
partial unique index. No job, lease, reservation, TTL, native process or identity
is rewritten by the migration or status reads. The readiness upgrade fixture now
also checks the v7-to-current upgrade preserves held work while creating no proof.

## Evidence

Repository tests cover all active/terminal states, cleanup exclusion, owner scope,
history/detail parity and expiry during reads. The actual SQLite-driver counting
wrapper verifies both 1 and 32 documents use exactly three document-query calls,
plus the constant separate principal check. Batch tests preserve multiple page
ordinals, owner isolation, current output and invalidation of older artifacts.
Fixture setup honors the published-output revision foreign key: it removes the
publication before advancing revision, matching real edit/admission behavior.

API tests discover the owned job and open its status while refusing another owner.
The real embedded-browser fixture creates a queued job in the private API DB,
reloads its document, checks mutation controls are disabled, opens status from
both document and history, and recovers the same job from the scan route. Existing
polling/focus/cancel/reload/terminal checks continue. These fixtures do not prove
physical cancellation, startup hardware reconciliation or legacy writer handoff.

The query-count proof applies to history and the previously tested scanner list;
other repository/query/alternate-backend acceptance remains unfinished. Real
readiness production, complete editing/cleanup/recovery, mobile/IdP/resource and
release/deployment/cutover remain part of the full plan.
