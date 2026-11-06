# Scanner kiosk

A browser-based scanner for phones and desktops. Scan documents, import PDFs,
insert or replace pages, rotate/reorder/delete pages, and download PDF or image
outputs. Documents belong to individual users and expire automatically.

Linux amd64 distribution includes the frontend, Go application, SANE/airscan,
Tesseract, and Japanese/English OCR data in one executable. Native tools unpack
into a private, integrity-checked executable cache; no host Node, SANE or
Tesseract installation is required. The container uses distroless.

**Status:** unreleased. Automated and device checks cover implemented workflows;
real provider/mobile acceptance, final distribution review and production cutover
remain incomplete. This repository does not imply a production-ready release.

## Run and configure

Use the same executable for both roles:

```sh
scanner serve
scanner worker
```

`serve` provides the frontend, authentication and HTTP interface. `worker` handles
scanning, OCR, PDF generation and cleanup. Both use the same private state and
document stores and must run with matching OS identities. Configure environment
variables explicitly; copying `.env.example` does not make the binary load it.

- [Standalone packaging and build](docs/standalone.md)
- [Compose and standard Kubernetes deployment](deploy/README.md)
- [Environment settings](.env.example)
- [Local user/password setup](docs/design/simple-authentication.md)
- [OIDC configuration and HTTPS proxy](docs/design/api-runtime.md)
- [UID/GID, storage, backups and resource requirements](docs/design/homelab-readiness.md)
- [Identity recovery](docs/design/state-recovery.md)

Use HTTPS for browser access. Configure either OIDC or local username/password
authentication. State and temporary documents use separate directories; exclude
the document store from backups. Existing volume ownership is the operator's
responsibility. Deployment examples default to UID/GID 10001, API 512 MiB and
worker 1536 MiB; resource needs depend on scan size and encoding.

Local OCR works without an external service. Optional LLM OCR sends document
content to the configured provider; credentials stay on the worker. Paperless
delivery is deferred; the initial workflow ends at downloading the output.

## Develop

The application uses Go and Astro. Astro produces static assets embedded in the
Go application. See [frontend build](docs/design/product-web.md),
[processing and OCR](docs/adr/0002-processing-and-ocr-ports.md), and the
[OpenAPI contract](api/openapi.json).

```sh
GOTOOLCHAIN=go1.27.3 go test -race ./...
```

The CI workflow builds the native runtime, frontend, onefile and distroless image,
and produces bound source, license, inventory and security materials. Main builds
are previews; SemVer tags select releases. Publication is separately gated:
[release channels](docs/design/release-channels.md).

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

Runtime source lives in `cmd/`, `internal/`, `pkg/` and `web/`. `scripts/` owns
build and verification commands; `native/` retains locked inputs, notices and
rebuild materials. [Native build documentation](native/README.md) explains these
inputs. Tests stay beside the code they verify. Local acceptance records and
personal deployment settings are kept outside version control.

## Security and license

See the [threat model](docs/design/threat-model.md),
[dependency inventories](docs/design/dependency-security.md), and
[source distribution](docs/design/source-distribution.md).

Project-owned code and assets use [Apache-2.0](LICENSE), unless otherwise marked.
Third-party components retain their licenses and redistribution conditions:
[notices](THIRD-PARTY-NOTICES.md) and
[distribution requirements](docs/design/license-distribution.md).
