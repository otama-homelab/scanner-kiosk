# Docker runtime image packaging

The canonical recipes are root `Dockerfile` (core `serve` / `worker`) and
`Dockerfile.tesseract` (optional terminal OCR API). Both use the same digest-pinned
distroless base, UID/GID 10001 and exec-form entrypoints. The core contains no
Tesseract executable, libtesseract or dictionaries. The compiled LLM HTTP adapter
remains in the core; models and inference run at the configured endpoint.

`scripts/build-application.sh` owns the complete build. The verified native input
is partitioned by ELF interpreter/library closure, including the SANE airscan
dlopen backend. Both roots retain source provenance and the complete small notice
set; a Tesseract license filename in the core is not an embedded OCR engine.

`prepare-runtime-images.py` creates allowlisted contexts from the ordinary Go app
binary, separate service binary, role native roots, manifests, provenance, project
LICENSE and source recipes. The internal compatibility onefile is built and tested
separately; it is never copied into either runtime image. Do not use the repository
or a home directory as an image build context.

The base uses `/lib -> usr/lib`; recipes copy logical `native/lib` files into
`/usr/lib` and preserve the base symlink. After each version smoke, the pipeline
exports that stopped container's final rootfs and verifies installed bytes, modes,
owners and links against its assigned inputs. An offline inventory stage has no
Docker socket. Saved-image layer/config hashes and SBOMs are verified separately.
The service's build-linked native manifest digest must equal the actual payload;
its model revision also includes implementation source identity.

The material stager binds both image IDs, archives, versions, payload receipts,
role manifests/provenance and inventories to the same source and verified parent
native payload. Corresponding source/licenses precede registry publication. Both
immutable image pushes are checked before moving development aliases. The latest
source owning either preview variant prevents an older run rolling either back,
including after a partial alias update. G5/formal/latest/cutover remain separate.

Runtime images need no extraction cache. Keep private state/document storage and
role scratch separate, preserve selected UID/GID across updates and never replace
stores merely to update an image. See [deployment examples](../../deploy/README.md)
for OCR-off, optional Tesseract and direct LLM configurations and migration.

These are unreleased packaging candidates until actual image/native/storage and
acceptance evidence exists. Source-level tests do not establish image size, RSS,
OCR quality or physical scanner acceptance.
