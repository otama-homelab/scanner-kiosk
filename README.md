# Scanner kiosk

Browser-based scanner for phones and desktops, with OIDC, local username/password login or legacy isolated guest sessions, temporary per-user documents, PDF editing, pluggable Tesseract/LLM OCR.

The application uses Go and Astro. Astro builds static assets served by Go; document, job, ownership, and retention state belongs on the server. Deployment settings come from environment variables.

Development status: the Go API/worker and English/Japanese Astro frontend connect OIDC/simple authentication, owner-isolated history, scan admission, capture previews, page editing, PDF/image/OCR rendering, downloads and expiry/deletion cleanup. Isolated native fixtures and browser tests cover implemented paths. Physical readiness/recovery, real scanner/mobile/IdP/LLM acceptance, complete storage backend separation and release/cutover remain pending. Named none authentication remains pending. The existing scanner service has not been replaced.

- [Stack decision](docs/adr/0001-go-astro.md)
- [State and storage design](docs/design/state-storage.md)
- [Local credentials and account commands](docs/design/simple-authentication.md)
- [Logical identity export and fresh-state recovery](docs/design/state-recovery.md)
- [PDF and OCR decision](docs/adr/0002-processing-and-ocr-ports.md)
- [OCR extension contract](docs/design/ocr-plugins.md)
- [Job and worker transitions](docs/design/jobs-worker.md)
- [Threat model](docs/design/threat-model.md)
- [Deployment and release contract](docs/design/operations-release.md)
- [Trunk previews and tagged releases](docs/design/release-channels.md)
- [Local image packaging and verification](docs/design/image-packaging.md)
- [Dependency inventory and security scan evidence](docs/design/dependency-security.md)
- [HTTP runtime and local startup](docs/design/api-runtime.md)
- [Product frontend build and verification](docs/design/product-web.md)
- [Job status and cancellation API](docs/design/job-http.md)
- [Render admission and worker connection](docs/design/render-http.md)
- [OCR catalog selection boundary](docs/design/ocr-catalog.md)
- [Worker catalog storage](docs/design/worker-catalog-storage.md)
- [Registered scanner driver](docs/design/registered-scanner-driver.md)
- [Advertised scanner identity binding](docs/design/scanner-identity.md)
- [Physical writer handoff preflight and candidate scope](docs/design/physical-writer-fencing.md)
- [Verified scanner settings](docs/design/scanner-metadata.md)
- [Public capability API](docs/design/capability-http.md)
- [Deployment design examples](deploy/README.md)

## License

Project-owned code and assets use [Apache-2.0](LICENSE), unless otherwise marked.
Third-party components retain their own licenses and redistribution conditions.
See the [license decision](docs/adr/0005-project-license.md) and
[distribution review](docs/design/license-distribution.md).

## Intended layout

- `cmd/scanner/`: API server and worker commands.
- `internal/`: application services, repositories, authorization, adapters, and cleanup.
- `pkg/ocrplugin/`: versioned OCR extension contract.
- `web/`: Astro pages and TypeScript interaction modules.
- `api/`: OpenAPI contract.
- `native/`: [Build-only native input locks](native/README.md); runtime containers remain distroless.
- `deploy/`: standalone deployment examples; homelab-specific configuration lives in the infrastructure repository.
- `scripts/`: compiled-command, browser and independent artifact verification; Go tests live beside their packages.

Local repository only. Remote hosting and publication are not configured.

Paperless delivery is deferred. The initial release ends at local download; any later document organization or agent-driven delivery remains outside the initial scanner workflow. LLM OCR remains in scope.

## Module ownership

The CLI composes modules; it does not own their transaction or process-lifecycle
rules. Use the existing interfaces when changing behavior:

