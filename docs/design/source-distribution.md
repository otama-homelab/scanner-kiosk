# Source delivery for standalone and container releases

Status: selected delivery method; recipient delivery is not implemented or
verified yet. First-party code is Apache-2.0. This document does not approve the
native closure for release. See the [notice index](../../THIRD-PARTY-NOTICES.md)
and [distribution review](license-distribution.md).

## Delivery method

Publish the exact corresponding source as downloadable release assets alongside
each distributed binary, with the same recipient access and no additional fee.
Keep those assets available while offering the binary. Do not rely on a written
source offer, a home-directory path or an upstream link that may disappear.

For GPL-2.0 material the intended route is §3(a), including the network download
provision following §3(c). For GPL-3.0 material requiring source delivery use
§6(d). For LGPL-2.1 libraries preserve notices and library source under §§4/6;
for LGPL-3.0 preserve the GPL terms and the applicable §4 combined-work materials.
Final component review must select the actual applicable version and exception;
these routes do not assign one license to every file in a component.

A separate CLI's GPL does not automatically relicense the independent API.
SANE/airscan linking exceptions apply only to the files granting them, not to
scanimage as a whole. Preserve the exceptions and the modified sane.h record.
The selected airscan backend is the unmodified official Alpine 0.99.36-r0 APK.
Supply its exact 0.99.36 upstream archive and recorded aports build recipe;
no local fork or TLS patch is part of the selected runtime.

## Release asset contents

The same candidate manifest must bind:

- Application commit/source, LICENSE, retained Go module sources and notices,
  module sums, launcher/API/helper build instructions and pinned tool versions.
- Native upstream archives, exact aports recipes and their local patches, model/CA source and notices, the Tesseract 5.5.2 `pdf.ttf`/config source
  association (distinct from the rebuilt 5.5.4 executable), source identity checksums,
  and any generated inputs required to build the delivered covered work.
- Frontend source and build instructions/lockfile for the delivered assets.
  Build-only archives are a separate distribution scope if actually shipped;
  do not automatically bundle the SDK image or every package cache.
- Binary/native manifest, image/base digest, source archive checksums and a
  mapping from each delivered covered component to its corresponding source.
- Modification records and the retained license/NOTICE texts for the archives
  themselves, including third-party subcomponents.

The existing locally verified source archives are inputs to this process.
Refresh the source bundle once after the batch of candidate changes; do not
label the older c4a4fa7 source snapshot as source for a later changed artifact.
The clean-runner pipeline must prove that the supplied controls and declared
inputs are available without the distributor's private home cache.

## Modification and installation

Recipients may modify the covered native libraries and debug their modifications
under the applicable terms. The distribution must not impose a conflicting
restriction. The [repackaging instructions](native-replacement.md) describe how
to create a fresh manifest and executable without the distributor's key; they
do not disable verification in the original executable.

The existing GnuTLS replacement test establishes that technical mechanism only.
Use the matching library source/build controls for a semantic rebuild. For each
LGPL combination, check whether the chosen suitable shared-library mechanism
suffices or whether application/relink materials are required; do not assume
all combinations share the same answer. If GPL-3.0 User Product installation
information is applicable, provide the information necessary to install and run
the modified covered work. No locked hardware or distributor signing key is a
required part of the current personal-repackaging mechanism.

## Publication gate

Before publication, the release job must reject missing or mismatched source,
notices, checksums and component findings. Verify a recipient can download the
actual assets, check hashes and follow the build/repack instructions with the
promised access. Private repository access is not an adequate offer to a public
binary recipient. Repository visibility and recipient access remain checkpoint
C1; do not change visibility or billing silently.

R4 preparation is not complete until exact component terms and any remaining
notice/source gaps have concrete dispositions. Actual tag/image/source binding
closes in remaining plan 43-06.1 after G5; no local document can substitute for
that final check.

## Adopted material set for the next candidate

