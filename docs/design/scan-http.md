# Authenticated scan commands

The API now connects `POST /api/documents` and
`POST /api/documents/{document_id}/scans` to the transactional readiness boundary.
The physical ownership/readiness producer is not wired into the worker, so normal
production startup still cannot advertise or accept a ready scanner. There is no
new default activation, device request or legacy-service change from these routes.

The document handler uses the existing session/Origin authorization, bounded
request middleware and owned job projection. `scanadmission.Service` depends only
on an `AdmitReadyScan` records port and applies server deployment policy. It never
constructs a native provider or resolves readiness in a separate HTTP preflight.
The composition root supplies registered IDs, document retention/count/page/pixel
limits, scan timeout, user/global/job byte ceilings and receipt retention.

## Request and policy

The first command requires `scanner_id`, `purpose` and a complete `capture`
(size/color/dpi), plus an optional photo JPEG/PNG format. Photos default to JPEG;
explicit photo format on a document is invalid. The next-page command requires
only scanner and capture; purpose/output intent remain with the existing document.
It requires a canonical quoted positive revision in `If-Match`. Create rejects an
`If-Match` header. Both commands require exactly one valid `Idempotency-Key` and
same-origin authorized principal.

Only application/json within 64 KiB is accepted. Duplicate keys at every nested
object, case aliases, unknown fields, explicit nulls, missing required fields,
incorrect scalar types, trailing JSON, query strings, duplicate control headers
and noncanonical revisions are refused. Owner, document IDs in the body, worker
identity, model/internal execution policy and reservation bytes are never accepted
as browser fields. No file upload, endpoint or CLI arguments enter a scan command.

The service reserves the configured whole-job byte ceiling; the DB still checks
shared physical/logical capacity and the live worker's scan-slot policy. It clones
the registered-device policy into the transaction. That policy is applied **after**
receipt lookup: removing a scanner from API configuration must not turn a safe
exact replay into a new command or require a device that has since disappeared.
The authoritative boot and scan concurrency come from current readiness records,
not from browser fields or a stale API-side worker concurrency setting.

Successful admission/replay returns `202` with the owned minimal Job and
`Location: /api/jobs/{id}`, no-store and no private worker/handoff/native metadata.
It says the command was accepted, not that physical acquisition or publication
completed. The handler never invokes native acquisition. Missing/stale/busy
readiness returns `503 scanner_unavailable`; an unregistered fresh selection is
`422 unsupported_capability`. Revision mismatch is `412`, conflicting operation or
photo append is `409`, and cross-owner documents are inaccessible. After an
uncertain response, the server's durable receipt prevents a duplicate capture;
clients still must not automatically replay physical commands.

## Insert and replace a scanned page

`POST /api/documents/{document_id}/scans` also accepts either
`insert_before_page_id` or `replace_page_id`. Omitting both retains append behavior.
The two fields are mutually exclusive, require an existing page in the same owned
document, and are forbidden on document creation. The existing `If-Match`,
Origin/session, request size and idempotency requirements apply. Placement is part
of the command digest and immutable worker snapshot; changing the target while
reusing a key is a conflict.

Insertion shifts following pages and commits the new page in one transaction.
Replacement leaves the target visible throughout capture and removes its page row
only in the successful publication transaction. Failure, cancellation, stale
completion or a rolled-back storage error preserves the target and its original
artifact. Unaffected pages retain their IDs. The replacement gets a new page ID;
its position is the old target's position. Existing artifact retention/cleanup
still owns the displaced original; publication never deletes its file directly.
A replacement is allowed at the final page-count ceiling, but still reserves and
checks the complete capture byte budget. Admissions invalidate previous output
at the new revision as they do for append scans.

These are backend/API capabilities. The page insertion/replacement UI and hosted
binary/browser acceptance are still pending in plan 43-08.

## Evidence

Real API composition/session/repository tests exercise readiness refusal without
partial work, then fixture readiness → new scan → boot-pinned queued job and
server reservation. They verify exact replay after readiness withdrawal and after
API scanner registry removal, changed-command conflict and no duplicate work.
The next-page test claims and commits an artificial PNG through the actual records
boundary, renews fixture readiness, rejects foreign ownership/stale revision, then
accepts/replays a next-page command while preserving the original page and TTL.
JPEG/default JPEG/PNG intent is pinned and a captured photo cannot append pages.
Strict-input, body/media/header, anonymous/Origin and capacity-policy checks run
through the same handler/middleware as the application. The HTTP group was repeated
three times with the race detector.

`check-api.py` also starts the compiled serve command with private fixture stores:
new/next route composition, anonymous/Origin/browser-policy rejection and absent
readiness are exercised over real loopback HTTP. The missing-readiness case must
leave documents, jobs, reservations, receipts and device leases empty. Successful
queued admission in the Go API tests uses explicitly synthetic handoff/idle proof;
it is not a real hardware or production writer-fence acceptance.

Still required: real readiness/ownership production and renewal, public per-device
status, actual capture/scanner-selection UI/device acceptance, actual API → daemon → device acceptance,
mobile/OIDC/deployment/resource tests and release/cutover. Existing thumbnail,
whole-document cleanup/recovery and other plan requirements remain unfinished.

The embedded capture UI now calls these routes; see [capture UI](capture-ui.md).
Its browser scanner/admission fixtures are separate from physical acceptance.
