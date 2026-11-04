# Distribution boundaries and open license work

Current navigation: [complete selected notice index](../../THIRD-PARTY-NOTICES.md)
and [source delivery method](source-distribution.md). The tables and dated
observations below preserve historical findings; they are not a current
Debian runtime inventory or a new first-party license decision.

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
| Tesseract PDF font and optional OCR font | The native payload includes Tesseract’s 572-byte `usr/share/tessdata/pdf.ttf`. Optional administrator-supplied fonts are separate. | Retain Tesseract source/license association for the bundled glyphless font. Review any separately supplied font before distributing it. |

## Image and compression component dispositions (2026-11-04)

The following decisions use the retained exact notice texts and pinned recipe
contents, not just Alpine license strings. Recipe review covered 20 files for
10 current origins plus the compressed APNG patch (SHA-512 matched to APKBUILD).
No recipe, patch, or upstream program was executed. This resolves the stated
recipe-scope questions; it does not attest every upstream source file or grant
release approval. Exact hashes and full recipe text are retained in workspace
`distribution-recipe-review-20261104`.

| Current component | Selected obligation / distribution action | Recipe and transformation finding |
| --- | --- | --- |
| Brotli 1.2.1 | MIT: preserve copyright, permission and disclaimer in delivered materials; root LICENSE is selected in native manifest. | `optimize-mips.patch` changes MIPS-only unaligned access; `fix-lp64.patch` changes platform macros. Neither adds/removes a copyright or license notice. Test-script shebang rewrite is build/test scope. |
| GIFLIB 5.2.2 | MIT: preserve COPYING with delivered library. | Two CVE patches affect the unshipped gif2rgb utility; CVE-2026-26740 changes library egif_lib.c bounds checking. Two remaining patches affect documentation builds/install. No notice removal in these diffs. |
| Leptonica 1.87.0 | Preserve exact LICENSE and its source notices; do not replace the custom text with a guessed SPDX label. | Only declared patch removes pngio_reg from test list. No library source or notice altered by that patch. |
| libjpeg-turbo 3.1.3 | Retain LICENSE.md, README.ijg, required IJG attribution and applicable BSD/zlib text. The IJG sentence now appears directly in root THIRD-PARTY-NOTICES. Source redistribution must retain original notices and identify any actual source modifications. | No local patch declared. CMake builds shared/static variants; runtime manifest selects actual libraries, not every produced variant or tool. |
| libpng 1.6.59 | PNG Reference Library License v2: preserve source copyright and mark altered source. Keep pinned APNG and ARM-NEON patches separate from pristine upstream source with modification records. | APNG 1.6.58 patch adds read/write APIs and changes a 2025 copyright year to 2026, already represented in retained LICENSE. No added licensing text. ARM-NEON patch changes configure/header guards; autoreconf regenerates build controls. Generated/build-only source remains a separate source-archive scope. |
| WebP 1.6.0 | BSD-3-Clause: retain COPYING and patent grant, preserve disclaimer and avoid endorsement. PATENTS termination conditions are retained without rewriting. | No local patch; recipe enables mux/demux/decoder and disables TIFF. CLI tools are not implied by a library origin mapping. |
| LibTIFF 4.7.2 | Preserve LICENSE.md and Berkeley attribution for the delivered library; do not infer rights from package name alone. | No local patch. Shared-library source selection differs from separately packaged tools/C++ library. CMake generates build controls. |
| XZ/liblzma 5.8.4 | Select 0BSD for liblzma as explicitly stated by upstream COPYING; do not assign GPL terms from xz scripts to the delivered library. Retain bundled notices. If shipping the full source archive, retain its distinct Autotools/script terms too. | No local patch. autoreconf and libtool rpath edits affect build controls, not a GPL-only runtime script. |
| zlib 1.3.2 | Preserve original notice in source; identify altered source and do not misrepresent origin. Include pinned security patch with corresponding source. | CVE-2026-85091 changes gzwrite.c state reset after failure. It neither deletes notice text nor creates a separately licensed file. Source bundle must identify this patched distribution. |
| Zstandard 1.5.7 | Choose BSD-3-Clause route for the library under the declared BSD/GPL alternative; preserve copyright, disclaimer and non-endorsement condition. Keep COPYING as upstream context instead of deleting it. | No local patch. Meson PGO/LTO/tuning changes build output, not the declared license choice. Library scope excludes CLI/contrib tools unless actually selected. |

