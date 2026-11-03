# Render job admission and idempotency

`documents.Store.AdmitRender` now admits export work for an existing captured
document. It does not create documents, operate a scanner, resolve OCR capabilities,
run rendering, or accept filesystem paths from a browser. HTTP authentication and
server-side capability resolution remain coordinator responsibilities.

## One transaction

The enabled-principal guard and state → document lock order from
[document transactions](document-transactions.md) apply. Admission first looks up
the principal-scoped SHA256 key receipt. The canonical command digest includes
operation/version, document ID, expected revision, output format/compression and
client OCR choices. Server-resolved model version, omitted-preset resolution,
reservation amount and deployment timeouts are excluded from the command hash.
This preserves a retry's original job after provider/policy reconfiguration.

An existing key with a different command conflicts. A matching live receipt returns
the same job, including completed/failed/cancelled work; it never re-executes it.
Deleted results, expired content or expired receipts return gone. Receipt lookup
precedes current document revision/state and current provider version validation.
The eventual HTTP coordinator must also check replay before requiring a currently
installed provider; an old receipt is not dependent on a fresh model-health call.

For new work, the service checks an owned, unexpired review/ready/failed document,
expected revision, no incompatible active job, allowed document/photo output,
ready original for every page, and page count. OCR-enabled snapshots require a
server-pinned model version; capability support still needs the registry check.

It records the ordered page IDs, original artifact IDs, rotation, capture paper
size/color/DPI and resolved output/OCR settings as an immutable versioned snapshot.
Later edits/provider changes cannot rewrite that snapshot. A newly requested output advances the document revision and invalidates its previous
publication pointer. Job creation at this new revision, positive byte reservation,
hashed content-free receipt and processing document state commit together. A replay
does not advance revision. Failed admission preserves the old revision/publication. Deadline is clamped to document expiry. Receipt lifetime must cover the
remaining content lifetime. Neither admission nor replay extends document TTL.

A commit error is uncertain. Repeating the same canonical command/key reads back a
committed receipt rather than creating another job; a rolled-back operation can
be admitted once. No network request or file processing runs in the transaction.

## Capacity boundary

The coordinator supplies deployment-owned limits and a conservative reservation;
these fields are not part of the HTTP Render schema. Admission sums existing
artifact metadata and all outstanding reservations, including cleanup-pending
records, for both principal and global budgets. Checked subtraction avoids integer
overflow. Reservations across independent DB connections serialize, so two users
cannot both consume the same remaining logical capacity. Do not release a failed
or cancelled job's reservation before process/workspace reconciliation.

`SCANNER_GLOBAL_MAX_BYTES` adds a provisional 1GiB global logical budget beside
500MiB/user and 200MiB/job defaults. This is not a measured production capacity or
filesystem quota. Physical free-space/headroom admission checks are now implemented; see
[disk capacity](disk-capacity.md). Scratch growth, sidecar
usage, storage metadata, expired-but-not-removed files and orphan recovery remain
separate work. A successful logical reservation does not prove a disk write can
succeed. Final provisioning/resource acceptance may revise these defaults.

Compose now forwards the documented capacity, timeout, Tesseract and OIDC/session
settings rather than silently leaving those .env overrides unused. OIDC/session
values stay in the API environment; worker metadata/limits are shared explicitly.
Arbitrary LLM keys still require explicit worker-only environment injection.

## Evidence and remaining scope

Real SQLite/race tests cover same-key concurrency, mismatched payload, gone results,
owner/expiry/revision/state rejection, missing originals/pages, page limit,
user/global/job budgets, cross-user capacity competition, frozen page/model/default
snapshots, commit-then-error replay and admission → claim → publication. The scoped
file publication integration also starts with actual admission. Its opaque content
fixture proves record/file lifecycle, not real PDF validity or scanner behavior.

First/next scan and document-count/lease admission are now implemented as described
in [scan admission](scan-admission.md). Still pending: physical device
reconciliation, execution-time capacity checks, API error mapping, worker loop,
OCR/PDF processing coordinator, failure-resource release, edits, downloads/stream
cancellation, GC and application/browser/mobile/device acceptance.
