# Distribution boundaries and open license work

Project-owned code uses **Apache-2.0**, selected on 2026-11-04 under
[ADR 0005](../adr/0005-project-license.md). Third-party terms remain separate.
This document records observed distribution boundaries; it does not approve a
release or replace exact license texts. Dated observations below that describe
the project license as undecided predate this decision.

| Delivered material | Current evidence | Remaining work |
| --- | --- | --- |
| Go executables | Runtime Docker stage copies `scanner` and the CGO `sane-parameters` helper. The build collects indexed Go module and standard library notices. | Include the selected project license; check each module's actual terms, generated code and native linkage; bind notices and corresponding source to the release digest. |
| Browser JavaScript | Observed Astro client build contains seven project TypeScript modules; its single generated chunk has no static or dynamic imports. All five built assets match the embedded assets and prior candidate image delivery hashes. | Review copied/generated material and authorship. Repeat module and asset checks when build inputs or dependencies change. |
| HTML, CSS, SVG and inline/public JavaScript | `index.html` is generated from the project Astro page. `logo.svg`, `style.css` and `theme.js` are byte-identical copies of `web/public`. | Confirm source/rights of the logo, icons, styling and translations; generated HTML is not covered by the JavaScript module inventory. |
| Node/Astro/npm build tooling | Temporary prerender chunks contain Astro and other npm modules; they are absent from the five final static assets. Docker copies `web/dist` into the Go build, without copying Node or `node_modules` into runtime. | Keep the build-stage inventory and unresolved archive notices. Review any source, build-stage image or dependency bundle actually distributed; do not treat every lockfile package as browser-delivered code. |
| Debian native tools, libraries and OCR data | Runtime installs SANE, sane-airscan, Tesseract, English/Japanese data and their dependencies; distro notices are retained and checked. | Review exact package copyright files, exceptions, corresponding-source and replacement/relinking requirements. Supplementary repository notices cannot substitute for bundled native dependencies. |
| Optional OCR font | Supplied separately through administrator configuration; no font is included in the current candidate image. | Record the actual font's license and redistribution/embedding conditions before distributing it or approving OCR output behavior. |

## Current single-file notice updates