The above actions use existing selected notice files; no additional native
payload is created for this review. Final component/source archive review still
must account for relevant upstream exceptions and actual recipient access.
The remaining special-source reviews are the already named libxml2 dict/list,
musl third-party sections, libffi templates, p11-kit conditional/generated files,
SANE/fork/library combinations, Go subcomponents, assets and container base.
Do not turn generic historical “all per-file coverage open” language into a new
requirement to compile every APK or copy every source comment into runtime.

## Current Go API dependency dispositions (2026-11-04)

Read-only `go version -m` on API SHA-256
`f3a6c12580f1d88fd476843ba099f9e7a1e58a277a3326aa24c893f47ca6569a`
reports producer Go 1.27.3, CGO disabled, and the following 21 modules. Every
name/version/module sum matches the retained Go notice index; all 49 indexed
notice files (including standard-library notices) match their SHA-256 values.
The metadata reader was official installed Go 1.25.7 in a network-none,
read-only container; this was not a build with that older compiler.

| Module | Version | Root terms and required retained materials |
| --- | --- | --- |
| `github.com/alexedwards/scs/v2` | `v2.9.0` | MIT; copyright, permission, disclaimer |
| `github.com/clipperhouse/uax29/v2` | `v2.7.0` | MIT; copyright, permission, disclaimer |
| `github.com/coreos/go-oidc/v3` | `v3.21.0` | Apache-2.0; license, attribution/NOTICE where supplied, source modification notices |
| `github.com/dustin/go-humanize` | `v1.1.0` | MIT; copyright, permission, disclaimer |
| `github.com/go-jose/go-jose/v4` | `v4.1.7` | Apache-2.0; license, attribution/NOTICE where supplied, source modification notices |
| `github.com/google/uuid` | `v1.6.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `github.com/hhrutter/tiff` | `v1.0.7` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `github.com/mattn/go-runewidth` | `v0.0.31` | MIT; copyright, permission, disclaimer |
| `github.com/pdfcpu/pdfcpu` | `v0.16.4` | Apache-2.0; license, attribution/NOTICE where supplied, source modification notices |
| `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `github.com/xdg-go/stringprep` | `v1.0.4` | Apache-2.0; license, attribution/NOTICE where supplied, source modification notices |
| `go.yaml.in/yaml/v3` | `v3.0.5` | MIT for libyaml-derived files plus Apache-2.0; retain full LICENSE and NOTICE |
| `golang.org/x/crypto` | `v0.58.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `golang.org/x/image` | `v0.47.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `golang.org/x/oauth2` | `v0.37.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `golang.org/x/sys` | `v0.49.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `golang.org/x/text` | `v0.43.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `modernc.org/libc` | `v1.79.0` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `modernc.org/mathutil` | `v1.7.1` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `modernc.org/memory` | `v1.12.1` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |
| `modernc.org/sqlite` | `v1.61.1` | BSD-style; copyright, conditions, disclaimer and applicable non-endorsement |

These root terms do not override copied subcomponents: retain go-jose JSON,
TIFF/pdfcpu LZW, pdfcpu PKCS7, modernc third-party appendices and their exact
texts. Go standard library and golang.org/x packages retain BSD-style notices
and the supplied patent grants. Apache-2.0 §4 requires retaining provided NOTICE
attributions; the selected go-oidc and YAML NOTICE files are already indexed.
No local modification to these module source archives is introduced here.

The actual API metadata has neither go-isatty nor go-strftime as a dependency.
Their absence as standalone notice-index entries is therefore not a missing
runtime-module notice for this API candidate. The sqlite third-party appendix
also preserves their MIT texts for source distribution. Its graph-only MPL
entries, repository sponsor marks and memory logo reference must not be treated
as newly embedded browser assets or relicensed as this project's logo. If
shipping full module source archives, retain those original source notices and
trademark qualifications; do not infer source-archive rights from API linkage.

This confirms the current API's module-to-notice association. The launcher/helper
and final changed binaries still require their own identity/scope binding; this
is not a blanket native/container/source distribution approval.

The current outer launcher and sane-parameters helper were also read with the
same isolated metadata reader. Both report Go1.27.3 and no external Go modules;
CGO is disabled for the launcher and enabled for the helper. The helper bytes
match current native manifest SHA `0266e0812b622d0569d5dd6e0ba26a09a3464d9fc0fa985793c6db4ebe73f0d0`.
Their Go dependency notice scope is therefore the standard library; native
SANE/header obligations remain separate for the helper. No target executable
was run by this metadata check.

## Current library choices and combination requirements (2026-11-04)

These decisions concern the identified library/code portions, not every tool in
their upstream archives. Full license texts remain authoritative. Existing
source/replacement proofs are reused; no new binary is produced for this review.

| Component / delivered boundary | Selected route and concrete required materials | Evidence / remaining acceptance |
| --- | --- | --- |
| Avahi client/common 0.9-rc4 | LGPL-2.1-or-later library route. Retain LICENSE, source/build controls and library modification/replacement rights. | Exact client.c/malloc.c preambles read from pinned `v0.9-rc4.tar.gz`; filename-selection gap from earlier review is resolved. Recipe patch only alters a daemon service dependency, not these libraries. Final source delivery remains gated. |
| GnuTLS 3.8.13 | LGPL-2.1-or-later for library; preserve library source and LGPL text. Root GPL COPYING concerns other portions too. | lib/handshake.c and lib/gnutls_int.h identify terms. Four recipe patches affect tests, not library licensing. Existing real ELF replacement/repack proof establishes the technical path; final matching source/control delivery remains gated. |
| gettext libintl 1.0 | LGPL-2.1-or-later for intl library, not blanket GPL for gettext tools. | Exact intl/dcigettext.c notice retained. Recipe patches affect gettext-tools tests. Full source archive retains its own tools/build-file terms. |
| libtasn1 4.21.0 | LGPL-2.1-or-later for lib/. Preserve GNU Bison’s additional permission for generated ASN1.c; do not relabel that skeleton GPL-only. | README explicitly separates library, GPL tools and FDL manual. ASN1.c preamble has Bison exception. No local recipe patch. Source distribution must keep those distinct texts. |
| GMP 6.3.0 | Choose LGPL-3.0-or-later option for library. Preserve LGPL3/GPL3 materials, library source, application/relink materials as applicable. | README offers LGPL3+ or GPL2+. gcc15.patch changes acinclude.m4 build detection. GPL2-only must not be inferred from an alternative. |
| Nettle/hogweed 3.10.2 | Choose LGPL-3.0-or-later option for library. Preserve library source, notices and applicable combined-work materials. | aes-encrypt.c/rsa-sign.c permit LGPL3+ or GPL2+. No local recipe patch; pkg-config comment stripping is build metadata, not a license change. |
| libidn2 2.3.8 | Choose LGPL-3.0-or-later option for library, retaining included Unicode terms separately. | lib/idn2.h.in grants LGPL3+ or GPL2+. No local recipe patch. Unicode notice must not be replaced by LGPL text alone. |
| libunistring 1.4.2 | Use LGPL3+ library distribution route where granted; keep individual file notices permitting LGPL2.1+ and the applicable source/build terms. | u8-check.c has LGPL2.1+; root COPYING.LIB contains LGPL3. No local recipe patch. Do not describe every file as LGPL3-only. |
| libusb 1.0.30 | LGPL-2.1-or-later library route; notices/source/modification and replacement materials retained. | libusb/core.c identifies terms. No local patch; configure disables udev. Runtime shared-library identity remains distinct from SDK static output. |
| D-Bus 1.16.2 library | Choose GPL-2.0-or-later alternative, allowing GPL3-compatible combinations when required by linked LGPL3 components. Retain exact source/notice and combination terms. | COPYING and dbus-connection.c offer AFL2.1 or GPL2+. No local patch. Do not infer that this choice relicenses the separate CGO-free API, or that a library exception removes another dependency’s GPL. Actual native combination/source obligations must be accounted for in final delivery. |
| GCC libgcc/libgomp/libstdc++ 15.2.0 | GPL3 with GCC Runtime Library Exception 3.1 for files granting it. Preserve both texts. Independent modules may use the exception only for an Eligible Compilation Process. The runtime library itself retains its terms. | Current source-managed preambles explicitly grant the exception. Final build controls must identify the actual compiler/transforms; merely seeing a GCC package name does not establish eligibility. |
| SANE 1.4.0 and sane-parameters | The header’s retained permission/modification record and dll.c’s SANE linking exception support the independent Apache helper; scanimage remains GPL-2.0-or-later. | Reuse exact source/recipe review and helper rebuild. Preserve sane.h’s Alpine include.patch modification. Exception is not extended to scanimage or unrelated backends. |
| Modified airscan 0.99.38 fork | GPL-2.0-or-later plus the retained library-linking exception. Provide exact fork source, patch and build controls, retaining exception for the modifications as recorded by the fork. | Existing pinned fork/source rebuild and provenance bind its bytes. No hidden application-Dockerfile patch or unmodified upstream substitute. |

For LGPL combined works, distribution must permit modification for the
recipient’s own use and reverse engineering to debug those modifications. No
EULA or other added restriction may contradict those rights. Supply the actual
library source and, where the chosen clause requires it, corresponding
application/relink materials and installation information. The existing personal
repackaging mechanism needs no distributor key. Its success is a technical
replacement proof, not automatic satisfaction of every library’s source and
combination conditions. The independent API invokes native programs as separate
processes; CGO linkage belongs to sane-parameters and must be evaluated there.

### Bundled PDF font identity

The current native manifest’s `usr/share/tessdata/pdf.ttf` is 572 bytes, SHA-256
`c7845420925a23d88ed830a63957b8af85a66a8daf8d9fc90e843673b2ef1a59`.
It matches `tesseract-5.5.2/tessdata/pdf.ttf` in the pinned source archive.
That archive’s root LICENSE is Apache-2.0; the associated pdfrenderer.cpp also
identifies Apache-2.0. The fixed Alpine recipe installs the Tesseract tree and
separately installs eng/jpn/osd models from tessdata 4.1.0. Therefore the bundled
font/configs are not reattributed to the rebuilt Tesseract 5.5.4 executable or to
the optional administrator font. Preserve the exact source association and
original notices; no first-party ownership is claimed for this font. This
identity check does not prove all font-specific rights or cover arbitrary fonts
later selected by an operator.

## Current distroless-only delivery scope (2026-11-04)

The candidate image `sha256:024750f34391f3402a6369d0615949c2dc885b69544b2cb8d29f7a443f223c59`
retains the pinned static-debian13 base layers unchanged. Reading its dpkg
status files identifies six installed-package records; these are container
scope, not extra modules packed into the standalone executable. Package records
can describe more files than distroless retains, so they do not prove an entire
Debian package or its executable tools are delivered.

| Package record | Version | Observed terms and selected material |
| --- | --- | --- |
| base-files | 13.8+deb13u7 | GPL-2+ for package files, separate verbatim permissions for license documents. Copyright/GPL2 present; exact matching source .dsc/tar.xz retained and freshly size/SHA256 checked. |
| ca-certificates | 20250419+deb13u1 | Mozilla certdata is MPL-2.0; Debian tools/build files have GPL-2+. Copyright/MPL2/GPL2 present. Exact matching source .dsc/tar.xz retained and freshly checked; include covered source and source-access notice when conveying generated certificates under MPL §3.2. Do not infer that update-ca-certificates runs in distroless. |
| media-types | 13.0.0 | Copyright file identifies compiled public information as public-domain/ad-hoc. Preserve exact attribution; do not assign GPL from another base package. |
| netbase | 6.5 | GPL-2-only, distinct from base-files’ or-later. Preserve copyright/GPL2 with actually retained data. Distribute preferred-form text files as source where applicable; do not substitute a different package version or infer an LGPL exception. |
| tzdata | 2026f-0+deb13u1 | Copyright identifies IANA database as public domain. Preserve that attribution. |
| tzdata-legacy | 2026f-0+deb13u1 | Same source package and public-domain database terms; distinct package record. |

All six copyright files plus GPL2 and MPL2 were copied individually from the
actual stopped image filesystem and SHA-256 recorded. Containers were never
started and were removed. An initial whole-document-directory copy failed at
the read-only application directory; only successful individual copies count.

[Exact source input subset](distroless-source-inputs.json) selects the matching
base-files and CA sources from earlier authenticated Debian source metadata.
The prior 229-package image inventory is not the current six-package base
inventory. In particular, its retained tzdata 2026e is **not** source identity
for this base’s 2026f; netbase/media-types records are also not covered by that
old inventory. Public-domain data does not acquire a new copyleft source-offer
condition merely because its package differs. Actual retained GPL-covered data
and covered-source/release binding still must be included by the release job.

These findings close the unknown base-package/notice identity scope. They do
not constitute an OS vulnerability scan, prove every selected data file’s
preferred source form, or satisfy actual recipient access. Do not turn that
limited result into complete image approval.

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

## Current SANE linking boundary, 2026-11-04

The retained SANE 1.4.0 archive (`f99205c903dfe2fb8990f0c531232c9a00ec9c2c66ac7cb0ce50b4af9f407a72`)
was read without extracting or executing its files. Its root LICENSE defers to
individual file notices. `backend/dll.c` retains GPL-2.0-or-later with the SANE
library linking exception; `frontend/scanimage.c` declares GPL-2.0-or-later.
The exception does not relicense scanimage or remove source/notice obligations
for the delivered libraries. It must not be extended to every SANE backend.

`cmd/sane-parameters/main_linux.go` explicitly links `-lsane` and includes
`sane/sane.h`; the CGO-free API/worker invokes separate native tools. The header
permits public-domain use subject to retaining its message and indicating
modifications. The exact Alpine 1.4.0-r4 recipe at commit
`6bbd45b4dd10f4ec6cd6d99265aba2de11569522` carries `include.patch`, adding
`#include <sys/types.h>` after the header guard without changing its notice.
Its other two declared patches affect saned's pid path and test selection;
none changes dll.c or scanimage.c. Preserve this header modification record
alongside the retained upstream notice and recipe.

