# Documentation

[Project overview](../README.md)

## Get started

- [Install with Docker](guide/getting-started.md): primary `docker run` workflow, storage, authentication, HTTPS, and first capture.
- [Use Docker Compose](guide/docker-compose.md): declarative alternative with the same runtime/storage contracts.
- [Use Scanner Kiosk](guide/usage.md): import, review, OCR candidates, export, and troubleshooting.
- [Use Paperless-ngx](guide/paperless.md): searchable text, OCR modes, and the direct-delivery compatibility boundary.

## Configure and operate

- [Deployment reference](../deploy/README.md): Compose, Kubernetes, authentication, OCR, and storage contracts.
- [Environment variables](../.env.example): available application settings.
- [Release artifacts](distribution.md): images, checksums, sources, licenses, and SBOMs.
- [Single-file compatibility artifact](standalone.md): internal build and verification scope.

## Develop and understand

- [Development](development.md): toolchains, source ownership, tests, and change expectations.
- [HTTP API](https://github.com/otama-homelab/scanner-kiosk/blob/main/api/openapi.json)
- [Design references](https://github.com/otama-homelab/scanner-kiosk/tree/main/docs/design)
- [Architecture decisions](https://github.com/otama-homelab/scanner-kiosk/tree/main/docs/adr)
- [Screenshot provenance](screenshots/README.md)
- [Third-party notices](../THIRD-PARTY-NOTICES.md)

The maintained overview and user/operator guides are in English. `docs/design/`, `docs/adr/`, and research subdirectories retain their original technical and historical records, which may use Japanese. A prototype, rejected model, or evaluation result in those records is not a supported product feature. Start with the current guides for installation and use.
