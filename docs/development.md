# Development

[Overview](../README.md) · [Documentation](README.md)

Scanner Kiosk uses a Go backend and an Astro/TypeScript frontend. Static frontend assets are embedded in the Go executable. `go.mod` pins the Go toolchain; build configuration and `web/package-lock.json` pin frontend tooling and dependencies.

## Source layout

```text
cmd/                 Executable entry points and role composition
internal/            API, worker, documents, OCR, and PDF implementations
pkg/ocrplugin/       Public OCR provider contract
web/                 Astro pages, TypeScript, and styles
api/                 HTTP API contract
deploy/              Compose and Kubernetes examples
docs/guide/          Installation and user workflows
docs/screenshots/    Real UI captures using synthetic data
docs/design/         Design, implementation, and evaluation records
docs/adr/            Architecture decisions
native/              Pinned native inputs and rebuild materials
scripts/             Build, verification, and distribution tooling
tests/               Cross-cutting test assets
```

Keep tests next to the code they exercise. Credentials, private configuration, private documents, and local progress records do not belong in distributed source.

## Module ownership

| Location | Responsibility |
| --- | --- |
| `cmd/scanner` | CLI, signals, and role-specific configuration |
| `internal/apiruntime` | HTTP, authenticated sessions, and API startup |
| `internal/workerruntime` | Startup consistency, job execution, and shutdown |
| `internal/documents` | Ownership, revisions, retention, and durable job state |
| `internal/scanner` / `internal/scannerdriver` | Device validation, acquisition, and owned-attempt cleanup |
| `internal/ocr` / `pkg/ocrplugin` | Provider implementations and OCR contracts |
| `internal/pdftextlayer` / `pdfassemble` | Text geometry and PDF assembly |
| `internal/staterecovery` | Identity backup/restore; excludes documents and sessions |

## Build and check

For focused OCR/PDF changes:

```sh
go test ./pkg/ocrplugin ./internal/ocr/llm ./internal/pdftextlayer
python3 -m unittest discover -s scripts -p 'test_prepare_candidate.py'
python3 scripts/build-web.py --output /tmp/scanner
```

Use the [pinned CI workflow](https://github.com/otama-homelab/scanner-kiosk/blob/main/.github/workflows/checks.yml) for full race, native PDF runtime, browser, release-profile, and security checks. Some integration tests require explicitly supplied native/browser dependencies. Passing a focused package does not establish physical-scanner or owner-device acceptance.

## Change expectations

- Reproduce a behavior defect before repairing its owning layer. Update callers, fixtures, configuration, and documentation affected by a contract change.
- Preserve ownership checks, input validation, original artifacts, explicit candidate adoption, retention, and failure recovery.
- Keep UI text in the existing English/Japanese message catalogs. Do not add credentials or private content to logs, screenshots, or fixtures.
- Document prerequisites, runnable commands, expected outcomes, compatibility boundaries, and material limitations in maintained English guides.
- Include the problem, resulting behavior, and relevant verification in a change description. Keep historical failed measurements distinct from later passes.

## Releases

Main-branch images are development previews. Stable releases bind exact source, tests, licenses, SBOMs, security reports, and reviewed candidate materials. Do not rewrite published artifacts or treat a successful build as publication approval. See [distribution](distribution.md).