The separate sane-airscan fork retains its explicit library linking exception.
These permissions support keeping the project-owned helper under Apache-2.0;
they do not change the fork's license or approve the complete native closure.
Per-file coverage of SANE support code, transitive libraries, complete notices,
and recipient-facing source/replacement delivery remain open. The workspace's
`sane-linking-review-20261104/result.json` retains the selected full-file hashes,
leading notices, root license texts and all seven exact recipe files.

## p11-kit library notice supplement, 2026-11-04

The source-managed native notices now include the complete leading comments
from 35 C sources named by p11-kit 0.26.2’s shared-library, internal-library and
common-library Meson targets. The original root COPYING is retained. The
supplement preserves actual copyright holders and the separate ISC terms in
common/base64.c and common/hash.c; it does not assign the root BSD-style terms
to those files. Exact source and comment hashes accompany the collection.
Header/generated-template coverage and conditional build transformations remain
open. This updates the native input manifest to 233 entries; the retained
232-entry onefile/image candidates have not been rebuilt with this supplement.

## musl third-party source notice supplement, 2026-11-04

The source-managed musl notices now supplement its root COPYRIGHT with 118
verbatim comments from 116 selected TRE, generic/x86_64 math/complex,
DES/blowfish crypt and qsort source files. Each excerpt retains its original
text, full-file SHA-256 and exact byte range. This includes the TRE BSD terms,
DES attribution to David Burren and Solar Designer, Sun notice-preservation
terms, qsort attribution and the blowfish public-domain fallback permission.
The existing comment inventory was reused; every selected file and excerpt
was independently checked against the fixed upstream archive before inclusion.
This closes the known missing notices in those selected source comments;
compiled-file/assembly/line-comment and recipe-transform coverage remain open.
The native input now has 234 entries; the existing 233-entry onefile remains a
prior candidate until these added notices are packaged.

