# Changelog

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
