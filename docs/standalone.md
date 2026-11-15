# Single-file compatibility artifact

[Deployment reference](../deploy/README.md) · [Release artifacts](distribution.md)

Released Docker images are the recommended installation path. The onefile artifact is retained for internal compatibility and verification; this document does not establish a separate supported standalone installation path. Core and optional Tesseract service images run their executables and native runtime directly, without onefile extraction.

## Runtime behavior

On first execution, the launcher extracts the bundled runtime into `$XDG_CACHE_HOME/scanner-kiosk/native/`, or the OS user cache when unset. The cache must be writable and executable. Private directories use `0700` permissions; files and links are checked against the manifest, and detected changes cause failure. A single distributed file does not imply extraction-free or single-process execution.

Run the existing `serve` and `worker` commands separately with the same executable and the established authentication, scanner registry, identity, and state/document configuration. Bundled tool paths are set automatically; explicit supported environment overrides remain available.

## Build inputs

Use a previously verified application executable with embedded frontend assets and a provenance-bound native runtime. For branch/tag application builds:

```sh
python3 scripts/build-web.py --output /absolute/path/scanner --ref "$GITHUB_REF" --sha "$GITHUB_SHA"
```

A complete Git checkout and `origin/main` history are required. Only refs and commits allowed by the [release-channel contract](design/release-channels.md) may supply an identity; do not inject arbitrary version strings. Omitting both identity arguments produces a local development build.

To package an already built application without writing generated payload into the source tree:

```sh
python3 scripts/build-standalone.py \
  --app /absolute/path/scanner-with-embedded-frontend \
  --native /absolute/path/native-bundle \
  --native-manifest native/runtime-manifest.json \
  --output-dir /absolute/path/new-output-directory
```

Use the toolchain pinned by the repository/build configuration. The packager checks Go build metadata for `scanner-kiosk/cmd/scanner`, Linux amd64, disabled CGO, and the `webui` tag. A build tag alone does not verify frontend behavior. Every native file/link/size/mode/hash must match the reviewed manifest; unexpected files, missing OSD language data, or substituted inputs are rejected.

The launcher builds in a temporary directory and does not overwrite an existing output directory. Output includes `scanner-onefile-linux-amd64`, `LICENSE`, `manifest.json`, `SHA256SUMS`, and `build.json`. Checksums cover both the executable and project license; the receipt also records the license digest. Partial output without `build.json` is not a distribution input.

## Verification and release boundaries

The command neither acquires nor executes native materials. The compiler environment is restricted to build keys; launcher builds disable network acquisition and ambient workspace/Go flags. Packaging preserves the application's prebuilt Version/Revision. Generated payloads and manifests are excluded from Git.

Native provenance, corresponding source, notices, security findings, and redistribution review remain separate requirements. Reproducibility and matching checksums alone do not authorize publication. Consult the selected release's reports for retained advisories. Physical capture and recognition quality require their own acceptance; synthetic Japanese/English OCR does not establish device-specific accuracy.

Existing OCR verification accepts `scripts/check-tesseract.py --standalone-binary /absolute/path/scanner-onefile-linux-amd64`; use that script's `--help` for its other required arguments. The test worker starts with a fresh dedicated cache and automatic bundled paths, without native-path or host-PATH overrides. Python, Pillow, pikepdf, Poppler, and Japanese fonts used by the verification host are not requirements of the distributed runtime. Retain raw Go test logs alongside receipts.

Bundled notice material belongs under `usr/share/scanner-kiosk/notices/` in the native input, where the regular manifest checks also cover it. See [license and distribution boundaries](design/license-distribution.md#experimental-single-file-linux-amd64-package). Build Docker images through [image packaging](design/image-packaging.md) and `scripts/build-application.sh`; do not substitute onefile output into the root Dockerfile.
