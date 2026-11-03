# ADR 0001: Go API/worker and Astro static frontend

Status: accepted for the application stack; individual adapters remain under evaluation.

The owner requested Go and Astro and explicitly rejected Django. The approved screen prototype requires phone-first scanning, desktop side-by-side panels, document previews and editing, English/Japanese, and neutral light/dark themes.

Astro produces static routes and assets. Go embeds and serves those assets alongside a same-origin API. Node is a build tool, not a production server dependency. Interactive TypeScript modules obtain authorized projections from the API and send explicit commands. Persistent document and job state never lives in browser storage.

Go services are separated by interfaces for identity, documents, jobs, scanner control, PDF processing, OCR plugins, repositories, and retention. These boundaries do not require separate network services. The first deployment is a single application replica, with a worker process and bounded subprocesses for heavy work. No Redis or distributed queue is required by default.

Selected candidates to verify: standard `net/http`, SQLite through `database/sql`, coreos/go-oidc and golang.org/x/oauth2, and SCS with SQLite-backed sessions. Go 1.27.3 is the current stable toolchain checked for this design; dependency versions and checksums are pinned when introduced.

OIDC signature/issuer/audience/expiry validation uses an existing verifier. Nonce, one-time state, browser-session binding, and PKCE must be explicitly enforced; a successful library call alone is not application authentication. Guest sessions have independent principals and never expose a common public history. Auth mode is deployment-controlled and never silently falls back.

PDF and OCR engines remain replaceable. Python library experiments establish capability only and do not force a Python web server or Django. Go-native PDF libraries and existing CLI engines are compared separately. Stirling is optional, and PDF parsing/encoding is not reimplemented.

Evidence in the workspace plan: the Go/Astro static-build, embedded serving, and same-origin browser API spike passed. Authentication, worker, physical device, resource, and deployment acceptance are separate requirements.

Scope update: Paperless integration and personal connector settings are deferred. LLM OCR remains part of the independent OCR plugin boundary. The initial API contains no delivery endpoints.


Scanner parameter-read adapter update: a separate tagged Go/cgo helper links to official libsane for the standard parameter API unavailable through scanimage's non-acquiring CLI. The API/worker executable stays CGO-free and the canonical asset build sets CGO_ENABLED=0. No upstream source is modified or built. This localized native binding avoids copying driver geometry rules; its build/runtime packaging and production driver composition remain separate release gates. See [geometry boundary](../design/scanner-geometry.md).
