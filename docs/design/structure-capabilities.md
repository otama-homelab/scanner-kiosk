# Application-owned capability boundaries

Correction, OCR, structure and delivery keep their existing owners. They do not
share a new runner or provider interface. Registration is compiled and enabled
by operator configuration; metadata never authorizes a job or proves connection
health. Optional processing stays behind an explicit user action. Ordinary
scan → confirm → save remains available when optional processing is unavailable.

## Responsibility and state matrix

| Capability / owner | Enablement and configuration | Metadata, health and destination | Explicit admission and scoped output | Failure / unknown recovery |
|---|---|---|---|---|
| Correction: `internal/documents/correction.go` and worker transforms | Existing correction suggestions; full original / no correction stays default. | Revision-bound suggestion, original/correction hashes and risk flags; local transform, no remote connection claim. | Application checks owner, live TTL, revision, source hash, idempotency and no active job; every risk flag blocks apply. PNG derivative is separate from the immutable original. | Preview/apply/undo/reprocess are explicit. Conflict or unknown request outcome is read back with the existing receipt; no automatic apply. |
| OCR: `internal/ocr/runtime`, `pkg/ocrplugin`, `internal/renderadmission` | Only entries in `config.OCR.Plugins` construct compiled factories; an unconfigured factory is never built. Worker defaults are operator preferences, not enabled document processing. | Registry freezes validated capabilities once; runtime separately checks factory identity at startup. Catalog reads do not rediscover or run providers. Destination is a sanitized public label; external processing is explicit. Metadata freshness/availability is distinct from remote connection health. | Application resolver selects exact provider/model, pins advertised version and approved output for the operation, then application admission checks owner/revision/TTL/limits/idempotency. Provider receives an immutable page allowlist and bounded job artifact access. | Unsupported, stale or failed selection rejects without provider/model fallback or probing. Replay precedes catalog lookup. Optional failure leaves a plain export available. |
| Structure: `internal/evaluation/structure.go` and existing source-linked OCR result UI | Evaluation only; no production structure provider is registered. [Recorded Docling attempt](structure-comparison.md): unavailable at isolation, actual exit32; defer/non-adoption. | Zero candidate measurements; quality, time/RSS/disk and complete installed/model-license closure remain unmeasured. Source-linked filename suggestion is generated content, not transcript or model-quality evidence. | Evaluation validates immutable artifact/hash/page/block/cell references; application-owned result routes authorize owner/session/TTL. Existing suggestion requires an explicit action and cannot adopt or rename a saved output. | Recorded exact unavailable disposition is the only implemented analyzer acceptance branch; successful/mixed evidence remains unimplemented. Generated content cannot fulfill transcript/structure/PDF completeness; no automatic adoption. |
| Delivery: Phase48 application adapter responsibility | Future fixed compiled, independently configured adapter; no delivery implementation, credentials or endpoint is added by Phase47. | Enablement, configured state, connection health and processing destination must remain distinct. A metadata row cannot mean connected or sent. | Application must own authorization, exact destination admission, revision/TTL/idempotency and the send/publication decision; adapter returns scoped status/output only. | Phase48 owns lost-response reconciliation and duplicate-send policy; no retry, success or external-send UI is implied here. |

## Authority and HTTP projection

`ocrplugin.Provider` returns capabilities and job-scoped results. It does not own
document authorization, retention, quotas, revisions, receipts, publication or
adoption. `artifactscope.Scope` assigns opaque output IDs, enforces the input
allowlist and byte bounds, seals staging and removes unreleased output on abort.
Sealing is not publication. The worker/coordinator and document store validate
the complete output and revision/expiry before publication; a completed reOCR
candidate leaves the prior saved output current until explicit whole-document
adoption (`internal/documents/ocr_results.go`, `ocr_result_commands.go`).

Authenticated `GET /api/capabilities` reads the worker catalog snapshot and
projects bounded public metadata with `Cache-Control: no-store`. It does not
receive document identifiers, payloads, provider credentials or private paths.
Queries are rejected. Unavailable/stale/malformed catalog failures use the same
content-free `provider_unavailable` response. Unconfigured providers and models
incompatible with ordinary PDF export are absent from processing UI metadata.
`GET /api/scanners` is independent of OCR catalog availability. Existing document
read/download routes likewise do not depend on optional OCR metadata.

Version and output are server-pinned admission decisions, not browser authority:
ordinary export supports the advertised PDF layer/complete-PDF contract; reOCR
can retain positioned/text artifacts before application PDF composition. This
explicit operation policy is not fallback to another provider/model/version.
The registry then rejects any unsupported exact version/language/preset/output.

## Excluded surfaces and proof limits

No Go shared objects, dynamic loading, arbitrary user URL/code, marketplace,
user executables, generic capability runner, provider-owned authorization or
provider-owned publication is introduced. Administrator-installed compiled code
remains trusted; the port is not an isolation boundary against hostile code.
Provider configuration stays worker-side and is never a browser credential API.

Focused registry/runtime/admission/HTTP tests exercise the actual owners. The
runtime performs a startup identity check as well as registry snapshot discovery;
neither accepts document data, and repeated metadata reads do not call Run.
The existing Plan03 browser receipt supplies phone/desktop baseline-save proof
for unchanged production/browser sources; it does not prove physical phones,
screen readers, native OCR quality or household-document quality. Phase46 remains
`human_needed`, 40/40 functional truths, with three unresolved nonblocking
advisories; no phase pass follows from this seam.