## libffi Linux x86_64 source notice supplement, 2026-11-04

The source-managed libffi notices now supplement root LICENSE with 15 complete
leading notices from generic sources, the non-x32 X86_64 backend and associated
header templates, plus dlmalloc’s opening public-domain attribution excerpt.
configure.host selects ffi64.c/unix64.S and ffiw64.c/win64.S for that backend;
the Win64 calling-convention source is relevant even on Linux. Each retained
notice has its original source-file hash and byte range. Conditional debug and
allocator paths are included conservatively. Exact compiled-object/configuration
and recipe transformation coverage remain open. The native input now has 235
entries; the retained 233-entry onefile has not been rebuilt with the later
musl/libffi supplements.

## Major native notice coverage, 2026-11-04

The source-managed set additionally retains exact major notices for Avahi,
D-Bus, GMP, GnuTLS, GCC runtime libraries, gettext/libintl, libidn2, libtasn1,
libunistring, libusb and Nettle. Each component record maps its pinned upstream
archive, recipe files and actual retained runtime paths. GCC Runtime Library
Exception 3.1 is accompanied by selected libgcc/libgomp/libstdc++ source
preambles, including their attribution and additional permissions. libidn2’s
Unicode terms and D-Bus’s referenced LICENSES directory are preserved.

Root license files can cover upstream tools or build inputs absent from the
runtime. In particular, D-Bus COPYING offers AFL-2.1 or GPL-2.0-or-later and
identifies GPL-only standalone tools; it must not be read as assigning one
license indiscriminately to all files. A full license text or a mapped root
notice is not evidence of complete per-file/compiled-source coverage. All 28
current APK source origins now have major-notice mappings; exact patch/build
transformations and recipient/distribution obligations remain under review.
The retained 235-entry onefile predates these notice-input additions.

