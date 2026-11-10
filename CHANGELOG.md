# Changelog

## 0.2.6

- Verify photo child dispatch with the production executable under its unchanged parser limits; retain race coverage for normalization code.
- Surface only bounded public package acquisition diagnostics on native build failure.
- Keep sandbox smoke tests on the same explicitly authorized Chromium executable as the browser suite.
- Earlier v0.2.x tags were unreleased CI candidates.

## 0.2.4

- Give the rapid viewport/reload test matrix its own explicit API rate budget while preserving production limits and separate rate-limit coverage.
- Wait for actual photo-input readiness before browser tests set hidden file inputs; retain response-status diagnostics for failures.
- Verify sandboxed Chromium startup before running the full CI race suite.

## 0.2.3

- Allow the pinned test Chromium executable to create its sandbox on Ubuntu AppArmor runners; keep Chromium sandboxing enabled.
- Serialize only the two integration packages that share frontend builds; retain race checks across every Go package.
- Earlier v0.2.x candidate tags did not produce a published release.

## 0.2.2

- Align corresponding-source recipe roles with the native acquisition contract and validate the actual production lock.
- Create browser-test receipts on clean checkouts and serialize shared frontend build tests.
- Refresh the exact native advisory review using clean-build evidence; preserve raw findings and mandatory source vulnerability checks.
- v0.2.0 and v0.2.1 were unsuccessful CI candidates and were not published.

## 0.2.1

- Prepare the verified native PDF runtime, Japanese font and pinned Chromium/Playwright dependencies before CI integration tests.
- Preserve safe package-specific diagnostics when locked APK acquisition fails.
- v0.2.0 candidate failed CI and was not published; this release includes its document-processing and UX changes below.

## 0.2.0

- Preserve original documents while previewing, applying and undoing optional image corrections. Normal fixed-area scanning and no-correction defaults remain.
- Add revision-bound reOCR with source-linked comparison, explicit adoption/discard and cancellation/failure recovery.
- Import JPEG/PNG photos with immutable originals, orientation normalization, manual crop/rotation and consistent preview, PDF Save and reOCR pixels. Automatic perspective, shadow and curved-page correction are not included.
- Add explicit reviewed-PDF Paperless delivery with a durable single-attempt ledger, asynchronous task/checksum reconciliation and safe unknown-result recovery.
- Keep Save primary across phone/desktop scan, PDF and photo workflows; add source-linked filename suggestions.
- Record reproducible correction/OCR evaluations and browser/native/monetization decisions. Heavy candidate adoption, new clients and billing remain deferred.

## 0.1.4

- Docker-first Linux amd64 deployment with separate API and worker roles, Compose examples and a Helm chart.
- Separate `scanner-kiosk` and `scanner-kiosk-tesseract` image packages, each with its own version and preview tags.
- LLM OCR uses the configured processing timeout while waiting for slow responses.
- Browser scanning through the unmodified sane-airscan backend, with bounded IPv4 Bonjour discovery, manual configuration and explicit scanner selection.
- Document and single-photo workflows, compact page previews, rotation, page ordering, deletion and PDF/image downloads.
- PDF import with existing text and vector content preserved, and resumable document editing after interrupted work.
- Optional OCR: a separate Tesseract API container or the lightweight LLM OCR client connecting directly to a configured service such as LiteLLM. Ordinary scanning and saving work without OCR.
- OIDC, local password and authentication-free modes, with private ownership, expiring sessions and documents, quotas and guarded job admission.
- Small footer showing the running application version.
- Mobile document overview and History grid/list views, direct PDF import and a single save action.
- Repository-linked core and Tesseract images, corresponding source and license materials, SBOMs, and documented storage, upgrade and rollback procedures.
