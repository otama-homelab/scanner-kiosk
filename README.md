# Scanner kiosk

[Source repository](https://github.com/otama-homelab/scanner-kiosk) · Apache-2.0

A browser-based scanner for phones and desktops. Scan documents, import PDFs,
insert or replace pages, rotate/reorder/delete pages, and download PDF or image
outputs. Documents belong to individual users and expire automatically.

Docker is the primary Linux amd64 deployment path. The distroless core image
contains the frontend, Go application and SANE/airscan, with OCR disabled by
default. Tesseract and Japanese/English dictionaries live in an optional terminal
API container. The lightweight LLM OCR plugin is compiled into the core and
connects directly to LiteLLM or a configured Chat Completions endpoint. No local
model, inference SDK or extra LLM adapter service is required.

**Status:** unreleased. Automated and device checks cover implemented workflows;
real provider/mobile acceptance, final distribution review and production cutover
remain incomplete. This repository does not imply a production-ready release.

## Run and configure

The core image uses the same executable for both roles:

```sh
scanner serve
scanner worker
```

`serve` provides the frontend, authentication and HTTP interface. `worker` handles
scanning, OCR, PDF generation and cleanup. Both use the same private state and
document stores and must run with matching OS identities. Configure environment
variables explicitly; copying `.env.example` does not make the binary load it.

- [Compose and standard Kubernetes deployment](deploy/README.md)
- [Environment settings](.env.example)
- [Local user/password setup](docs/design/simple-authentication.md)
- [OIDC configuration and HTTPS proxy](docs/design/api-runtime.md)
- [UID/GID, storage, backups and resource requirements](docs/design/homelab-readiness.md)
- [Identity recovery](docs/design/state-recovery.md)

Use HTTPS for browser access. Choose OIDC, local username/password, or explicit
`SCANNER_AUTHENTICATE_TYPE=none` for private browser sessions without a login screen. State and temporary documents use separate directories; exclude
the document store from backups. Existing volume ownership is the operator's
responsibility. Deployment examples default to UID/GID 10001, API 512 MiB and
worker 1536 MiB; resource needs depend on scan size and encoding.

For LAN plug and play on Linux Docker, use `deploy/compose.discovery.yaml` alongside
`deploy/compose.yaml`. The worker discovers IPv4 Bonjour eSCL scanners on its LAN,
including devices switched on after startup. One device is selected automatically;
multiple devices can be selected in Home. Manual registrations remain supported.
See [discovery networking and limits](deploy/README.md#scanner-discovery).

Without OCR configuration, ordinary PDF operations remain available and OCR
controls stay hidden. Optional Tesseract OCR sends images to its configured API
container; LLM OCR sends them directly to its configured provider. Credentials
stay on the worker and the selected OCR service, outside the browser/API role. Paperless
delivery is deferred; the initial workflow ends at downloading the output.

## Development container images

Successful main pushes publish two images in the same repository-linked package:

| Image | Immutable tag | Moving development tag |
| --- | --- | --- |
| Core (`serve` / `worker`) | `sha-<full-source-commit>` | `preview` |
| Optional Tesseract API | `tesseract-sha-<full-source-commit>` | `tesseract-preview` |

Package: `ghcr.io/otama-homelab/scanner-kiosk`.
The image is linked to this repository through its OCI source label and the
repository's Actions token. These are development previews, not G5-accepted
production releases. They never move `:latest` or open the version-tag release gate.

```sh
docker pull ghcr.io/otama-homelab/scanner-kiosk:preview
docker run --rm ghcr.io/otama-homelab/scanner-kiosk:preview version
```

GHCR controls package visibility separately from repository visibility. A newly
created package starts private; authenticated pulls are required until its owner
makes it public in package settings. The workflow does not change that access
policy or billing settings. Production deployments should pin the verified image
digest, rather than a moving preview tag.

Before pushing the image, the publisher retains corresponding source, third-party
notices, inventories and raw inspection reports in a source-only prerelease named
`preview-source-<full-source-commit>`. Image tarballs remain CI artifacts until
formal release acceptance. The compatibility onefile stays internal in every
channel. The manifest and checksums cover the complete CI candidate, including
omitted artifacts; verify the entries for the files you download. Native bytes must match the reviewed
manifest and every advisory finding must match the retained review; new findings
or changed native payloads stop automatic publication. Raw advisory failures are
retained, not relabeled as a passed production security review.

## Develop

The application uses Go and Astro. Astro produces static assets embedded in the
Go application. See [frontend build](docs/design/product-web.md),
[processing and OCR](docs/adr/0002-processing-and-ocr-ports.md), and the
[OpenAPI contract](api/openapi.json).

```sh
GOTOOLCHAIN=go1.27.3 go test -race ./...
```

The CI workflow builds the native runtime, frontend, internal compatibility onefile
and both distroless images,
and produces bound source, license, inventory and security materials. Main builds
are development image previews; SemVer tags select G5-gated formal releases:
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