Use these materials together; do not substitute an upstream homepage for a
missing covered source input:

1. The exact project Git source (launcher, API, helper, static frontend source,
   license/notices, locks and build/repack controls), with the release revision.
2. The existing native source set: pinned upstream archives, aports recipes,
   local patches, official airscan source/recipe and helper inputs. Update
   source-managed notice/source records from the final tree once after the
   preparation batch; the older source bundle is an input, not the final offer.
3. For the container, the exact base-files/CA source inputs identified in
   distroless-source-inputs.json, the retained netbase preferred-form text files
   and notices/licenses, and the actual timezone/media data at the image digest.
4. Corresponding application/build materials for the LGPL native combinations,
   rather than relying only on the optional suitable-shared-library shortcut.
   This includes the exact helper source and native command source/build controls.
   Existing source rebuild/repack proofs apply to unchanged code/materials.

The native commands/libraries are a separate licensing scope from the independent
CGO-free API, launcher and browser assets. A SANE/airscan linking exception does
not override a GPL dependency elsewhere in the loaded native combination. Use
the GPL2-or-later permissions as needed for GPL3-compatible native combinations,
retain the LGPL options/texts and their required application materials, and keep
Apache/BSD/MIT notices for permissively licensed portions. The native combination
must not be advertised as Apache-only. Project-owned source retains Apache-2.0;
this is not a demand to relicense independent first-party source.

No proprietary optimizer or intermediate-code plugin is part of the recorded
native build controls: the Tesseract rebuild identifies GNU15.2.0 C/C++ with
normal CMake compilation, and the helper SDK pins GCC15.2.0 plus Go's normal CGO
build. Preserve those controls for the GCC Runtime Library Exception's Eligible
Compilation Process condition. Do not strip the exception from runtime sources
or treat it as permission to relicense GCC runtime libraries themselves.

Go/npm compiler caches and build SDK images are not default release assets.
The first-party project source and locks/build instructions do not require
redistributing every cached module archive or repository logo/test fixture.
If optional caches are subsequently shipped, they introduce their own source
archive scope and must retain the terms for the material actually included.
This does not permit omitting required native corresponding source or helper
application/relink materials.

The pipeline in43-02 must generate and verify this concrete material set against
the candidate. It must fail on a missing/mismatched required material.43-06 must
then verify the actual recipient access/download and release hashes after G5.
Preparation approval cannot be used as publication approval.

## Local exact-candidate materials, 2026-11-04

Parent evidence `candidate-review-20261104/source-materials.json` binds the
c304a66 source export, current native manifest, API/onefile/image identities
and three refreshed source subsets. Native sources include the official
airscan archive/recipe instead of the historical fork; all184 selected notice
bytes match the source export. Application inputs retain47 Go modules in141
proxy files after rehashing and use the current embedded assets. Frontend
inputs retain201 verified npm archives; a new network-free recipient replay
from the refreshed bundle reproduced all five candidate assets. Raw SDK
images/caches are excluded. Optional source/build-input scopes remain separate
from the required native source set and container-base materials.

This is still the local0.0.0-dev/unreleased candidate. Its API carries VCS/debug
metadata; the source README distinguishes its reference hash from the trimmed
recipient build command and retains actual build settings. A refreshed source
archive is not a new API/native rebuild, a frozen release RC, license/security
approval or proof of actual recipient access. Final base/material binding and
the hosted main/tag/publisher gates remain open.

The container-only base subset is now prepared as `scanner-base-sources.tar`
(SHA256`8afa410e…`, 471,040bytes). A stopped container of the exact current
image supplied21 checked source/image members: eight notices/license texts,
three preferred-form netbase text files, six package-status records and four
matching base-files/CA source files. The source bytes are rechecked against
retained authenticated metadata; all image-file hashes match the reviewed
base ledger. The container was never started and was removed. This closes
local base subset preparation, not final manifest/recipient/publication gates.
