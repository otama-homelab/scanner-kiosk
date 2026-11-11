# Native build inputs

## Current decision — 2026-11-04: official airscan, no maintained fork

The selected runtime uses unmodified Alpine `sane-airscan 0.99.36-r0`.
No fork source, fork build, or separate airscan service is required. The native
manifest selects 295 entries: 111 code/data and 184 notices, with 37 official APK
components supplying 106 entries. Only Tesseract (four entries) and the
first-party parameter helper (one entry) require source-build exports.

Registered SANE scanner endpoints accept HTTP only: upstream does not provide
the verified TLS policy required for HTTPS scanner endpoints. Browser/OIDC HTTPS
is unaffected. The Canon TS8130 uses HTTP eSCL. Keep scanner traffic on a trusted
LAN. Prior fork/TLS build results below are historical evidence, not evidence
for this newly selected official backend. Candidate assembly and device
acceptance must use the new manifest before release.


The distributed container remains distroless. This directory owns build-only
native input locks; it contains no native binaries, private documents or SDK
package cache.

`tesseract-sdk.lock.json` freezes all 174 APK inputs of the observed Tesseract
5.5.4 SDK. It records whole-archive SHA-256, package control identity, version,
archive architecture, origin commit, repository snapshot hashes and the trusted
public key hashes. The installed database reports x86_64 for all entries; the
original package archives preserve `noarch` for 24 architecture-independent
packages. Their control identities, versions and sizes match the observed SDK.

`tesseract-sdk.SHA256SUMS` and `alpine-sdk-keys.SHA256SUMS` are the checked
installer inputs derived from that lock. Keep them consistent with the JSON.
The build-only libarchive entry was refreshed to signed 3.8.11-r0 after the
3.8.10-r0 archive disappeared upstream. Its per-package `index_sha256` records
the replacement snapshot; the original snapshot hashes for unchanged SDK inputs
remain intact. This package is not part of the product native runtime.
To prepare a separate build context, copy `Dockerfile.tesseract-sdk` as
`Dockerfile`, the two checksum lists, and exactly the 174 pinned APK files under
`apks/`. Fetch only the recorded versions; verify publisher signatures and
whole-archive hashes before using or retaining those inputs. Do not copy the
application repository or credentials into this context.

The SDK Dockerfile checks trusted keys, archive count/type and all archive
hashes before installing with networking disabled. Package signatures remain
required; there is no untrusted-package override. BuildKit bind mounts keep the
APK cache out of the resulting SDK filesystem. An offline reconstruction installed all 174 pinned packages and produced a
package listing byte-identical to the observed SDK. This recipe supplies build
tooling; it does not itself compile Tesseract. A separate offline reconstruction
using the retained unmodified 5.5.4 source and original CMake options passed two
selected upstream tests. Its executable and shared library were byte-identical
to the current native payload, and both library symlink targets matched. This
proves that Tesseract subset, not reproducibility of the complete native payload.
SDK archives are retained build inputs, not preview/release uploads. Their own
redistribution obligations need review before any SDK artifact publication.

## Tesseract source build

`tesseract-source.lock.json` pins the unmodified upstream 5.5.4 commit/archive
and the SDK lock it requires. `tesseract-source.SHA256SUMS` is the corresponding
installer input. `build-tesseract.sh` now owns the offline configure/build/test
recipe; `Dockerfile.tesseract-build` supplies its read-only input image.

Reconstruct and verify the SDK first, under the local tag
`scanner-kiosk:tesseract-sdk-locked-20261104`. Prepare a separate directory with
`Dockerfile.tesseract-build` renamed `Dockerfile`, `build-tesseract.sh`,
`tesseract-source.SHA256SUMS` and the locked source archive renamed
`source.tar.gz`. That input-image build only copies inputs; it is not a successful
Tesseract compilation. It does not fetch source or install additional packages.