## Named permissive-source findings closed for selected inputs (2026-11-04)

- **p11-kit:** root COPYING requires binary notices in accompanying materials.
  The35 selected library C preambles were already retained. A bounded textual
  include closure now covers their local headers, all conditional branches and
  version.h.in generation;20 additional unique preambles are retained verbatim.
  This includes common/pkcs11.h’s unlimited-copy permission with notice
  preservation, ISC and BSD holder/year notices. Test/mock headers not reached
  by that closure were excluded. No project-local include remains unresolved.
- **musl:** root COPYRIGHT explicitly places the x86_64 port under standard MIT,
  identifies the third-party TRE/math/crypt/qsort regions and permits public
  headers/crt to omit attribution. The existing118-comment supplement preserves
  identified generic/x86_64 third-party texts. Do not require attribution for
  the exempt header/crt material, or ARM code absent from this target. Source
  redistribution keeps the complete upstream copyright text and source notices.
- **libffi:** root LICENSE requires copyright/permission preservation. Existing
  Linux non-x32 X86_64 selection retains generic/ffi64/ffiw64/assembly/template
  notices and allocator attribution; Win64 ABI support on Linux is included.
  Preserve these with root LICENSE and unchanged source/recipe controls.
  A byte-identical rebuild of every APK is not an extra license condition.
