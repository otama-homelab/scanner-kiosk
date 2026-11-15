# Release artifacts

[Overview](../README.md) · [Installation](guide/getting-started.md)

Use files and images from the **same [GitHub release](https://github.com/otama-homelab/scanner-kiosk/releases/latest)**. Verify downloaded checksums and pin image repository digests for reproducible deployments.

## Choose an artifact

| Artifact | Purpose |
| --- | --- |
| `scanner-deployment.tar` | Compose/Kubernetes examples, environment reference, README, user guides, screenshots, and licenses. |
| Core image | Embedded frontend, Go API/worker, and SANE/airscan. Both `serve` and `worker` use this image. |
| Tesseract image | Optional OCR API with Tesseract and Japanese/English language data, running separately from the core. |
| `SHA256SUMS` and `manifest.json` | File identities and the release/source/image configuration relationships. |
| Corresponding source, notices, SBOMs, and security reports | License review, rebuilding, dependency inventory, and advisory findings. |
| Single-file artifact | Internal compatibility/verification packaging. Read its [scope](standalone.md); it is not the recommended installation path. |

## Images and channels

- Core: `ghcr.io/otama-homelab/scanner-kiosk`
- Tesseract API: `ghcr.io/otama-homelab/scanner-kiosk-tesseract`

Stable tags use `vMAJOR.MINOR.PATCH`. `latest` aliases the current stable release; `preview` and `sha-<commit>` are development channels. Pin the release's **repository digest** for deployment. An image configuration ID is a different identifier and cannot be substituted for that digest.

## Deployment archive layout

```text
README.md                 Features and installation entry point
LICENSE                   Project license
THIRD-PARTY-NOTICES.md     Third-party notices
.env.example              Configuration reference
deploy/                   Deployment examples
docs/guide/               Installation, usage, and integrations
docs/screenshots/         Documentation screenshots and provenance
docs/                     Distribution and development guides
```

The deployment archive is generated from the corresponding source. Its inventory binds included files and hashes. Internal research records are excluded from the deployment bundle; corresponding source materials are distributed separately.

Documentation updates enter a new release built from that source. Published immutable assets are not edited in place.