| Module | Interface and ownership | Change here for |
|---|---|---|
| `cmd/scanner` | Command selection, signal context, role-specific configuration | CLI commands and process entry |
| `apiruntime` | `New`/`Run`, HTTP/session composition and request budgets | API startup and HTTP wiring |
| `workerruntime` | `Run`, boot identity, reconciliation before dispatch, shutdown | Worker lifecycle and scheduling |
| `documents` | Durable owner/revision/TTL/job/lease transitions | Document authorization and transactional invariants |
| `scanner` / `scannerdriver` | Validated capture plans and owned attempts; native adapters behind the seam | Device capability/capture behavior and confirmed cleanup |
| `pkg/ocrplugin` / `internal/ocr` | Provider contract and worker-side implementations | OCR models/providers; preserve owner/publication checks outside providers |
| `staterecovery` | `Run`/`Export`/`Restore`, identity-only archives and fresh destination publication | Configuration recovery; sessions/documents remain excluded |

For the current Linux/amd64, CGO-disabled, `webui` source configuration,
`go list -deps -json -tags=webui ./...` completed across 53 local packages and
143 local import edges. API and recovery imports do not reach scanner/OCR
provider implementations or `workerruntime`; the worker owns those implementations.
This is a source dependency check, not physical-readiness or deployment evidence.
The bundled launcher, CGO helper and browser source require their respective
configuration checks. The parent plan retains the exact graph and input hashes
in `current-module-import-graph-20261104/current-evidence.json`.

## Current verification

```sh
GOTOOLCHAIN=go1.27.3 go test -race ./...
```

This checks SQLite startup/migrations, ownership and retention, revision-safe processing, scanner lease fencing, server sessions/OIDC, and implemented document HTTP routes. Isolated compiled-command, synthetic PDF and opt-in real Tesseract checks are described below. Physical scanner, complete frontend workflow, live proxy/IdP and deployment acceptance remain pending.

The OpenAPI 3.1 contract in `api/openapi.json` defines 21 initial operations and has been validated with openapi-spec-validator 0.9.0. Implemented routes are described in the HTTP runtime and document design; remaining endpoints and broader response-conformance coverage are pending.

Design verification is reproducible with:

```sh
python3 scripts/check-design.py --validator-python /path/to/openapi-validator-venv/bin/python --receipt /tmp/scanner-design-check.json
```

The validator environment needs openapi-spec-validator 0.9.0 and PyYAML. Helm and Docker Compose are used only for rendering/configuration checks; no daemon, image pull or deployment is performed. The receipt records the current Go test count across the implemented components.

## Implemented components and remaining work

The [worker runtime](docs/design/worker-runtime.md) dispatches PDF, photo JPEG/PNG and native PDF-text-layer OCR renders under a fresh boot lock. [Render admission](docs/design/render-http.md) stores immutable jobs, reservations and replay receipts through authenticated HTTP. [Disk capacity](docs/design/disk-capacity.md), cancellation/authorization monitoring, confirmed-stop completion and publication read-back guard processing. Durable restart reconciliation, cleanup/GC and production scanner dispatch remain unfinished.

The [OCR plugin port](docs/design/ocr-plugins.md) separates provider/model/language/preset selection from document ownership and output publication. The Tesseract provider has real Japanese/English CLI and multi-page PDF verification. The LLM Chat Completions adapter has local mock integration tests; positioned-text rendering and live model acceptance remain pending. [OCR result binding](docs/design/ocr-result-binding.md), [artifact scopes](docs/design/artifact-scope.md), [stage output budgets](docs/design/artifact-budget.md) and [native staging reservations](docs/design/ocr-native-reservation.md) constrain provider inputs/results. Credentials remain in the worker environment.

[Worker catalog publication](docs/design/worker-catalog-storage.md) exposes leased model metadata to the API without provider clients. [Catalog resolution](docs/design/ocr-catalog.md) pins exact versions/presets and checks trusted page/pixel bounds against immutable validated dimensions before admission. [GET /api/capabilities](docs/design/capability-http.md) projects selectable OCR models, effective limits and sanitized scanner labels; scanner settings are still unknown until their worker publication is connected. The output dialog consumes selectable models and the worker's configured default selection, with editable languages/quality and explicit external-processing notice. Browser OCR admission has fixture coverage and optional real Tesseract daemon-to-browser-download verification with independent text/pixel checks. Live LLM, physical scanner/mobile and deployment acceptance remain pending.