- **libxml2:** root Copyright’s specifically named dict.c/list.c exceptions and
  dict.c’s additional common-license statement are already retained as exact
  byte-range excerpts. They are not relicensed solely under the root holder.

These are dispositions of the named notice-preservation findings, not claims
about every source file in unrelated architectures/tools. Required source and
final artifact/recipient access remain subject to their separate gates. The
header supplement changes selected notice inputs only; the old candidate is
not represented as containing it, and no per-notice repack is performed.

## Preparation disposition (2026-11-04)

The named data/source-form findings now have concrete dispositions:

- The tessdata4.1.0 LICENSE member is Apache-2.0 and matches the retained license
  SHA-256. Its complete archive identity reuses the earlier source-integrity
  proof; this was a bounded fresh member read, not a new668MB full rehash.
  eng/jpn/osd source/recipe associations remain distinct from rebuilt executable
  and the glyphless font/configs.
- The container actually retains `/etc/protocols`, `/etc/rpc` and `/etc/services`
  as plain UTF-8 data. Preserve these preferred-form texts with netbase's GPL2
  copyright/license; don't invent an object-code source gap from absent Debian
  utilities. Matching base-files/CA sources are already retained separately.
- The frontend’s SVG path drawing, static theme/CSS, template and JA/EN catalogs
  are maintained as project assets under ADR0005. The existing output graph
  records only project-rendered JS modules. Current entry/public source has no
  external font/stylesheet/code import. This records the actual project source
  origin; it invents neither a copyright holder nor rights in upstream npm or
  source-module logos.
- The recorded GNU15.2.0 compiler/cache and ordinary CMake/CGO build controls
  provide the selected GCC exception build path; known whole-archive sources,
  fork patches and helper materials are selected for distribution. All LGPL
  application/relink materials are supplied in source as described by the
  adopted source set; the design does not rely solely on a shared-library
  label to avoid those materials.

Preparation has no remaining unspecified component-condition finding for this
selected input set. **This is a preparation disposition, not the final license
gate.**43-02 must generate the refreshed exact source/notice material set, verify
build/control completeness and bind it to the candidate; a missing required
material fails that plan.43-06 must bind actual tag/image/served assets and
recipient access after G5. No existing source bundle or old executable is
retroactively relabeled to include the new header supplement.