Execute the image with no network, read-only root/inputs, UID/GID10001, bounded
CPU/memory/process/file resources and fresh private writable `/work`. Keep this
working path: compiler paths and CMake options match the existing byte-reproduced
build. The local build uses 2 CPUs, 3 GiB memory, 128 tasks and 1 GiB scratch.
The one-shot script returns nonzero if the source checksum, configure, compile,
either selected upstream test, version command or artifact export fails. It
cannot print `RESULT_READY` after a failed test. The four CI fixture tests use
stubbed external build tools to exercise these failure/output contracts; actual
compiler/upstream-test evidence is a separate offline SDK run.

Collect `/work/native.tar` from the private scratch before it is discarded. A
collector using tmpfs must supervise collection before container teardown; the
build script itself does not keep a completed container alive. The archive's
`native/` subtree contains the four runtime file/link paths expected by the
assembler's built root. Logs, CMake cache and SDK inventory are separate members,
not runtime files. Validate the four identities against `runtime-manifest.json`
before merging with the helper/fork artifacts. The archive is not a complete
corresponding-source offer, security/license approval or a preview/release.

## SANE helper SDK

`sane-sdk.lock.json` and `sane-sdk.SHA256SUMS` pin all 46 APK inputs of the
SANE helper SDK, alongside the official Go 1.27.4 base digest. Seventeen package
identities come from that base; the original SDK build log records the exact
29 additions. All match the retained official repository indexes. The
`Dockerfile.sane-sdk` context uses the same layout as above, with exactly its
46 APKs and its checksum list. It verifies publisher signatures and hashes and
installs offline. This lock does not assert archive architecture from repository
architecture; whole-archive identity is pinned directly.

The reconstructed SDK originally reproduced the prior 4,924,560-byte SANE
helper (`90b4829bc502a37908762ea3e3cee0e4b26167bb52a616da8b85b6555dba3e0d`).
After adding the explicit CA setting to its imported SANE package, the same
locked SDK rebuilt the helper as 4,924,568 bytes, SHA-256
`0266e0812b622d0569d5dd6e0ba26a09a3464d9fc0fa985793c6db4ebe73f0d0`.
The checked-in native input now pins that rebuilt helper and matching source/SDK
hashes. That intermediate update kept the other 181 native entries unchanged. No external Go modules were
downloaded and no scanner was accessed. The earlier 6eab onefile retained the older helper. The new experimental dbef72cf
onefile included the rebuilt helper, TLS-verifying backend and CA data; the newer
b85a7dae candidate also includes the HTTPS downgrade guard.

`helper-source.SHA256SUMS` now mirrors that first-party source inventory.
The 2026-11-05 PDF worker change added file-backed stdin to the shared process
supervisor, which is also a selected helper source. The same locked SDK rebuilt
the helper as 4,924,568 bytes, SHA-256
`c4a357f263145565791690c243d7515c6d0a67e52c40a5543081a64cb87a8b8d`.
All 46 SDK package identities matched the lock. Only that helper entry changed;
the other 294 current native entries and external libraries stayed identical.
The updated manifest SHA-256 is
`21bb815521d41b3ced50405e34605b54aa4720c9b45a20832fdc833115bf599a`.
Source/SDK/manifest contract tests passed; this is not new real-device acceptance.

The Go 1.27.4 security update was measured in CI run `43059656151` from
source `537255dc21d4e2c0ea196b2cb3da125984414332`. The offline helper build
reported Go 1.27.4, CGO enabled, Linux/amd64 and the `saneparams` tag. All 46
SDK package identities matched the existing lock. The helper is 4,924,720 bytes,
SHA-256 `0373fe4ea283a599c8a59569753ee3b3d37a640441e9f9715a837788419ff511`.
The refreshed Go notice index changes only its toolchain version; all license
texts are unchanged. The resulting runtime manifest SHA-256 is
`467cea6c409a0704cf56db9404a796ee3d45f191bd87f2aa6ec0c45d4fa63e1c`.
The measurement run failed during assembly because its intermediate APK lock
still referenced the prior provenance hash. The final lock binds the refreshed
provenance; the full candidate build must verify assembly before publication.