The current distribution uses a native closure inside one executable and a
distroless image; the Debian inventory below is historical evidence. The native
manifest now selects verbatim Leptonica 1.87.0 and libjpeg-turbo 3.1.3 notices, the
IJG attribution, and source-association records from `native/notice-files/`.
The assembler validates those files against the manifest before packaging.
See [source-managed notices](../../native/README.md#source-managed-notices).
Other native component obligations, replacement/relinking materials, recipient
source access remain under review; the project license is now Apache-2.0. This update
does not approve distribution or establish full notice completeness.

The same source-managed path now also retains root notices for12 transitive
packages: Brotli, GIFLIB, libffi, libpng, WebP, libxml2, musl, p11-kit, LibTIFF,
XZ, zlib and Zstandard. LibTIFF carries explicit Berkeley attribution. Exact
source/member hashes and runtime associations are recorded per package. A root
notice is not sufficient evidence of per-file coverage: libxml2 excludes dict.c
and list.c from its common copyright, musl identifies third-party source terms,
libffi refers to individual files, and p11-kit's root COPYING omits copyright
attribution. Their source records retain these open checks. No complete
component or release license approval is inferred from the new files.

## Browser build observation, 2026-11-03

The workspace plan's `frontend-delivery-graph-20261103/receipt.json` and
`module-graphs.json` record an observer-only build using the installed Astro
7.4.4 and Node 24.22.2. The normal Astro config was imported unchanged. A Vite
`generateBundle` hook recorded both prerender and client chunks without editing
their contents; the build used a temporary output directory.

The generated client chunk contains `app.ts`, `capture.ts`, `deletion.ts`,
`login.ts`, `ocr.ts`, `request-key.ts` and `review.ts`. The Astro page's script
entry has zero rendered length. No npm module contributes rendered code to that
chunk, and it has no external or deferred imports. The three public assets are
copied unchanged. All five output hashes equal both the current embedded assets
and `browser_verified_served_assets` in the prior image validation receipt for
`sha256:829cf7e4799d028e5f3f9c0270d6da33d986793eb461ed00dc75885647f6f26c`.
This is a comparison to retained image-delivery evidence, not a new image run.

Consequently the known npm archive notice gaps (12 installed packages, 49
lockfile entries) are not evidence that those packages' code is delivered in the
current browser JavaScript. They remain unresolved archive/build-tool findings.
The observation does not clear generated output rights, native libraries,
source delivery, the project license, or the full license gate. It does not
attest installed package bytes against registry archives or make a legal claim
that generated outputs cannot carry upstream obligations.

## Retained Debian source files, 2026-11-03

The workspace plan now retains `runtime-debian-source-bundle-20261103`: 516
source files, 1,074,716,972 bytes, covering the exact 155 source groups for all
229 Debian packages in the same candidate image. The complete binary/source
identity inventory matches its runtime SBOM. Every file was size/SHA256 checked
against the pinned manifest, then rehashed after collection. Four retained
signature-verified source indexes independently bind all manifest source entries
to Debian metadata. The original manifest named an earlier image; compatibility
here is established by comparing every binary/source tuple to the current SBOM.

This prepares local source availability. It does not establish reproducible
binary/source correspondence, satisfy every corresponding-source requirement,
implement a recipient-facing source offer, or clear native dependency licenses.
See [source collection](dependency-security.md#retaining-debian-source-files).

Before release, repeat the observation for the exact release inputs, compare
all delivered assets to the final image, and finish the component-by-component
license and source-delivery review. Changes introducing a client dependency,
external import, SSR runtime, font or additional asset require a new review.

## Core SANE notices

The Dockerfile now includes reviewed upstream notices under
`/usr/share/scanner-kiosk/native-notices`, supplementing Debian's retained
copyright and common-license files. Only the two SANE directories are allowed
into the build context; the supplementary npm notices remain source audit
material. Each directory includes a provenance record with authenticated source
archive size/hash/URL, exact source version, binary mappings and copied file
hashes. This changes notice delivery, not the installed native packages.

| Material | Exact source evidence | Scope |
| --- | --- | --- |
| sane-airscan 0.99.35 | Debian source 0.99.35-1 upstream `LICENSE` and `COPYING` | GPL-2-or-later plus the explicit library linking exception in upstream LICENSE. Debian's copyright records GPL-2+ without reproducing that exception. Both notices are preserved; the exception is additional permission, not permission to omit the library's distribution obligations. |
| sane-backends 1.3.1 | Debian source 1.3.1-3 upstream `LICENSE`, `COPYING` and leading `backend/dll.c` comment | The root LICENSE defers to each file. The copied dll comment has the library linking exception. The marked excerpt does not replace the full corresponding source or extend the exception to scanimage or every backend. |
| SANE API header | Unmodified upstream `include/sane/sane.h` | The header declares public-domain use while requiring preservation of its copyright message and indication of changes. It is copied verbatim, without claiming project authorship. |

The workspace's `native-source-notices-20261103/debian-patch-review.json`
checks all 17 active sane-backends quilt patches: the only selected-file hunk is
in dll.c after its 41-line leading comment. sane-airscan's Debian archive has no
active quilt patches. This supports preserving these upstream notice regions;
it is not an audit of every build transformation or a reproducible binary proof.

The historical Debian checker `check-image.py` requires every reviewed notice and provenance file to enter the
actual Docker build context, compares image bytes to the source-bound copies,
checks the file hashes declared in provenance, and exercises missing-directory
rejection. SANE exception scope, native transitive dependencies, build/source
delivery, final project license choice and recipient access remain part of the
open release review. A successfully packaged notice bundle does not clear them.

## Offline API source rebuild

The workspace's `go-offline-source-bundle-20261103` retains a working-tree API
source snapshot, frontend source/embedded assets and an exact Go file proxy for
the complete 47-module graph. Original zip/mod/info files have recorded hashes.
Module acquisition uses temporary copies of go.mod/go.sum, preserving product
inputs; the build itself uses the candidate's official pinned Go 1.27.3 SDK,
network disabled, no host mounts, a read-only root and private bounded compiler
storage. The file proxy and pinned go.sum supply/check dependencies offline.

The resulting CGO-free webui API executable matched the then-current historical candidate's
SHA256 `0020b142bae8b6e4b429131cfff8afb4fd02ca44b10583fcc8bd0fb8e701aa28`.
Its version command also runs in that isolated SDK container. This is a local
API rebuild proof with the same SDK; it does not establish independent-builder,
native-helper, frontend-toolchain or complete-image reproducibility. It is not
a tagged source release, complete repository export, public source offer or
license clearance.

The earlier standard-vendor experiment is retained separately: a vendor-free
control fails dependency resolution; vendored source builds successfully but
produces different binary bytes. It is not used for the candidate reproduction
claim. Vendor retains 42 runtime Go notice files; five notices from omitted
testdata/packages are retained as supplementary evidence. The module proxy's
original archives preserve complete dependency source separately from vendor's
selected build packages. Component obligations still need review against the
actual source and binary distribution method.


## Experimental single-file Linux amd64 package

The one-file prototype now includes notices in its verified runtime cache under
`usr/share/scanner-kiosk/notices`. The notice candidate contains 64 files: 50
copied Go notice/index files, selected exact upstream SANE 1.4.0, sane-airscan
0.99.36 and Tesseract 5.5.2 notices/source excerpts, and inventory/provenance
material. The original application/helper/native/data bytes are unchanged.
The outer launcher adds standard-library Go code, covered by the included Go
standard-library license/patent notices; the project's own license remains open.

The delivered-file inventory maps 272 of the original 278 payload entries to
67 APK packages from 57 source origins. Every mapped file or symlink matches the
retained APK database checksum and each name/version appears in the immutable
comparison image SBOM. The other six entries are the custom application/helper
and four generated APK database files. The database and payload inputs have
SHA256 bindings. APK SHA1 is used solely to check consistency with that database,
not as independent signature verification. This inventory describes delivered
files; the whole comparison image inventory is a different scope.

SANE/airscan/Tesseract source archives (three files, 9,627,464 bytes) are retained
locally and verified with the SHA512 checksums from the matching Alpine aports
recipes, whose commits are named by the installed packages. SANE's image LICENSE
and COPYING match the corresponding source bytes. The airscan LICENSE preserves
its explicit linking exception. The SANE header and full dll.c source preserve
their notices without extending the exception to scanimage or other components.
Recipes are read as text and never executed by the collector.

This is **partial coverage**. The 67 package license strings include LGPL/GPL,
MIT/BSD and custom/multiple-license terms; they do not substitute for exact texts
and obligations. Transitive native notices, OCR data provenance/terms, package
patches/build scripts, complete corresponding source and replacement/relinking
conditions, project license selection and recipient source access remain open.
The three locally retained archives are not a public source offer or complete
corresponding-source delivery. Vulnerability gates remain failed. This candidate
is unreleased and does not replace the Debian image candidate.

The [native source input workflow](native-sources.md) adds bounded, non-executing collection of the exact aports recipe directories and adjacent patches. Recipe content completeness is distinct from upstream source archive completeness or a recipient-facing source offer; all existing license and publication gates remain.


The known libxml2dict.c/list.c exceptions are now preserved as three exact
comment excerpts (including dict.c's additional main-license statement). Each
has an original source member, complete source-file digest and byte/line range.
The shipped library exposes xmlDictCreate and xmlListCreate. This supports these
specific modules' relevance without proving a complete binary/source rebuild or
all other per-file obligations. Other libxml2/musl/p11-kit/libffi conditions and
the full release license/source gate remain open.


## Technical replacement path

[Native replacement and personal repackaging](native-replacement.md) documents
the existing packager/build commands for a deliberately modified native tree.
A bounded local fixture verified that the original executable rejects a tampered
cache while a newly packaged executable can install and execute a byte-distinct
GnuTLS library without a distributor key or guard bypass. The fixture added only
a non-loaded section and preserved all allocated sections; no source-code change
or ABI/source-rebuild proof is inferred. Product/runtime locks are unchanged.
Corresponding source, permission to modify/relink the relevant materials, any
required installation information, recipient access and all other component
conditions still need review before the license gate can pass.

## Current application-source rebuild (2026-11-04)

The current e5363774… onefile was rebuilt from the exact application commit
`5af8f14c53f8891473aab39986bc899004fa64e2`, a Git source archive, the retained
47-module file proxy and its 141 original zip/mod/info files. The original
`go.mod`/`go.sum` bytes match and remain unchanged. A fresh empty module cache,
read-only official Go 1.27.3 SDK, no host module cache, no external network and
bounded private scratch produced API SHA256 `9e26d8e7…`, identical to the current
inner app. Repackaging with the exact 232 native files from the current onefile
then reproduced the whole onefile SHA256 `e5363774…`. Those native bytes were
verified and reused; they were not rebuilt from source in this run. Embedded
frontend assets were likewise the previously verified build inputs.

A portable local application-source archive is retained as
`scanner-application-sources.tar` (98,703,360 bytes, SHA256
`c4cc4ffbe646819547b9041e1dce3f9f53bc46bba551096823cb0992d1abc892`).
It contains the committed source archive, complete retained Go file proxy,
embedded assets, checksum index and an API rebuild recipe. An isolated verifier
checked all 146 outer archive members and their hashes without extraction.
The archive remains local and does not supply the complete native source/SDK
closure, npm archive set, final license/relinking review or recipient source
access. The project license is still undecided. Parent-plan evidence:
`current-application-source-rebuild-20261104/current-evidence.json`.

## Fresh offline frontend dependency reconstruction (2026-11-04)

A later check closes the prebuilt-frontend limitation for the exact five asset
bytes used above. All 201 Linux x64/glibc build records in the committed lockfile
were available in the retained npm content cache and verified against SHA512
SRI. The 59 other-platform records are explicitly listed and excluded from this
target-specific input set. The check exposed no host `node_modules` or npm
configuration, created a fresh cache and dependency tree, and used Node 24.22.2
with npm 11.21.1. The npm tool tarball was reacquired as data and matched the
previously retained SHA256/SRI exactly; registry signature verification remains
unperformed. No package tarballs were downloaded by the build.

`npm ci --offline --ignore-scripts --no-audit --no-fund` followed by Astro build
produced five assets whose paths/hashes exactly match the current e5363774…
onefile inputs. Product package/lock files remained unchanged; the only source
config adaptation places Astro/Vite caches in private scratch. The initial
fixture's duplicate npm-config path and host npm 11.19.1 mismatch were retained,
then corrected without relaxing the fixed tool versions.

The local `scanner-frontend-source-inputs.tar` contains the committed source,
201 cached archives, pinned npm tool, integrity/index records and tested build
recipe: 76,195,840 bytes, SHA256
`27c537f9dbf6465752c97be8a8d384aeb3874d2278a6f10132e4bd38c34c6d28`.
A separate isolated verifier checked all 208 outer members and their hashes.
Node binaries, other-platform dependency variants and native C/APK/SDK sources
are separate inputs. These source archives remain local and unpublished; they
do not replace the final license/source-delivery or vulnerability acceptance.
Parent evidence: `current-frontend-offline-source-20261104/current-evidence.json`.
