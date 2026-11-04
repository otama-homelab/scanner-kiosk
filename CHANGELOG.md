# Changelog

## Unreleased

- Linux amd64 executable with the web frontend, SANE, Tesseract and OCR data included.
- Browser scanning, page review and searchable PDF generation, including rotated-document correction.
- Distroless image with `serve` and `worker` commands; standard Kubernetes and Compose deployment examples.
- Unmodified official sane-airscan backend, without a maintained fork or separate airscan service.
- UID/GID-aware storage settings and documented backup, upgrade and rollback procedures.

These describe the development candidate. Physical-device, storage, mobile/provider,
release and cutover acceptance remain tracked in `.planning/`; no stable release
has been published.