`build-sane-parameters.sh SOURCE_DIRECTORY CHECKSUM_FILE` copies only the listed
Go source/module files into fresh private scratch, rejects unsafe selection paths,
then checks the copied hashes before compilation. Extra source files cannot enter
the Go package. It fixes the CGO/tag/trimpath settings, disables Go workspace/env
overrides and module/toolchain downloads, and uses scratch-local home/cache/module
directories. A failed checksum, compiler or artifact step stops the one-shot
script before `RESULT_READY`.

Prepare `Dockerfile.sane-build` as `Dockerfile`, the build script, checksum list
and a `source/` directory containing exactly the recorded source inputs. Do not
copy the whole workspace or credentials into this build-only context. Reconstruct
and verify the SDK under `scanner-kiosk:sane-sdk-locked-20261104` first; building
the input image alone does not compile the helper. Execute with no network,
read-only inputs/root, UID/GID10001 and fresh private `/work`; local verification
uses 2 CPUs, 1 GiB memory, 128 tasks and 512 MiB scratch.

`/work/native.tar` contains an assembler-compatible `native/` subtree plus build
log, Go build info and SDK inventory. Collect and validate it before discarding
scratch, using the same lifecycle rules as the Tesseract build. The actual SDK
run included an invalid unlisted `.go` file in the helper package as a control;
the selected build still reproduced the current helper hash exactly, without
external modules or scanner access. Fixture tests cover source selection/hash,
compiler failure, path escape, usage and inventory binding; they stub the compiler,
while the real offline build supplies the binary identity evidence.

Stable native provenance/closure packaging, complete corresponding-source and
notice delivery, exact-candidate security/license acceptance and publication
remain separate work.
See [native source workflow](../docs/design/native-sources.md).

## Joining source-build outputs

Use the official APK backend through `--apk-root`. Combine the four Tesseract
entries and one rebuilt first-party helper through `--built-archive`; the
assembler checks their exact file/link identities. No fork export is selected.

The actual canonical build outputs from all three repositories/recipes were
combined with the verified 105-entry APK root and the preserved 80-entry notice
input. Assembly reproduced all 191 native entries, and the existing packager
produced the current 192-entry app/native manifest and payload hash unchanged.
This verifies the component handoff; the controller still used explicit local
artifacts. A single acquisition/SDK/source-build CI orchestration, stable complete
notices/source delivery and release gates remain unfinished. Historical notices
are deliberately identified as preserved inputs, not regenerated current proofs.

## Source-managed notices

The current native manifest has **298 entries**:111 code/data entries and187
notice/source/observation files. All 187 notices are source-managed in `notice-files/`.
The 80 retained historical inputs preserve their original runtime bytes. The first six source-managed files
cover Leptonica1.87.0/libjpeg-turbo3.1.3.32 additional files preserve19 upstream
texts for12 transitive packages,12 source-association records and LibTIFF's
Berkeley attribution. Original native code/data and prior notices are unchanged.

The source records bind exact archives, selected member hashes and runtime files.
libffi, libxml2, musl and p11-kit explicitly retain per-file review follow-ups.
XZ's root summary identifies the delivered liblzma as0BSD; the extra GNU texts
are upstream package context, not an assignment of GPL terms to every binary.
Three exact libxml2dict/list comment excerpts additionally preserve known exceptions
to its root Copyright. Their source ranges/full-file hashes are recorded; the
actual library exports the corresponding dictionary/list APIs. Other per-file and
build-transform conditions remain open. These selected notice deliveries do not
establish complete license/source approval.

