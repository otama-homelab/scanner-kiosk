# Native source inputs and reproducible builds

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


The current onefile is assembled from a selected native file closure. Its APK
package provenance describes source components; Alpine is not installed inside
the distroless runtime. A manifest match alone does not establish source
completeness, a reproducible native rebuild or permission to publish.

For the current candidate, the retained package inventory maps 36 APK-derived
components to 28 origin recipes at APK-recorded aports commits. The Tesseract
5.5.4 executable/library are a separate unmodified upstream source build;
traineddata and other retained files still use their recorded APK provenance.
Do not relabel that source build as an installed APK version or substitute the
older Debian image's source bundle for this native payload.

## Collecting recipe directories

`collect-aports-sources.py` accepts the recorded recipe receipt used by the
existing provenance inventory. Each entry supplies `origin`, `repository`
(`main` or `community`), full `commit`, `collected: true`, and the already
verified APKBUILD `sha256`.

```sh
python3 scripts/collect-aports-sources.py \
  --recipe-receipt /absolute/path/source-recipes-receipt.json \
  --output-dir /absolute/path/new-recipe-content-directory
```

The collector generates GitHub API/raw URLs itself, always at the recorded
commit. It ignores supplied download URLs, uses no token or ambient proxy,
rejects redirects, and bounds file counts, directory depth and total bytes.
It verifies each returned file's Git blob SHA-1 and records SHA-256, with an
additional comparison to the prior APKBUILD SHA-256. It stores ordinary files
as non-executable data. Unknown entry types and symlinks are rejected rather
than followed. Existing output directories are never reused or overwritten.
Partial failures retain a receipt with `complete: false`; they cannot be
promoted as a complete set of recipe contents.

`recipe-trees.json` records the retained files and selected origins. Its
`complete` flag applies only to this recipe-directory collection. It is not a
source-offer, license, native-rebuild or release approval flag. Neither the
collector nor its safety tests executes an APKBUILD or downloaded patch.
Network collection is explicit release preparation; source CI runs only the
offline safety tests and performs no automatic source upload.

## Retaining referenced upstream source files

The recipe ledger itself must be SHA-256 pinned before it can drive
`collect-aports-archives.py`. The collector verifies every recipe file against
that ledger, then reads only the literal `sha512sums` data. It neither expands
shell variables nor sources an APKBUILD. Local patch/source checksums must
match too; missing external source filenames are requested from the fixed
Alpine 3.24 source mirror and verified with their recorded SHA-512.

```sh
python3 scripts/collect-aports-archives.py \
  --recipe-dir /absolute/path/verified-recipe-contents \
  --recipe-ledger-sha256 <reviewed-recipe-trees-json-sha256> \
  --output-dir /absolute/path/new-upstream-source-directory
```

Downloads are streamed to private temporary files, with 2 GiB per file and
4 GiB per collection bounds. They become read-only retained files only after
checksum verification; existing paths are never overwritten. Auth, ambient
proxies and redirects are not used. Archives are not extracted or executed.
The receipt is saved after each source file; any missing, oversized or corrupt
source makes `source-archives.json` incomplete and the command exits nonzero,
while retaining other verified files. Its `complete` flag covers only the
files enumerated by the reviewed recipe checksums, not the whole release or
all SDK dependencies. CI runs the offline safety tests; network collection is
an explicit release-preparation action.

## Remaining native pipeline

Retain and verify all upstream archives/blobs referenced by those exact
recipes, including traineddata, plus the separate Tesseract 5.5.4 source.
Preserve the native helper's source, original build scripts/options,
relocation/closure selection, matching notices and any required source/relink
materials. Bind the SDK, transitive build inputs and transformations to fixed
identities, then rebuild in an offline bounded environment and compare the
resulting files. Do not execute a downloaded recipe in a credentialed CI job.

