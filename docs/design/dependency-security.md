# Dependency inventory and vulnerability evidence

This workflow prepares local release materials. A complete inventory or scan is
not a release pass. The project has no selected license, published source/tag,
approved vulnerability exception policy or production deployment acceptance.

## Inputs and outputs

`scripts/image-sbom.py` catalogs an immutable local image as described in
[historical Debian image packaging](debian-image-packaging.md). This Debian-oriented
image workflow is not the complete inventory procedure for the current onefile
inside distroless; see [current packaging](image-packaging.md). The runtime Syft JSON is the input to
Grype, retaining Debian distro metadata and exact compiled Go module versions.
The scanner never needs Docker credentials or production content.

For the current onefile, catalog both the immutable distroless image archive
and the private cache produced by the integrity-checking launcher. An image
scan sees the outer Go launcher and distroless packages, but misses the embedded
app's modules and native code. A cache scan discovers the Go modules, but has no
installed APK database and cannot identify the native C package origins.

The local 2026-11-04 inventory retained these two Syft/CycloneDX outputs plus a
separate provenance-derived native CycloneDX supplement. The supplement binds
`native/runtime-manifest.json` and `runtime-provenance.json` to the actual packed
manifest and rehashed cached files. It records 36 selected APK origins, two
source-built components and the first-party SANE helper; The initial snapshot contained 111 runtime code/data
entries and 80 preserved notices. Later source-managed additions bring the
current tree to 232 entries: 111 code/data entries and 121 notices. Package records
represent selected archive files, not complete installed packages. In particular,
the retained 5.5.2 Tesseract APK configs/data are separate from the source-built
5.5.4 engine, and the modified airscan fork keeps its patch/source identity.