`runtime-provenance.json` maps these destinations through `notice_sources`.
The assembler reads those sources only from `notice-files/` beside the supplied
manifest, instead of the historical `--notices-root`. Both source-managed and legacy caller-supplied paths pass the same
manifest size/hash checks and descriptor-relative no-follow traversal. Missing,
changed or symlinked source-managed notices fail without falling back to historical
input. Keep this directory beside the manifest in a source export or build input.
The current manifest needs no historical notice root: all 187 inputs are now
in the checkout. For the assembler’s required `--notices-root` argument, supply
a new empty directory; it is not read for this manifest. Generic older input
manifests retain the existing explicit historical-root path.

The p11-kit notice set additionally retains verbatim leading notices from the35
C sources explicitly listed for its shared, internal and common libraries.
This includes ISC terms in base64.c/hash.c alongside the BSD-style notices.
Exact source-file and excerpt hashes are indexed in source.json. Headers,
generated templates and conditional build transformations remain under review.

The musl supplement preserves118 exact third-party comment notices from116
selected TRE/math/complex/crypt/qsort sources, including BSD and Sun terms and
the blowfish fallback permission. Source hashes and exact byte ranges accompany
the notices. Other source/build coverage remains under review.

The libffi supplement retains15 full leading source/header-template notices
and the dlmalloc opening public-domain attribution. configure.host selects both
the Unix64 and Win64 calling-convention support on non-x32 Linux x86_64;
ffiw64.c is therefore included. Exact source/range hashes accompany the text.

Major notices for11 further origins (Avahi, D-Bus, GMP, GnuTLS, GCC runtimes,
gettext/libintl, libidn2, libtasn1, libunistring, libusb and Nettle) now retain
upstream GNU license texts, authors and available exceptions. GCC Runtime Library
Exception3.1 and selected runtime source preambles are preserved. D-Bus LICENSES
files and libidn2 Unicode terms retain upstream package context; their presence
does not assign every included license to each runtime library. All28 current
APK source origins now have major-notice mappings; per-file and distribution
approval remain separate.

Per-component notice filenames are flat; source_member retains the original
upstream path. Each source record includes the pinned archive SHA-512 required
by the native runtime contract tests.

## Fixed runtime input

`runtime-manifest.json` pins the current 298 native file/link entries, without
the app executable. Pass it explicitly to `scripts/build-standalone.py` using
`--native-manifest native/runtime-manifest.json`. Any native input change needs
an intentional lock update and validation; the packager compares all entries.

`runtime-provenance.json` maps every entry to exactly one APK source component,
upstream source build, first-party helper or historical notice role. It includes
36 APK source components (105 retained entries), Tesseract's 4 entries, the
modified sane-airscan fork entry, the first-party helper and 80
notice/source/observation files. The first-party source
hashes bind its native build to the relevant Go source and module inputs; CI
requires a native rebuild/review when those change. These records deliberately
exclude a whole-app version or payload hash, so an API/frontend-only change does
not imply a native component change.

The fork patch/build metadata tracks the selected current backend. The other
fixed inputs preserve historical notice/receipt bytes. Some describe earlier
images and broader inventories; they are not current installed-package claims
or instructions to regenerate run-dependent fields. This input identity is not
a complete reproducible closure assembly recipe, downloadable native archive, SBOM,
license acceptance or publication approval. Do not replace the current notice
set with this index or remove upstream license texts.

## Pinned runtime APK inputs

`runtime-apks.lock.json` and `runtime-apks.SHA256SUMS` pin the 36 official APK
archives that supply the runtime's 105 APK-origin entries. The lock records each
whole-archive SHA-256/size, package/control identity, source origin/commit,
repository and archive architecture, plus the trusted public-key hashes and
runtime provenance binding. Construct a download URL from a recorded repository,
`repository_architecture` and `filename`; never resolve an unversioned latest
package. Retain the verified archive cache for offline builds.

Acquire the recorded package versions in a bounded, credential-free build
sandbox containing Python 3 and the trusted Alpine `apk` tool:

```sh
python3 scripts/fetch-native-apks.py \
  --lock native/runtime-apks.lock.json \
  --keys /private/pinned-public-keys --apk /sbin/apk \
  --output /private/new-locked-apk-cache
```

The command supports the existing runtime, SANE SDK and Tesseract SDK lock
formats for Linux amd64/Alpine 3.24. It validates fixed repository URLs, package
identities, sizes, hashes and the exact trusted-key set before fetching each
exact filename URL with Python’s standard HTTP client.
Proxies and redirects are disabled. It checks complete private archive snapshots
and runs `apk verify` offline before creating a new directory of read-only archives.
No package is installed or target code extracted. Existing output is refused;
acquisition or verification failure creates no output directory. Save its JSON receipt outside the archive
folder so the SDK's exact file-count checks stay meaningful. Missing locked
archives fail; retain verified archive caches for later offline builds. Only
acquisition needs networking.

Before reading package content, verify every archive's checksum and official
signature with the pinned trusted keys. `scripts/prepare-native-apks.py` now owns
this step and selects only the manifest's APK-origin members:

```sh
python3 scripts/prepare-native-apks.py \
  --lock native/runtime-apks.lock.json \
  --manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --archives /private/locked-apk-cache \
  --keys /etc/apk/keys --apk /sbin/apk \
  --output /private/new-apk-root
```

Run this build-only command inside the independent acquisition image (or a verified SDK) with networking disabled,
read-only inputs, private writable scratch and bounded resources. Its Python
standard-library dependencies and trusted `apk` tool are not host requirements
for scanner users. It does not fetch archives or install packages. Output must
be new and separate from the archive/key roots. It snapshots pinned archives and
the exact trusted-key set privately, then verifies signatures on those immutable
snapshots before parsing them. Selected controls, file hashes, sizes, modes and
links must match; unselected members are never extracted and package scripts are
never run. A failure removes partial output. Pass the resulting directory as
`assemble-native.py --apk-root`.

The local offline verification used the
already locked airscan SDK's `apk` tool, with no network, package installation or
package-script execution. All 36 signatures/control identities matched; empty
trust and a corrupted archive were rejected. Selecting only the 105 recorded
members reproduced their exact current file hashes and symlinks. This supplies
an APK-installed-layout root for the assembly command below, not a new runtime
installation or a complete APK dependency repository.

The retained repository index describes the three OCR data archives as x86_64,
while their signed archive controls declare noarch. Both observations are kept:
`repository_index_architecture` preserves the index value,
`repository_architecture` selects the download path, and `archive_architecture`
matches the actual signed archive. They must not be silently equated.

The old `tesseract-ocr` package supplies selected config/data files; its 5.5.2
package version does not describe the source-built 5.5.4 engine. Package-level
dependency warnings from indexing this selected set are retained: some archives
contain tools/libraries not shipped in the native tree. They neither require
adding unused dependencies nor prove the selected ELF closure is complete.
Source builds/notices, source delivery and security/license acceptance remain
separate from these archive checks.

## Offline assembly command

`scripts/assemble-native.py` selects the pinned native tree from three explicit,
already verified input roots. Each uses runtime-relative paths, except APK files
with a `relocated_from` mapping (the CA bundle is read at its original
`etc/ssl/certs/ca-certificates.crt` path):

- `--apk-root`: installed files from verified, pinned official packages.
- `--built-root`: source-built Tesseract and the
  first-party SANE helper at their recorded runtime paths.
- `--notices-root`: pinned historical notice/source/observation files. Entries
  mapped by `notice_sources` are read from the manifest-adjacent `notice-files/`
  source directory instead, without merging input trees.

```sh
python3 scripts/assemble-native.py \
  --manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --apk-root /private/verified-apk-root \
  --built-root /private/verified-source-builds \
  --notices-root /private/verified-notices \
  --output /private/new-native-tree
```

Source-build exports can be passed directly instead of manually merging a
`--built-root`. Repeat `--built-archive` for each uncompressed tar produced by
the canonical Tesseract/helper build commands:

```sh
python3 scripts/assemble-native.py \
  --manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --apk-root /private/verified-apk-root \
  --built-archive /private/tesseract/native.tar \
  --built-archive /private/helper/helper.tar \
  --notices-root /private/verified-notices \
  --output /private/new-native-tree
```

Archive input and directory input are mutually exclusive. The command selects
only registered `native/` entries and requires complete, unique source-build
coverage with exact hashes, sizes and links. It never extracts archive paths
with `extractall`, runs build output or installs package scripts. Regular build
logs outside `native/` are ignored. Archive symlinks, special files, path escapes,
duplicate or unregistered native files and incomplete builds fail before final
assembly; temporary build inputs are removed on success and failure. Links are
created after all selected regular files. Inputs are limited to 16 uncompressed
archives, 512 MiB total and 4096 members. Runtime file modes come from the pinned
manifest, not archive headers. These checks establish runtime content identity;
callers still own SDK/source verification and upstream test success.

All paths must be supplied explicitly; output must be new and separate from the
inputs. The command checks manifest/provenance binding, complete unique ownership,
source-build identities, file size/hash and exact contained links. It refuses
symlink parents and non-regular file inputs, normalizes the recorded read/execute
modes, copies only selected entries and removes its partial output on failure.
The resulting directory is the `--native` input to `build-standalone.py`.

This supplies deterministic selection/relocation, not input acquisition or build
orchestration. Callers must verify archive signatures/hashes and execute the
recorded SDK/source recipes separately. Notices remain explicit, preserved inputs;
they are not regenerated from earlier receipts. Initial reconstruction used the
retained runtime partitioned into its three origin roles and reproduced all 191
entries and the current packager payload. That does not prove a fresh complete
APK/source reconstruction, complete source delivery, or license/security approval.

## Bundled CA default and local TLS candidate

`usr/share/scanner-kiosk/ca-certificates.crt` is required by the packager. The
launcher sets `SSL_CERT_FILE` to that integrity-checked cached file for both the
app and native wrappers when no explicit value was supplied. An explicit value
is preserved, including an empty one so it cannot silently choose different
trust; worker config rejects an explicitly empty path. Native wrappers do not
forward SSL_CERT_DIR. Go clients retain the standard Go root-store behavior.

The current input uses the official ca-certificates-bundle 20261020-r0 bytes
(118 roots, 178,791 bytes, SHA-256
`5961c0a3c65459931c023e0075c09733f09aea4ca439cb85bc4a08bb4f3f568b`).
The APK archive and installed-file checksum were checked. Its retained source
archive matches the SHA-512 in the exact recorded aports recipe. Source/header
and MPL text are included as partial notice/source materials; complete license
and recipient source-delivery acceptance remain pending.

The sane-airscan binary is now a modified, unpublished 0.99.38 source build,
not the former Alpine 0.99.36 APK binary. Its upstream commit, source/patch/SDK
hashes, GPL texts and patch are retained in the source-build notice directory.
The old notices remain identified as historical evidence.

The current b85a7dae local onefile contains these 191 entries with the updated
480e1bbd backend and matching patch/build metadata. The Go app/frontend and all
105 APK-origin entries are unchanged. In its matching fef2bd85 distroless image,
the actual cached scanimage and parameter wrappers passed 26 direct TLS cases
and 14 redirect cases. Certificate/name/expiry failures are refused, and HTTPS
cannot redirect to plaintext, including after an HTTP upgrade. Explicit HTTP,
relative HTTPS and authenticated HTTPS targets still work. The fixture adds no
native libraries and performs no acquisition. This does not establish full
admitted-job processing, real certificate/device/provider/mobile acceptance or
complete source/license/security/release approval.

## Native selected-origin SBOM

Generate a CycloneDX supplement from the assembled native tree or an
integrity-checked launcher's cache. This build-only command requires Python;
scanner users do not install it.