The [Tesseract SDK lock](../../native/README.md) now pins all 174 APK archives,
the base digest, repository snapshots and trusted keys. Offline reconstruction
matched the original installed package identities and byte-identical package
listing. Using that reconstructed SDK, a separate offline build of unmodified
Tesseract 5.5.4 passed the selected bitvector/indexmapbidi tests and reproduced
the current executable/shared library bytes and two symlink targets. This
covers those four native records; it does not prove the complete native bundle.
The SANE helper SDK now locks all 46 APKs: 17 from the fixed official Go base
and 29 exact additions recorded by the original build log. Its offline replay
verified signatures and hashes; a separate offline CGO build reproduced the
then-current 4,924,560-byte helper exactly. The CA environment bridge later
required a fresh helper rebuild in that same SDK; the native input lock now pins
the new 4,924,568-byte helper and its source/SDK hashes. Prior packaged candidates
remain historical inputs. See the native input directory for both
SDK recipes and locks. The previous native
staging also includes historical observation receipts; their dynamic fields
must not be mistaken for reproducible build inputs. That earlier checked-in manifest fixed 182 native entries; the current manifest
grew to 191 entries after the CA/fork updates below; the latest notice additions bring it to 232 entries (111 code/data and 121 notices).
`runtime-provenance.json` covers each entry with one origin/notice role and binds
the first-party helper source. A native input archive and reproducible closure
assembly still need a stable provenance layout before registry/release
acquisition is connected. Keep historical evidence
intact while defining that layout.

Complete corresponding-source and notice review, project license selection,
exact-candidate security checks and recipient-facing source delivery remain
mandatory release work. No source tree collection automatically enables
preview/tag publication or production scanner readiness.


The current native input additionally replaces the former sane-airscan APK
binary with the unpublished 0.99.38 verification fork and relocates the official
ca-certificates-bundle file. Those source roles are explicit in provenance;
the fork is not catalogued as an unmodified APK. The CA recipe and its upstream
230,531-byte archive have been retained at recorded hashes without executing
fetched source. The new 191-entry native tree has complete ELF dependency
resolution within itself and no RPATH/RUNPATH. It was packaged with the current
app/frontend into a local onefile and matching distroless candidate; this is
not complete native rebuild/source-delivery or release acceptance.