The [PDF assembler](docs/design/pdf-assembler.md) preserves JPEG streams or lossless image pixels, page order, paper sizes, rotation and bound OCR layers. The [image exporter](docs/design/image-export.md) produces single JPEG/PNG outputs. Independent verification uses `scripts/check-pdf.py`, compiled worker verification uses `scripts/check-worker.py`, and opt-in real Tesseract verification uses `scripts/check-tesseract.py`; each writes a receipt describing its scope.

[Scan admission](docs/design/scan-admission.md) and [completion](docs/design/scan-completion.md) implement transactional capture records, lease epochs, fresh stop/idle evidence requirements and uncertainty read-back. The [SANE adapter](docs/design/scanner-adapter.md), including acquisition-free selected-option and [native parameter inspection](docs/design/scanner-geometry.md), and [claimed scan executor](docs/design/scan-executor.md) connect actual native capture to validated originals/pages using a test backend. [Image validation](docs/design/captured-image-validation.md) derives actual file hashes and dimensions in the worker. The [registered driver](docs/design/registered-scanner-driver.md) now connects selected constraints, native parameters, one-use capture plans and the idle observer to the executor in tagged virtual-backend tests. Environment-fed driver/configuration ownership is implemented as a guarded Session constructor; worker startup invocation, scanner capability publication, scan HTTP admission and boot-pinned dispatch are connected. Trusted physical readiness production and Canon idle/recovery acceptance remain pending. Test fixtures do not establish safe physical takeover.

The [HTTP runtime](docs/design/api-runtime.md) serves optional OIDC or isolated guest sessions, owned history/detail, page edits, previews, current output download/Range, render admission and job status/cancel. Local TLS fixtures verify OIDC protocol/identity binding; actual IdP and production proxy acceptance remain pending. The [Astro frontend](docs/design/product-web.md) embeds into the Go binary and connects sessions, history, review/rotation, output settings, render jobs and native downloads. Synthetic browser fixtures cover desktop/mobile viewport sizes, themes and Japanese/English. Capture/scanner selection, page ordering/removal, thumbnails and document deletion are also connected with fixture coverage. Real mobile/accessibility and the physical scanner workflow remain pending.

An experimental [single-file Linux amd64 package](docs/standalone.md) embeds the frontend, application and native OCR/scanner runtime, extracting it into a private verified cache without host package installation. It remains unreleased pending native dependency, license and physical-device acceptance.

The standalone deployment examples are design-validated only. Image publication, clean install/upgrade/rollback acceptance, deployment and scanservjs replacement remain pending.

Multiple-device concurrency contract and remaining integration:
[Multiple scanners](docs/design/multiple-scanners.md).

Trusted readiness evidence and transactional scan admission boundary:
[Scanner readiness](docs/design/scanner-readiness.md).

Authenticated native scan command boundary: [Scan HTTP](docs/design/scan-http.md).

OCR-independent scanner discovery and advisory state: [Scanner status](docs/design/scanner-status.md).

Capture/scanner-selection client and fixture scope: [Capture UI](docs/design/capture-ui.md).

Server-derived active work and constant-query history: [Active job/history](docs/design/active-job-history.md).

Review page editing: [design and acceptance](docs/design/review-page-editing.md).

Thumbnail worker: [generation boundary and remaining integration](docs/design/thumbnails.md).

Document deletion: [deletion, expiry and recovery boundaries](docs/design/document-deletion.md).

Artifact GC: [quarantine and durable completion](docs/design/artifact-gc.md).

Cleanup worker: [dispatch and remaining recovery](docs/design/cleanup-worker.md).