```sh
python3 scripts/native-sbom.py \
  --native-manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --native-root /private/verified-native-tree \
  --output-dir /private/new-native-sbom
```

Use read-only inputs, private writable scratch, no networking and bounded build
resources. Output must be new and outside the native root. The command reuses
the assembler's manifest/provenance and ownership checks, then verifies every
selected file's type, size, mode and SHA-256 and every link's exact target.
Descriptor-relative reads reject symlink parents, substituted links and special
files. It generates deterministic `native-origin.cdx.json`,
`native-origin-inventory.json` and `receipt.json`, removes partial output after
write failures and never overwrites existing output. Unselected cache helpers
and the embedded app are outside this native manifest's scope.

The supplement records selected APK files, source builds and the first-party
helper separately. It retains origin metadata, file identities and a separate
notice-file index. Unknown licenses and versions are left unknown; it does not
invent CPEs, APK versions for source builds, or dependency edges between native
components. APK configuration/data files are not evidence that the old package's
engine is shipped. Preserve the modified airscan source/patch identity.

Combine this supplement with the exact image and compiled Go inventories from
[dependency security](../docs/design/dependency-security.md). Neither output
counts nor duplicate Go records can simply be added as unique dependencies.
No binary/image identity is inferred from a caller-supplied directory: bind this
receipt's manifest/provenance hashes to the exact packaged release separately.
The command does not fetch, execute runtime files, call Syft, inspect an image,
validate against an independent CycloneDX schema, scan vulnerabilities, approve
licenses or publish artifacts. Publication/security/license flags remain false.

