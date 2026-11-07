# Changelog

## 0.1.1

- Docker-first Linux amd64 deployment with separate API and worker roles, Compose examples and a Helm chart.
- Browser scanning through the unmodified sane-airscan backend, with bounded IPv4 Bonjour discovery, manual configuration and explicit scanner selection.
- Document and single-photo workflows, compact page previews, rotation, page ordering, deletion and PDF/image downloads.
- PDF import with existing text and vector content preserved, and resumable document editing after interrupted work.
- Optional OCR: a separate Tesseract API container or the lightweight LLM OCR client connecting directly to a configured service such as LiteLLM. Ordinary scanning and saving work without OCR.
- OIDC, local password and authentication-free modes, with private ownership, expiring sessions and documents, quotas and guarded job admission.
- Small footer showing the running application version.
- Mobile document overview and History grid/list views, direct PDF import and a single save action.
- Repository-linked core and Tesseract images, corresponding source and license materials, SBOMs, and documented storage, upgrade and rollback procedures.