Keep all three inventories and their input hashes together. Counts overlap and
must not be added as unique packages. The native supplement is an explicit
origin inventory, not Syft auto-detection, a dependency graph inferred from ELF,
a complete recipient source offer or a vulnerability/license pass. `scripts/native-sbom.py` now generates that native supplement from verified
files using the assembler's shared identity checks; see the
[native command](../../native/README.md#native-selected-origin-sbom). The integrated
image/Go/native release producer remains unfinished. The three current local
CycloneDX documents separately passed the official 1.6/1.7 schemas pinned to a
CycloneDX specification commit, using an independent offline validator. Missing
type, malformed digest and invalid SPDX-ID controls failed as expected; unknown
schema retrieval was blocked. Optional URI/IRI/date-time format checkers were
unavailable, so those annotations remain unverified. Schema validity is distinct
from inventory completeness, vulnerability/license acceptance and release gates.
The producer itself does not run this independent validation.

## Onefile Go and native inventory bundle

`scripts/onefile-sbom.py` combines the compiled Go inventory with the existing
native supplement. Run it inside a bounded offline build sandbox, with read-only
inputs and private scratch. It requires no Docker socket and never publishes.

```sh
python3 scripts/onefile-sbom.py \
  --binary /private/packaged/scanner-onefile-linux-amd64 \
  --build-receipt /private/packaged/build.json \
  --packed-manifest /private/packaged/manifest.json \
  --native-manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --syft /path/to/verified/regular/syft \
  --output-dir /private/new-onefile-inventory
```

The binary and tool must be regular files. Output must be new; its parent must
exist in assigned scratch. The command checks binary/app/native identities
against the packaging receipt and manifests before executing `version`. It uses
an empty allowlisted environment and a new private cache, verifies the actual
packed app and native files, then runs the native producer and the supplied
trusted Syft. No host cache, credentials, package installation or dependency
acquisition is involved. Scratch and private runtime caches are removed on
success or failure. A conflicting output created during the scan is preserved;
only the producer's own failed partial output is removed.

Outputs include `onefile.syft.json`, `onefile.cdx.json`, the three native
producer files under `native/`, tool/version logs and a receipt binding all
artifact hashes to the input binary/build/manifest and tool hashes. Raw Go
records may repeat for separate executable locations. The command checks that
Go records and CycloneDX components exist; independent schema validation remains
a separate gate. Neither package counts nor native and Go records should be
summed as unique dependencies.

This is the executable's inventory bundle. Its receipt explicitly excludes the
container base. Retain the separate immutable image SBOM and image/binary
identity proof alongside it; the producer does not claim they match merely
because a directory was supplied. Vulnerability/license/publication flags stay
false. The combined image/onefile release gate and hosted publisher remain
unfinished.

## Declared build inputs

`scripts/dependency-inventory.py` separately catalogs declared build inputs:

```sh
python3 scripts/dependency-inventory.py --syft /path/to/official/syft \
  --runtime-sbom /path/to/runtime.syft.json \
  --output-dir /tmp/scanner-build-dependencies
```

Only `go.mod`, `go.sum`, `web/package.json` and `web/package-lock.json` are copied
to a private temporary directory. Syft's Go module file and JavaScript lockfile
catalogers are selected explicitly. Host node_modules, generated assets and
credentials are never scanned. Every package/version declared in the npm lock
must appear in the resulting Syft SBOM. Optional packages for other operating
systems/architectures are included, so this is a declared dependency graph,
not a list of packages installed in the Linux/amd64 build stage. CycloneDX and
lockfile license metadata are saved separately from the runtime inventory.

For third-party Go modules actually found in the runtime, the script downloads
the exact versions outside the application directory, checks module and go.mod
checksums against the committed go.sum, and runs `go mod verify` before reading
their extracted cache. It copies files named LICENSE/LICENCE/COPYING/NOTICE/
COPYRIGHT/AUTHORS/PATENTS, including nested files, into an indexed notice bundle
with content hashes. Go's own notices come from the pinned Go 1.27.3 toolchain.
This is reproducible source evidence, not a classification of every license or
an assertion that all distribution obligations are satisfied. The Docker build
now independently collects the runtime Go notices and includes them in the
candidate image. `check-image.py --go-notice-index ...` compares all file hashes
with this source bundle. Review of obligations and any remaining required
distribution material is still pending. No license is assigned
to the two scanner-kiosk module records merely to eliminate unknown metadata.

## Retaining Debian source files

`collect-debian-sources.py` retains the files listed in a previously authenticated
Debian source manifest. Pin the reviewed manifest hash explicitly and supply the
runtime Syft SBOM to which the bundle must correspond:

```sh
python3 scripts/collect-debian-sources.py \
  --manifest /path/to/authenticated/source-manifest.json \
  --manifest-sha256 REVIEWED_SHA256 \
  --sbom /path/to/runtime.syft.json \
  --output-dir /private/scanner-debian-sources
```

Manifest authentication is a prerequisite: a caller-supplied hash only pins the
input bytes; it does not verify Debian signatures or source-index membership.
Keep the signed Release/source-index verification evidence with the bundle.
The collector checks the complete Debian binary and source identity inventory
against the SBOM, including Debian's absent-source-version defaults. It refuses
duplicate/conflicting mappings, unsafe filenames, oversized collections and URLs
outside pinned HTTPS Snapshot pool paths. Redirects remain on the Snapshot HTTPS
origin and proxy discovery is disabled. No source archive is extracted or run.

Up to four independent files are retrieved concurrently. Each file is streamed
into a temporary file, size/SHA256 checked and published
without replacing an existing destination. The output directory must be private;
source symlinks and corrupt existing files are rejected. A failed receipt keeps
verified files. Repeating the same command rehashes those files and downloads
only missing files. Inputs for another bundle are rejected. A final verification
rehashes every source file before setting `passed=true`; preserve the manifest,
SBOM, receipt and files together. Source collection tests run offline via
`python3 -m unittest discover -s scripts -p test_collect_debian_sources.py -v`.

This is a local source-file bundle, not an implemented public source offer or a
finding that it is complete corresponding source under every applicable license.
Still review build scripts/instructions, binary modifications, notices,
replacement/relinking and actual recipient access for the exact release. The
bundle covers Debian packages only; Go dependencies, project source, fonts and
browser material have separate obligations. The current license/security gates
remain open.

## Vulnerability scan procedure

Use an unmodified official [Grype release](https://github.com/anchore/grype/releases)
with the release archive SHA256 verified. Populate its private DB cache first,
then run scans serially against that cache:

```sh
GRYPE_DB_CACHE_DIR=/path/to/private/grype-db \
  GRYPE_CHECK_FOR_APP_UPDATE=false /path/to/grype db update
python3 scripts/check-vulnerabilities.py --grype /path/to/grype \
  --cache-dir /path/to/private/grype-db --sbom /path/to/runtime.syft.json \
  --output-dir /tmp/scanner-runtime-vulnerabilities
```

Repeat the checker with `declared-build.syft.json` and another output directory.
The checker uses an explicit empty ignore configuration and a minimal tool
environment. It disables automatic DB updates during scans, retains age
validation, and records database metadata/file SHA256, tool/script/input hashes,
all findings and raw JSON/log output. There is no `only-fixed` filter, VEX or
severity suppression. Never interpret `wont-fix` as safe or omit it from review.

The local candidate threshold is `--fail-on high`, whose Grype exit status 2
is preserved as `passed=false`. `scan_completed=true` distinguishes a completed
scan with findings from tool failure. An initially aborted scan while serializing
the first database update remains incomplete; it is not a clean result. This
technical threshold does not establish a production release policy. All lower
and unknown severities remain in the reports too.

## Initial findings and remediation

The 2026-11-02 baseline runtime scan completed with 577 package/advisory matches,
including Critical 18, High 160, Medium 169, Low 57, Negligible 171 and Unknown 2.
These are matches, not a count of unique vulnerabilities. The local High gate
failed. Only three matches had a fixed version in that database, all in
go-jose v4.1.4. The declared build graph independently reported the same three
and one Unknown Go advisory.

The application now pins unmodified upstream go-jose v4.1.7. The change is only
that version and its two new go.sum entries. Upstream's [RSA JWK advisory](https://github.com/go-jose/go-jose/security/advisories/GHSA-mgcc-cww5-fp2q)
identifies v4.1.6 as the fix for the High finding. The [v4.1.7 release](https://github.com/go-jose/go-jose/releases/tag/v4.1.7)
also contains additional security fixes published after it. Do not infer that
the vulnerability database already covers every recent upstream advisory.

OS matches remain a release issue, not automatically accepted exceptions. For
example, Debian's [curl CVE-2026-19931 record](https://security-tracker.debian.org/tracker/CVE-2026-19931)
lists the installed trixie security version as vulnerable with a no-DSA note;
the scanner reports it as Critical/wont-fix. Debian's [TIFF CVE-2026-52490 record](https://security-tracker.debian.org/tracker/CVE-2026-52490)
describes a tiffcrop tool path while the inventory match is on libtiff6. Those
distinctions guide further analysis, but neither a no-DSA note nor an inferred
unreachable path suppresses the result here. No cross-distro package mixing,
upstream patch build, dependency removal or blanket waiver was used to turn the
gate green.

The post-update and notice-bundled image scan completes with 574 matches:
Critical 18, High 159, Medium 167, Low 57, Negligible 171 and Unknown 2. These
span 279 advisory IDs: 573 matches are Debian packages and one is the Unknown
Go OpenPGP advisory discussed below. The database reports no remaining matches
with an available fixed version; this does not mean the findings are safe.
The raw High gate remains failed. The declared build inventory has one Unknown
match and no High/Critical matches, so only its local High threshold passes.

The image includes 47 source notice files for all 21 runtime third-party Go
modules plus stdlib notices, checked against the independently collected source
files. A separate continuity receipt compares before/after SBOMs: all 254
package tuples and both compiled scanner/native-helper SHA256 hashes are
unchanged by adding notices. The declared build inventory remains applicable
to the same unchanged go.mod/go.sum/npm manifests. The notice-bundled image was
nevertheless scanned again using its own new runtime SBOM. npm lock metadata
contains license fields for all 260 declared entries; actual npm notice text
distribution and build-stage OS/toolchain inventories still need verification.

## Advisory import evidence

[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) identifies the unmaintained
OpenPGP packages within golang.org/x/crypto. The current official record lists
`v0.58.1-0.20261009181718-2f1f834330b2` as a fixed parent-module pseudo-version;
its separate OpenPGP-module entry has no fix. The observed stable proxy latest
is v0.58.0. The earlier blanket statement that no fixed module version exists
is superseded by that official record. No dependency update or exemption was
applied. The raw module-level finding is retained. A separate reproducible check compares
the official advisory's exact import paths with the product build graphs:

```sh
python3 scripts/check-go-imports.py --advisory /path/to/GO-2026-5932.json \
  --receipt /tmp/scanner-go-imports.json
```

The receipt covers scanner with CGO=0/webui and the native helper with
CGO=1/saneparams, both Linux/amd64/Go 1.27.3. It records all imports and proves
only whether the specified packages are absent. It is not general function
reachability analysis, a signed VEX statement or a reason to delete Unknown
findings. The full module matcher warns that missing captured function symbols
can cause conservative false positives; that limitation remains explicit.

The parent plan/task index and final receipts record actual post-update scan
counts and regression results. Prior image/SBOM/findings are retained with
`before-jose` names. Clean-install/upgrade/rollback, license obligations,
remaining OS triage/remediation, build-stage OS/toolchain inventory, actual CI,
real device/mobile/IdP and release/cutover gates remain open.


## Critical runtime triage (2026-11-03)

This is a scoped review of the 18 Critical matches in runtime image
`sha256:0d459ae6b02b089b33d60181f7fbb4eb987bb5be440cb523fd5c01c4f10d042a`.
They represent 10 distinct CVEs: eight curl IDs each matched to both installed
libcurl packages, plus one tiff and one libxml2 ID. Raw scan totals remain 574
and the security gate remains failed. No ignore, VEX, severity override, package
change or exception approval was applied. High and other severities, frontend
and Go-build stages are outside this triage's coverage.

The parent task records `runtime-critical-debian-primary-triage-20261103.json`,
`runtime-curl-critical-primary-triage-20261103.json` and
`runtime-critical-component-metadata-20261103.json` bind primary-page hashes,
exact installed versions, immutable image identity and bounded local observations.
All ten Debian tracker pages label the exact installed trixie version vulnerable.
The scanner's `wont-fix` field is distinct from Debian's no-DSA/minor-issue notes;
it does not establish that a component is unused or will never receive a fix.

| curl CVE | curl's own severity (separate from scanner Critical) |
|---|---|
| [CVE-2026-10536](https://curl.se/docs/CVE-2026-10536.json) | Low |
| [CVE-2026-11856](https://curl.se/docs/CVE-2026-11856.json) | Medium |
| [CVE-2026-18924](https://curl.se/docs/CVE-2026-18924.json) | Low |
| [CVE-2026-19931](https://curl.se/docs/CVE-2026-19931.json) | Medium |
| [CVE-2026-86414](https://curl.se/docs/CVE-2026-86414.json) | Low |
| [CVE-2026-8926](https://curl.se/docs/CVE-2026-8926.json) | Low |
| [CVE-2026-9079](https://curl.se/docs/CVE-2026-9079.json) | Medium |
| [CVE-2026-92388](https://curl.se/docs/CVE-2026-92388.json) | Low |

All eight curl metadata documents list upstream 8.14.1 as affected; this alone
cannot establish Debian patch status. Debian separately marks installed
8.14.1-2+deb13u7 vulnerable. Authentication/proxy/connection-reuse prerequisites
still require caller-level review; vendor severity does not prove unreachability.

For CVE-2026-19931, [Ubuntu's assessment](https://ubuntu.com/security/CVE-2026-19931)
differs from Debian's: it treats its releases as not affected based on upstream
behavior/documentation rather than a maintained special check. The upstream
[removal commit](https://github.com/curl/curl/commit/1c58877705b172da892cb9c18c5edb572f2d1d73)
and [current username API contract](https://curl.se/libcurl/c/CURLOPT_USERNAME.html)
place ambient-user connection reuse responsibility on the caller. The source
positions are retained in `curl-19931-source-discrepancy-20261103.json`; an advisory
fixed-version range or switching distro solely for a lower count is insufficient
to prove the application handles these conditions safely.

[CVE-2026-52490](https://security-tracker.debian.org/tracker/CVE-2026-52490)
concerns `tools/tiffcrop.c`. The exact image SBOM has libtiff6 but no tiff tools
package; a bounded read-only/no-network container search found no `tiffcrop`
path under `/usr`. This supports a component-absence candidate for that tool,
not a blanket libtiff exclusion. The upstream issue identifies the tool path,
but the upstream fix patch could not be retrieved. A later exact-source review
is described below; no formal not-affected statement or scanner suppression is
issued. Actual airscan still links libtiff.so.6.

[CVE-2026-6653](https://security-tracker.debian.org/tracker/CVE-2026-6653)
remains a native-path review priority: both airscan and scanimage link
libxml2.so.2 in the exact image. The distro version contains `really2.9.14`;
the apparent 2.12.7 prefix is not proof of an upstream fix. The native parser's
call sites/options and trusted-input boundary need review alongside a supported
fixed-package path. The separate Go eSCL XML bounds do not prove the native
parser cannot receive hostile input.

Exact sane-airscan 0.99.35-1 source inspection strengthens that priority:
`airscan-xml.c`'s `xml_rd_parse` constructs a parser context, resets it with the
input buffer and calls `xmlParseDocument`. The orig/debian archives (222,097
unique bytes) matched SHA256 values in the DSC already bound to the authenticated
Debian Sources index. Archives were inspected in memory and discarded, with no
source execution. `airscan-native-xml-source-triage-20261103.json` records archive
and member hashes, API references/lines and the current-SBOM version match. This
is source correspondence and call-site evidence, not a reproduced CVE trigger or
proof that the published binary is reproducible from that source.


The metadata container used the image's non-root UID, read-only root, no network
or added capabilities, empty process environment, 1 CPU, 128MiB memory, 32 PIDs,
1MiB tmpfs and a 60-second timeout. It only queried packages/paths/linkage, ran no
exploit or capture and was removed successfully. Production and source code were
unchanged. Existing broader scan failures and real-device/release gates remain.

## Current-image Critical refresh and TIFF source separation

For development image
`sha256:3467328c8466552f86d894eb8002277c8c11c6368580eb0009a2ae2150c7ca81`,
the ten Critical advisory IDs were checked again against their primary Debian
tracker pages on 2026-11-03. Each exact installed source version still had a
vulnerable Trixie row. `runtime-critical-debian-refresh-20261103.json` in parent
task 43 records response hashes and selected rows. This is tracker status, not
an APT update trial, a severity override or exploitability proof. A newer
upstream/testing version does not establish a supported stable update path.

For [CVE-2026-52490](https://security-tracker.debian.org/tracker/CVE-2026-52490),
the exact installed tiff 4.7.0-3+deb13u3 source orig/debian archives were fetched
in memory and matched against the SHA256/size from authenticated Debian Sources
metadata. The named `process_command_opts` symbol was found only in
`tools/tiffcrop.c` among inspected source files. The tools Makefile assigns that
file to the separate tiffcrop executable; the inspected libtiff build files do
not name it. The Debian patch touching tiffcrop changes tile-buffer handling
and main, without an observed change to the reported option multiplication.
`tiffcrop-installed-source-triage-20261103.json` records archive/member hashes,
build markers, symbol positions and patch hunk metadata. About 2.14MB of archives
were inspected and discarded, with no filesystem extraction, source build or
exploit execution.

This strengthens the tool-absence candidate already supported by the runtime
inventory/search. It is not a reproducible-build or complete reachability proof,
does not clear other libtiff issues, and does not make native libxml2 safe. The
[upstream issue](https://gitlab.com/libtiff/libtiff/-/work_items/846) identifies
the tiffcrop option path; the fixed commit's patch still returned 403. No VEX,
ignore, package change or distro change was made. The raw current-image gate
remains failed at 574 matches, including 18 Critical and 159 High matches.


## Scoped offline govulncheck analysis

Official govulncheck v1.9.0 was built with Go1.27.3 and module checksum
verification. The diagnostic scanned the actual API, onefile launcher and native
Go helper without executing them. Containers had network none, read-only root,
UID10001, bounded CPU/memory/process/tmpfs and no host mounts.

The local v1 database retained the complete official indexes plus **all 235
advisory records referenced for the 22 candidate/control module paths**. No
version, severity or withdrawal filter was applied during collection. The
snapshot reports modification time 2026-11-02T22:01:43Z; each record was checked
against its index modification time and hashed. This is complete for those
inputs, not a global Go vulnerability mirror. Regenerate and validate the cache
when modules change. A missing requested record is an error, with no network
fallback in the scan containers.

```sh
govulncheck -db=file:///path/to/validated-db -mode=binary -scan=symbol -format=json /path/to/binary
```

JSON mode can exit zero even when findings exist: parse its finding stream rather
than using the exit status as a gate. A compiled, never-executed OpenPGP control
produced GO-2026-5932 findings; official text mode returned exit 3 as expected.

The stripped API binary produced one module-level finding and seven wildcard
placeholders for GO-2026-5932. These are the official tool's conservative
stripped-binary fallback, not evidence of seven concrete linked OpenPGP functions.
See the pinned tool's `internal/vulncheck/binary.go` fallback for missing symbol
information. The raw findings remain unchanged. Launcher/helper binary scans
reported no findings at this snapshot; stripped-binary analysis still has that
precision limit.

A second offline **source** analysis used matching product source, Go1.27.3,
CGO0 and the `webui` tag. The positive control detected the actual OpenPGP
`ReadMessage` call. API source analysis retained the one module-level warning,
with zero affected-package/call findings. This agrees with the independently
recorded package import graph. It is static-analysis evidence with dynamic-call
limitations, not a signed VEX, ignore rule or full security clearance.

Raw JSON, positive controls, complete queried-record inventory, source/target
hashes, initial input-permission failure and cleanup receipts are indexed in the
parent plan at `tasks/43/go-vulnerability-binary-20261103/current-evidence.json`.
Native C libraries, distroless OS, the known native HTTPS peer-authentication gap,
full dependency/license/source delivery and release gates remain separate work.

## Fixed native input: offline Alpine secfixes comparison

The checked-in native manifest and origin mapping can be compared with retained
public `https://secdb.alpinelinux.org/v3.24/main.json` and `community.json`
snapshots. Acquire those data files separately over HTTPS, retain their hashes,
and run with the official `apk` version comparator in an isolated Alpine SDK:

```sh
python3 scripts/check-native-secfixes.py \
  --apk /sbin/apk \
  --native-manifest native/runtime-manifest.json \
  --provenance native/runtime-provenance.json \
  --main-secdb /inputs/main.json \
  --community-secdb /inputs/community.json \
  --output /scratch/new-native-secfixes-report.json
```

The command never downloads, executes the application or changes dependencies.
It checks manifest/provenance identity and repository metadata, uses `apk` for
Alpine version ordering, retains every comparison and lists unrecorded origins.
A fix for an advisory already addressed by an older backport is not reported
again solely because another branch also records that advisory. Source-built
Tesseract remains separate from APK-origin comparisons. Existing reports are
never overwritten. Exit 2 means newer fix records were found; exit 0 means only
that this comparison finished without newer recorded fixes. Errors fail the
command. `release_gate_passed` remains false in every report: this source does
not cover all unfixed CVEs, native HTTPS authentication, Go, distroless OS or
license acceptance.

The current fixed native inventory was compared with newly acquired official
snapshots on 2026-11-04. Twenty-seven of 36 APK-origin components had secfixes
records, nine were unlisted, and no newer fix records were found. An isolated
synthetic old-libxml2 control produced exit 2; repeat output creation refused to
overwrite its report. These are scoped comparison results, not clearance of the
current onefile or distroless candidate. Source CI runs the four offline safety
tests only; snapshot acquisition and real `apk` comparison are separate checks.


## Current Go binary and source-package checks, 2026-11-04

The local onefile candidate `85202ac0…` was checked with the retained official
govulncheck 1.9.0 and freshly retrieved complete official v1 indexes. The
database's published modified time remained 2026-11-02T22:01:43Z. All 235 records
referenced by the 22 candidate/control module paths were retained without
severity, affected-version or withdrawal filtering. Previously retained records
were reused only when their original SHA256 and current index modified timestamps
matched. This is a target-scoped database snapshot, not a global mirror.

After actual integrity-checked extraction, the exact app 29b2b6b9, helper 0266e081
and outer launcher 85202ac0 were analyzed offline. The launcher/helper had no
findings at this snapshot. The API retained GO-2026-5932 for x/crypto 0.58.0: one
module finding, seven package indicators and seven function fields ending in
`/*`. Those wildcard fields are not exact compiled function names or evidence of
call reachability. Do not interpret JSON mode's successful exit as a clean scan
or discard the raw indicators.

An additional `source`/`package` analysis used current tracked runtime sources,
Linux/amd64, CGO disabled, the webui tag and the same five verified embedded
assets. Its 21 third-party module versions matched the actual API binary. The
module finding remained, with zero affected-package findings for this app. A
separate OpenPGP-using source control produced six affected-package findings.
This supports package-level triage for this exact build; source call analysis,
VEX and vulnerability acceptance were not performed or implied.

The known vulnerable binary control produced the advisory and official text
mode exit 3. Removing its required GO-2026-5932 record from a private database copy
caused exit 1 instead of a silent clean result. All target processes had no
external networking. The initial local classifier incorrectly treated path
wildcards as exact function names; the raw JSON was preserved and classification
corrected for fields ending in `/*`.

The Go binary gate remains uncleared, and the overall vulnerability/release
gates remain false. Native C/OS advisory matching, the source-built fork, full
component/license/source delivery and scanner operational acceptance are separate
requirements. These checks were local; no hosted security job or publisher was
added by this observation.


## Current native and image Grype checks, 2026-11-04

The unchanged native manifest `5993f9ca…` and native-origin CycloneDX
`eaf1b142…` completed an official Grype 0.121.0 scan in 8.009 seconds, with
40 imported packages, zero matches and zero ignored matches. Alpine 3.24.3 was
recognized. Database schema 6.1.10 was valid, with both hash and age validation
enabled. The CPU quota and GOMAXPROCS were both two; earlier timeout evidence is
retained. This run does not isolate CPU count as the cause of the earlier delays.
The fork, helper, Tesseract and metadata component had no CPEs, so zero matches
does not clear those source-built components or prove distro feed completeness.

A fresh Syft 1.55.0 inventory of current image `636cd516…` verified the exact
image configuration digest and every layer diff ID from its Docker archive.
It contains six Debian packages and two outer-launcher Go components. Official
Grype completed that inventory in 14.725 seconds, recognized Debian 13.7, and
reported zero matches and zero ignored matches with the same validated database.
Neither scan added custom ignore rules; official built-in defaults remained.

Both scans ran locally without external networking, with read-only tools,
inputs and database, UID/GID 10001, two CPUs, 2GiB RAM, no swap and a 90-second
scan deadline. Separate embedded API/helper/native inspection is still required;
image scanning cannot see those compressed payloads. Whole-product security,
Go reachability, source-built coverage and release gates remain open. Raw
receipts are in the parent plan's `native-grype-cpu-aligned-20261104`,
`current-image-inventory-20261104` and `current-image-grype-20261104` directories.


## Current source symbol analysis, 2026-11-04

Official govulncheck 1.9.0 completed source `-scan=symbol -tags=webui ./...`
with Linux/amd64, CGO disabled and Go 1.27.3. All 352 selected Go/SQL/module
files matched the current candidate's recorded build inputs, and all five
embedded assets were hash-verified before analysis. The retained official
snapshot's 237 files (two indexes and 235 advisories) were freshly rehashed
against acquisition receipts; this run fetched no new database data.

The application retained the GO-2026-5932 module warning but had zero affected
package or symbol findings. A separate source fixture calling OpenPGP
`armor.Decode` produced three symbol findings, including an explicit
`main → armor.Decode` trace. Module, package and symbol findings were classified
separately; JSON exit zero alone was not used as evidence of absence.

This closes the missing local source symbol analysis for that configuration.
It does not cover the bundled-tag launcher or CGO/native helper, issue a VEX,
or establish whole-product security acceptance. Existing binary wildcard
warnings and earlier failures remain retained. The raw source/control results,
input hashes, database provenance and sandbox limits are in the parent plan's
`current-go-symbols-20261104/current-evidence.json`. The run used no external
network, read-only inputs/toolchain/modules, UID/GID 10001, two CPUs, 4GiB RAM,
no swap and bounded private scratch; its owned staging directory was removed.


## Inferred-CPE supplement and Tesseract triage, 2026-11-04

A separate official Grype run with `--add-cpes-if-none` completed in 3.003
seconds and exited 2 at the unchanged high-severity threshold. It reported eight
NVD CPE matches (five high, three medium), all against Alpine
`tesseract-ocr 5.5.2-r0`: CVE-2026-88047 through CVE-2026-88054. These findings
remain in the raw report with zero ignored matches. The earlier default scan's
zero-match result must not be read as clearing this supplementary scope.

The APK contributes only 33 read-only configs/font files under tessdata. Actual
Tesseract executable/library files are separately built 5.5.4, source commit
`7e548e6e…`, and match current native provenance/manifest hashes. Official
upstream comparison metadata places all eight referenced fix commits in that
source commit's ancestry. Ancestry alone does not prove a fix was not reverted.

The exact retained source archive was hash-verified, then inspected without
extracting or executing it in an isolated sandbox. Every changed production
source file was identical to the corresponding fixed-commit Git blob for
CVE-2026-88047/88048/88049/88050/88051. Files for 88052/88053/88054 changed later
and need further diff review. These are scoped source-identity results, not
exploit regression tests, recipient binary rebuilds or a VEX. No finding was
suppressed, no component version relabelled and no release gate cleared.

Raw matches, bounded official upstream metadata, source-file identities and
remaining triage are recorded in the parent plan's
`native-grype-cpe-supplement-20261104/current-evidence.json`. External requests
retrieved only public metadata; source inspection and scanning had no external
network and used read-only inputs. Both owned units terminated and staging was
removed. Other source-built components still need advisory coverage.


### Follow-up review of the three changed source cases

The later deltas for CVE-2026-88052/88053/88054 were reviewed against the
retained exact source archive. Nine changed-since-fix source files matched the
current Git blob identities from official upstream compare metadata; plumbing
retained the earlier exact-fixed-file proof. Nine explicit source checks across
ten files passed in an isolated, read-only-input inspection.

For 88052, the positive count and inserted-vector-size checks remain before
property writes; the later delta adds a MAX_NUM_CLASSES limit. For 88053, the
class/pruner/proto-set limits and initialization error propagation remain;
later changes add runtime class-ID checks, earlier neutralization of file-sourced
pointers and adapted-template construction failure handling. For 88054, empty
stack/one-element-series rejection and LSTM Load failure cleanup remain; the
later tessedit change adds a separate unicharset limit.

These three follow-up static reviews are complete. Together with the five
exact-fixed-source identities, they support scoped source triage of the eight
retained findings; they do not prove the shipped binary was rebuilt from those
sources or replace exploit regression tests. No ignores or VEX were added, and
the raw high-severity scan exit remains unchanged. Recipient Tesseract rebuild,
other source-built coverage and overall security/release acceptance remain open.
Evidence: parent plan `tesseract-followup-source-review-20261104/current-evidence.json`.


### Recipient-source Tesseract rebuild

The retained portable native-source bundle supplied both the unmodified 5.5.4
source archive and its canonical build script, checksum file and SDK lock. Each
selected outer member and the selected build-control files were hash-verified;
no developer checkout supplied source to the build. A fresh isolated build in
the retained locked SDK reproduced the current executable SHA256 `5cc98558…`
and shared library `32fbf741…` byte-for-byte, with both symlink targets matching.
All 174 SDK package identities matched the lock. The canonical two selected
upstream tests (bitvector and indexmapbidi) passed; the container exited zero
without OOM. Raw build mode 0700 and assembler deployment mode 0555 remain
separate checks.

This binds the reviewed source fixes to the current binary bytes. It closes
the missing recipient-source Tesseract rebuild proof, but the two upstream tests
are not exploit regressions for these eight CVEs. The SDK was retained rather
than reconstructed anew, other native components are outside this rebuild,
and source-delivery/license/security/release approval remains incomplete.
Evidence: parent plan `native-recipient-tesseract-rebuild-20261104/current-evidence.json`.
The build used two CPUs, 3GiB RAM, no swap, a 1GiB private scratch volume, no
host mounts/network, read-only root and UID/GID 10001. Owned container, input
image tag and staging contexts were removed; the locked SDK was retained.


### Recipient-source SANE helper rebuild

The same portable native-source bundle supplied the helper's 12 checksummed
Go/module inputs, canonical build script, checksum table and SDK lock. The
selected build-control archive and every selected input were hash-verified.
A fresh offline build in the retained locked SANE SDK reproduced the current
helper SHA256 `0266e081…` (4,924,568 bytes). All 46 SDK package identities matched;
build metadata confirmed Go 1.27.3, CGO enabled, the saneparams tag and no external
Go module dependencies. Raw mode 0700 and deployment mode 0555 were checked
separately. This complements the recipient-source Airscan and Tesseract proofs;
it does not rebuild every APK dependency or prove a fresh SDK reconstruction.

The initial source-root selection failure occurred before source execution.
The first compile then completed, but collection failed because the SDK lacked
Python. Both failures are retained. A retry used a bounded descriptor-validated
Go collector inside the same sandbox and completed normally. No product code,
build script or private-store guard changed. No new scanner/device test ran.
The owned containers, input image tag and staging were removed; the locked SDK
was retained. Evidence: parent plan
`native-recipient-helper-rebuild-20261104/current-evidence.json`.
