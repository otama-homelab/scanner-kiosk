# Single-file distroless image packaging

The root `Dockerfile` is the canonical runtime image recipe. It copies the complete
Linux amd64 onefile produced by `scripts/build-standalone.py`; frontend, application,
SANE/airscan, Tesseract, language models and native notices belong inside that executable.
The project LICENSE accompanies the standalone distribution and is copied into the
image at `/usr/share/doc/scanner-kiosk/LICENSE`; it is not a runtime dependency.
It does not build an API-only binary or install runtime packages. Alpine is a native
build input, not the container runtime. The distroless base is pinned by digest.

Prepare a new, private build directory containing only the root `Dockerfile`, root
`.dockerignore`, the packager's verified `LICENSE` and the onefile renamed `scanner`. Verify the packager's
`SHA256SUMS` and provenance before copying. Do not put captured content or credentials
in the context. The ignore file denies everything except those four inputs.

```sh
docker build --platform=linux/amd64 --network=none --pull=false \
  --tag scanner-kiosk:local /path/to/prepared-context
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --memory=768m --pids-limit=128 \
  --tmpfs=/tmp:rw,exec,nosuid,nodev,size=512m,mode=1777 \
  scanner-kiosk:local version
```

The pinned base must already be available locally for an offline build. Missing
`scanner` or `LICENSE` fails the build; this recipe does not fetch or assemble native inputs.
Only the complete reviewed packager output is a valid input. Do not substitute
`build-web.py`'s inner application executable.

The image runs as UID/GID10001. Exec-form ENTRYPOINT owns `/usr/local/bin/scanner`;
CMD defaults to `serve`, and `worker` is an argument replacement. Writable private
state/document storage and an executable temporary cache are required. Compose and
Helm supply the cache and preserve onefile tool-path defaults. Provision storage
ownership/modes separately; image ownership cannot repair an existing mount.
See [deployment examples](../../deploy/README.md) and [standalone details](../standalone.md).

`deploy/standalone/Dockerfile` and `.dockerignore` are compatibility symlinks to the
root definitions. The previous source-building Debian candidate lives under
`deploy/debian`; its [historical checks](debian-image-packaging.md) and
`scripts/check-image.py` apply to that candidate only. Its scanner/OCR versions,
SBOM and checks do not approve the current distroless payload.

This is an unreleased packaging path. Complete native acquisition/rebuild/source
and license obligations, exact-candidate security checks, hosted CI/publication,
real service/storage/upgrade/rollback and physical scanner acceptance remain gates.
See [release channels](release-channels.md). No registry or deployment is implied.