For the complete executable, prefer the
[onefile inventory command](../docs/design/dependency-security.md#onefile-go-and-native-inventory-bundle).
It checks the packaged binary and app/native file identities against the build
receipt, creates a private cache, reuses this native catalog and adds Syft's
compiled Go inventory. Container-base inventory remains separate.


## Personal native replacement

The [native replacement guide](../docs/design/native-replacement.md) documents
a separate working tree, provisional inventory and user-owned standalone build.
It preserves the original cache verification and does not require a signing key.
The tested path covers a non-loaded GnuTLS ELF marker and actual isolated TLS
execution, not a source rebuild or full LGPL/source-delivery acceptance.

### Selected p11-kit header supplement

The selected inputs now include20 additional unique leading notices from the
local-header/template include closure of35 selected p11-kit library C files.
This retains BSD/ISC and common/pkcs11.h's special permission, including the
version.h.in template, without adding unrelated test/mock header notices.
No local include remains unresolved in the conservative conditional closure.
Code/data bytes are unchanged. Native inputs now have299entries/188notices;
the retained8e8c75f4… candidate remains its earlier298entry/187notice build
until the next planned batch candidate. Do not infer it already contains the
supplement or refresh that binary on each documentation change.

### Independent acquisition bootstrap

`Dockerfile.apk-fetch` combines the pinned official Python image with the pinned
Alpine verifier/loader/libraries and three checksum-locked public keys. It does
not install the SDK being fetched and needs no home-directory cache. The native
SDK images remain separate, offline build inputs. Build on Linux amd64:

```sh
docker build --platform linux/amd64 --network none \
  --file native/Dockerfile.apk-fetch --tag scanner-native-fetch native
```

Use `/build-keys` and `/usr/local/bin/apk` for fetch/prepare in this image. The
adapter loads Alpine's verifier with its own musl/library path; it does not
execute a downloaded target package. The source workflow's `native-bootstrap`
job runs bounded locked acquisition and selection as UID10001 with a read-only
checkout, empty environment and private scratch. Its failure/cancellation/skip
blocks required-checks. It proves acquisition/signature/preparation only, not
SDK/source compilation, a complete candidate or publishing. No release write
permissions are added.

The bootstrap check now fetches all three committed APK locks (runtime, SANE
SDK and Tesseract SDK), each into a separate cold input directory. All archive
signatures and hashes must pass before the network-free runtime preparation.
The shared volume is limited to 512MiB; each stage has a 300-second bound and
the keeper has a 1260-second lifetime. This verifies SDK acquisition, not SDK
installation, native recompilation or a complete publishing pipeline.

## Cold native build orchestration

From a clean, committed checkout, run:

```sh
python3 scripts/build-native.py --output-dir /path/to/new-native-output
```

This controller pulls the digest-pinned official bootstrap/SDK bases, snapshots
Git source without home configuration or credentials, acquires all three APK
locks and the exact upstream Tesseract archive, reconstructs both SDKs offline,
builds Tesseract and the first-party helper, and invokes the existing verifier
and assembler. The selected airscan backend comes directly from its official
APK. No custom airscan source, patch, fork repository or running service is used.

Source and input volumes remain mounted by a bounded keeper across stages.
Compilers run with network disabled, read-only inputs/root, UID10001, dropped
capabilities and explicit empty environments. Tesseract has 4GiB RAM/swap0 and
3GiB scratch; the helper has 1GiB RAM/swap0 and 512MiB scratch. The controller
allows at most 2700 seconds before cleanup; stage timeouts can be shorter.
Only task-owned containers, temporary SDK tags and volumes are removed.

`native.tar` is the assembled payload input for the standalone build. The
helper/Tesseract tar files and source snapshot are diagnostic build inputs,
not automatically release assets. `result.json` and per-stage logs retain the
commands, source/archive hashes and failures. A failed stage or failed cleanup
leaves no final `native.tar`; an existing output directory is never overwritten.
This step does not yet build the frontend/API, freeze source/security materials,
publish a release, authorize physical acquisition or complete acceptance.

## Connecting the application and distroless output

Builders need Docker, Go 1.27.4 and Node 24.22.2. The npm client is fetched and
verified from `web/npm-tool.lock.json` inside the credential-free acquisition
sandbox, without changing host npm. Consumers need only the executable or image.
From a clean committed checkout, connect the native output with:

```sh
python3 scripts/build-native.py --output-dir /path/to/new-native-output
bash scripts/build-application.sh /path/to/new-native-output/native.tar /path/to/new-candidate
```

For a verified push event, append the exact ref and event SHA to the second
command. It reuses `build-web.py` and `build-standalone.py`; acquisition is
separate from the network-free frontend/API/launcher build. Only read-only
installed tools and Git input are exposed, with an empty child environment and
private bounded scratch. No existing home module/npm cache or auth config is
forwarded. Source/dependency changes fail the build. Native tar inputs are
filtered in the sandbox and compared to the committed runtime manifest.

The output contains `packaged/`, native SBOM/compiled Go metadata, an exact
distroless image archive and checksum/identity records. The image's entrypoint,
command, user, extracted executable hash and fresh-cache version are checked at
768MiB/swap0 with a read-only root and no network. That smoke is not a claim
that real scan/OCR workloads fit 768MiB. The local image remains available for
acceptance; task-owned build tools/containers/volume are removed.

The CI `candidate-build` job connects both commands and retains the internal
candidate for seven days, separately from bounded failure diagnostics. These
artifacts are not a preview/tag registry publication or source/license/security
approval. Source/security freeze, publisher and operator acceptance remain open.

## 2026-11-07 discovery dependency notice refresh

The bounded DNS-SD implementation adds `golang.org/x/net v0.62.0`. Its exact
module/archive checksums and source LICENSE/PATENTS are retained in the Go notice
index. The native inventory grows from295 to297 entries only through these two
read-only notice files and the refreshed index; every existing non-notice native
entry keeps its bytes, links and mode. The first-party helper selection now pins
the current go.mod/go.sum. Its expected executable remains c4a357f2… and the
candidate pipeline must rebuild it using the unchanged locked SDK before native
assembly/publication. The library versions and exact advisory tuples are unchanged;
this notice refresh does not approve new vulnerabilities or formal release gates.