The offline [`assemble-native.py` command](../../native/README.md#offline-assembly-command)
now owns deterministic selection/relocation from explicit APK-installed, source-built
and notice roots. It binds their complete content to the native manifest and
provenance and produces the packager's native input without fetching or executing
input code. Reconstructing the retained 191 entries reproduced the current
payload exactly; this is not a fresh complete input acquisition/build or a
corresponding-source/license acceptance.

The [runtime APK input lock](../../native/README.md#pinned-runtime-apk-inputs)
now pins all 36 archives supplying the 105 APK-origin files. Their complete
checksums, official signatures and package controls were verified offline before
selective content reads; selected file/link identities matched the current native
manifest. The three OCR data packages' signed noarch identity is retained apart
from repository-index/download-path architecture. This does not acquire/build the
source-built components, regenerate notices, establish a complete package
repository, or clear corresponding-source/security/license requirements.

`scripts/prepare-native-apks.py` is the build-only offline preparation command
for that lock. It snapshots archive/key bytes, enforces their recorded hashes,
uses the SDK's trusted `apk --no-network ... verify`, then parses only the signed
private snapshots. The selected control/file/link set must match provenance and
manifest; it never installs packages or runs package scripts. Its actual 36-APK
output fed the native assembler and packager with an unchanged current payload.
The related 20 unit tests include verifier failure, input mutation after
verification, malformed selected members and output preservation. These command
checks do not supply source builds/notices or release approval. See the
[preparation and assembly commands](../../native/README.md#pinned-runtime-apk-inputs).

The canonical unmodified Tesseract source archive and SDK relation now live in
`native/tesseract-source.lock.json`; `native/build-tesseract.sh` and its build-only
input Dockerfile own the configure/build/test/export recipe. Unlike the earlier
validation wrapper, a selected upstream test failure stops the recipe before
artifact readiness. A fresh isolated offline run passed the two selected
upstream tests and reproduced all four current Tesseract runtime entries. Logs
and SDK inventory remain separate from the assembler-compatible `native/`
artifact subtree. See [source build](../../native/README.md#tesseract-source-build).
The helper/fork orchestration and complete source/license/security/release gates
remain separate work.

The first-party helper now has `native/build-sane-parameters.sh`, its SDK input
Dockerfile and `helper-source.SHA256SUMS` tied to the existing source inventory.
The recipe snapshots only checksummed Go inputs, ignores unlisted package files,
disables Go network/workspace/env overrides and exports the assembler's native
path. The actual locked SDK reproduced the current 4,924,568-byte helper exactly
even with an invalid unlisted source file present in the input image. Build info
records CGO=1/Go 1.27.3/no external modules; SDK inventory matched. This is build
identity evidence, not a scanner request, physical readiness or release approval.

The fork's own build script now shares the one-shot `native/` output convention.
A fresh fork SDK build passed URI/zeroconf tests and reproduced the current
backend without changing `airscan-http.c`. Its artifact, the canonical Tesseract
and helper build artifacts (six runtime entries in total), the verified APK root
and preserved notices were handed to the existing assembler and packager. All
191 native entries and the current app/native payload matched exactly. This
checks actual artifact composition, while full CI orchestration and fresh
complete notice/source delivery remain unfinished. Historical build receipts
inside the preserved notice input have not been rewritten as current evidence.

The assembler now accepts the canonical source-build tar exports directly via
repeated `--built-archive`, as an alternative to `--built-root`. It selects only
manifest-bound `native/` files and links, requires complete unique build coverage
and ignores regular build logs without extracting them. The actual Tesseract,
helper and updated HTTPS-downgrade-guard fork exports reproduced the current
191-entry native tree when joined with the verified APK root and preserved
notices. This removes manual unpack/merge from that handoff; it does not execute
the three builds, regenerate notices, or implement full source acquisition/CI
or recipient source delivery. See the [assembly command](../../native/README.md#offline-assembly-command).


The current native manifest additionally carries six source-managed Leptonica
1.87.0/libjpeg-turbo 3.1.3 notice/attribution/source records. The original texts
were selected from their exact retained archives, not fetched from a newer branch.
`notice_sources` binds these entries to `native/notice-files/`; the assembler uses
its existing file size/hash and no-follow guards before including them. Historical
notices remain separate pinned inputs. See [source-managed notices](../../native/README.md#source-managed-notices).
This closes those specific notice-delivery gaps only after the resulting package
has been checked; other component conditions, source/relink delivery, project
license and security/release gates remain open.


A further32 source-managed files now preserve19 original root notice texts for
Brotli, GIFLIB, libffi, libpng, WebP, libxml2, musl, p11-kit, LibTIFF, XZ, zlib
and Zstandard, with12 exact archive/runtime association records and Berkeley
attribution. This uses the existing assembler mapping without a new acquisition
or runtime mechanism. The native manifest is229entries; original197 entries
are unchanged. Root license texts can refer to per-file conditions (notably
libffi/libxml2/musl/p11-kit), and those follow-ups remain explicit in source.json.
Full source/relink delivery, other components and release acceptance stay open.

## Current source correspondence and local fork export

The latest native manifest still contains 36 APK-derived components/105 selected
files, but its 28 source origins differ from the earlier 28-origin recipe
collection: `ca-certificates` at `e78f5987…` replaces the old `sane-airscan`
APK origin. The modified 0.99.38 airscan backend remains a separate source build;
the historical 0.99.36 recipe/archive is not its corresponding source.

The current correspondence ledger rechecks all 124 selected recipe files. The
CA recipe's complete three-file directory is retained at its exact commit,
including post-deinstall and trigger scripts, and its 230,531-byte upstream
archive was rechecked with SHA256/SHA512. The ledger selects 30 upstream source
artifacts totalling 851,671,725 bytes for the current APK origins. For the other
29 archives, this latest check verifies presence/type/size and links the previous
full checksum evidence; it does not claim to have rehashed all archive contents.
The 12 first-party helper source hashes and its SDK lock still match; the
separate Tesseract 5.5.4 source archive and SDK lock were also rechecked.

A local `git archive` of the independent airscan fork's committed tree
`3836d081a8e113b53d4ed64030222b4d61a6b950` is retained as
`sane-airscan-source.tar` (1,167,360 bytes, SHA256
`3cc22666b47d1f1537d99eb4e9d5a475465e3e6938a4beb2ca69b61fc23c5fc0`).
Its C patch equals the current runtime patch exactly, and the fork's SDK lock,
build script, LICENSE and COPYING match the runtime's recorded materials.
This exports committed files without Git configuration or ignored build inputs;
it has not been extracted, executed or published. It is one local source
artifact, not a complete corresponding-source/relinking delivery or permission
to release the whole scanner. The parent plan's
`current-native-source-coverage-20261104/current-evidence.json` records the
correspondence, retained data paths and remaining delivery/license gates.

## Portable local native source inputs (2026-11-04)

The current correspondence is now assembled into a local source-only review
archive, `scanner-native-sources.tar`: 859,699,200 bytes, SHA256
`86cdd71468a4a7416e7e63df72c089ab422b775bf7ac8eaf85e4994ebc3a3b9b`.
Its 161 payload files include all 30 current APK-origin upstream source artifacts,
124 complete recipe files, the Tesseract 5.5.4 archive, the committed modified
airscan source, exact patch/build record, runtime manifest/provenance and the
application/helper source archive with SDK locks and build/selection scripts.
The outer INDEX/README make 163 regular entries; no host-local source paths are
embedded in the portable index.

Every payload SHA256 was freshly checked during streaming assembly; all 30
APK-origin archives also matched their SHA512 values and all recipe files
matched their Git blob identities. The 13 selected helper/SDK source members
and five airscan source/build/license members were checked within their source
archives. A separate isolated reader verified the completed outer archive's
163 paths/types/modes/sizes/hashes without extraction or source execution.
The assembly selected 859,444,767 payload bytes.

This supplies local source inputs for review rather than final approval.
SDK/APK binaries and compiler images are intentionally separate pinned standard
build inputs; redistributing those build-only binaries would add another
distribution scope. Go module and npm inputs are in their separate local
application/frontend source archives. A recipient-style complete native rebuild
from this archive has not yet been run. Final corresponding-source/relinking
review, individual licenses, project license choice and recipient access remain
open, as do security and operational release gates. Parent-plan evidence:
`current-native-source-bundle-20261104/current-evidence.json`.

### Airscan recipient-source rebuild

The portable native source bundle was then used as the sole fork-source input
for a separate build check. A bounded isolated reader selected its committed
airscan archive, verified SHA256 `3cc22666…`, validated all 94 source files and
normalized only the outer input-directory prefix/timestamps for the SDK image.
The fork's unchanged `homelab/build.sh` ran in the retained locked SDK
`sha256:705578ca…`, with no external network/host mounts, read-only root,
UID10001, 2CPU/2GiB/no swap and 1GiB private executable scratch.

Compilation and the URI/zeroconf upstream tests succeeded. The generated
1,246,024-byte library matched the current native SHA256
`480e1bbdac3c6088cc74e220c5df486530fe822582666cb72941a3bbea84226e`;
all 216 SDK package identities matched the provided lock. The raw build artifact
mode was 0700 under its private umask, while the existing native assembler owns
the recorded 0555 deployment-mode normalization. Temporary input image, build
container and context were removed; the locked SDK was retained.

This checks the airscan subset using the source archive with a pre-existing
verified SDK. It does not reconstruct every SDK from scratch, rebuild all APK
origins/Tesseract/helper here, validate changed-library ABI/relinking behavior,
or clear the full native corresponding-source/license/security/release gates.
Parent evidence: `native-recipient-airscan-rebuild-20261104/current-evidence.json`.

## Portable notice inputs

All 187 current native notice inputs now live under native/notice-files and
are mapped by runtime-provenance.json. The 80 retained inputs preserve the
existing runtime bytes, including historical inventories and recipes; their
internal paths and older package claims are context, not current host/build
requirements. Native assembly no longer needs the original local notice working
directory. Pass a new empty directory for --notices-root with this manifest;
the generic assembler retains that argument for older caller-supplied manifests.
Source CI requires every notice to be mapped into the checkout and verifies
its bytes. This changes input acquisition only, not the current binary or
complete license/source/publication acceptance.

## Fetching pinned binary build inputs

`fetch-native-apks.py` obtains the exact APK filenames recorded by the runtime or
supported SDK lock from fixed filename URLs. It snapshots the pinned
public keys, uses only the fixed Alpine 3.24 main/community repositories with
proxies and redirects disabled, checks downloaded file type/size/SHA-256 and verifies signatures offline
before publishing a new local archive directory. Failed acquisition or
verification produces no output directory and never installs package scripts.
Run acquisition without credentials in a bounded sandbox with networking, then
run preparation, source compilation and assembly with networking disabled.
See the [input command](../../native/README.md#pinned-runtime-apk-inputs).

This command prepares binary build inputs, not corresponding source, an SDK
redistribution approval, the complete native build workflow or a release.
