# Explicit Paperless delivery

The reviewed document keeps **Save PDF** as its primary action. A separate
Paperless section loads public capability metadata and the owner-scoped current
delivery ledger. Disabled capabilities expose no delivery controls. Enabled but
unconfigured capabilities name the destination without a send action. Connection
health is an explicit, GET-only provider observation, independent of enablement,
configuration and delivery success. Checking health uploads nothing.

Confirmation names the reviewed document and the administrator's public
destination label. One request key is captured before confirmation. The browser
persists uncertainty before admission, blocks dismissal while admission is
pending, and reads the durable current ledger after response loss and on mount.
An unavailable ledger never grants permission to send. Browser delivery state
does not enter `saveJob`, `active_job_id` or normal job retry/poll handling.

## Durable state and authority

`queued → attempted → accepted → confirmed` is the successful path. The
transactional attempt marker commits **before** the external POST. Any uncertain
transport, process death or ambiguous remote observation becomes `unknown`.
Only exact task and checksum evidence can confirm; task failure/revocation becomes
`failed`. Local cancellation before an attempt becomes `cancelled`; after an
attempt, the result remains unknown and remote processing may continue. There
is no remote rollback promise. No terminal or unknown delivery offers resend.

The application admits a current published PDF only after explicit confirmation,
live principal ownership, document TTL, revision and idempotency checks. Semantic
deduplication covers the owner/document/revision/artifact/destination even under
new keys. Delivery receipts share the operation key space with existing commands.
API routes receive only Health, Task and Metadata method values. Upload authority
belongs solely to the independent worker lane; ordinary rendering and downloading
continue while a remote upload is blocked. Cleanup does not unlink an in-use
artifact; retirement/expiry fences the ledger and normal artifact cleanup owns
eventual removal.

## Pinned remote protocol and recovery

The adapter implements Paperless-ngx **v3.4.2 API 9**, sending
`Accept: application/json; version=9`. The health route map also checks v3.4.2
and its advertised maximum API version 10; that maximum does not negotiate task
responses into an API10 envelope. Upload returns one canonical task UUID. Task
recovery requests exactly `/api/tasks/?task_id=<UUID>`, requires one matching task,
the opaque upload filename marker, a supported status and valid related document.
Success then requires `/api/documents/<id>/metadata/` original SHA-256 to equal
the immutable application artifact. Missing tasks, wrong markers/checksums and
schema drift remain content-free unknown. No list-all task search or blind retry.

The owner may supply the exact UUID in unknown recovery. Neither task authority,
remote document IDs, markers, provider bodies, private origin nor credentials enter
the browser projection. The UUID form explains that reconciliation may remain
unknown. Destination fingerprint drift authorizes no request to a changed origin.

## Fixed-origin boundary

Operators configure one compiled adapter, a canonical origin/path and a bounded
public destination label. Tokens stay server-side in the selected environment
variable. Redirects, ambient proxy use, user URLs and provider-supplied navigation
are rejected. HTTP requires explicit operator opt-in. The browser renders bounded
localized states and uses `textContent`; it never displays raw provider errors.
Same-origin session, Origin/CSRF, owner, revision and TTL checks remain authoritative
at the existing API and store boundaries.

Delivery polling uses its own abort controller and bounded timer (40 observations
per mounted control); navigation disposes it. Confirmation uses the existing native
dialog and control vocabulary, keyboard Escape, pending protection and focus
restoration. No new motion, visual system or dependency is introduced.

## Local evidence and acceptance limits

`scripts/check-web.py --scenario delivery` builds the actual embedded application,
starts real serve/worker processes with private temporary SQLite/artifacts and a
loopback synthetic API9 Paperless server, then exercises Chromium through a local
TLS fixture proxy. The receipt binds source and built asset hashes, synthetic PDF
SHA-256, request methods/paths and upload counters. No real token, private URL,
document text or PDF bytes belong in the receipt. Display-only route mocks in the
companion browser test prove additional UI states, not backend success.

Existing real API/store/worker tests own the crash matrix, concurrent claim,
owner/revision/TTL fences, exact recovery, destination drift and blocked-upload
normal-render progress. The browser adds explicit confirmation, cancel, saved-file
hash preservation, remote-response loss, API/worker/browser restart, UUID recovery,
unresolved predicates and phone/desktop widths.

Phase46 remains **human_needed**, **40/40** functional truths and **three unresolved nonblocking advisories**; no pass or resolved advisory is inferred.
Phase47 remains **human_needed**, **16/16** functional truths, zero implementation gaps, **autonomous_blocking:false**; its **one nonblocking human readability/save-primary review** is retained for49.
The exact-unavailable Docling analyzer limitation, WINDOWS10 and unmeasured quality,
resources and terms stay unchanged. Earlier Phase44/45 flags remain unchanged.

Local protocol proof does not accept live Paperless version/token/proxy/consumer
compatibility, private household sends, physical phones, screen readers, deployment
or production rollout. Phase49 owns integrated/physical UX acceptance; independent
verification owns Phase48 acceptance.
