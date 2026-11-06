# Preview and tagged releases

The trunk is `main`. A push to `main` is a preview input; it never constitutes a
stable release. A pushed `vMAJOR.MINOR.PATCH` tag is a release input. A tag with a
SemVer prerelease suffix, for example `v0.1.0-rc.1`, remains a prerelease and must
not advance the stable `latest` image alias. Numeric version identifiers cannot
have leading zeroes. Build-metadata (`+…`) tags are not supported.

This adopts the separation of preview and production used by
`otama-playground-astro`, adapted to the scanner's distribution: a complete
Linux amd64 executable including frontend/SANE/Tesseract/data, plus the
corresponding distroless image. Cloudflare Pages, its credentials and browser-only
hosting are unrelated to this application.

## Exact source and channels

Every build uses the event commit, including a tag's peeled commit; a tag build
must never check out the latest trunk in its place. The checkout must match that
commit and be reachable from the fetched `origin/main`. Annotated and lightweight
tags are supported. An older commit still in trunk history is a valid input, so
another push arriving during a run does not invalidate its immutable preview.

PRs run checks without publishing. Main and tag pushes run the same quality
checks and the release-ref policy. Manual source-check runs do not publish.
Checks must pass before a future publisher can run. Main/tag checks are not
cancelled by a newer push; only stale PR runs are cancelled.

When publishing is implemented, retain a commit-addressed preview for each
successful main input and a `preview` alias for the newest qualified trunk
commit. An older run finishing late must not move that alias backwards. Stable
tags publish versioned binaries/images and a GitHub Release, with `latest` only
for a stable version according to version ordering. Prerelease tags publish a
GitHub prerelease and versioned image, without changing stable aliases. An
existing published version must not be overwritten or rebuilt from a moved tag.
Deployment to the scanner and legacy retirement remain separate operations;
publishing an artifact must not automatically acquire a physical scanner lease.
Rollback selects a previous immutable version/digest after quiescing the worker;
it does not rebuild an old version or bypass storage/schema compatibility.

## Current implementation and remaining gates

`checks.yml` now routes pushes on `main` and `v*` tags to source checks. Its
release-ref job uses a full checkout with read-only permissions and no retained
credentials, then validates the exact commit, version syntax and trunk ancestry
using `scripts/check-release-ref.py`. Invalid tag versions fail rather than
being silently treated as previews. The ref checker does not publish anything.

`scripts/build-web.py --output … --ref "$GITHUB_REF" --sha "$GITHUB_SHA"` now embeds the validated channel version and exact peeled commit into the app. Identity validation runs before dependency installation. Main inputs report `preview-<12-character commit>`; tags report their exact `v…` name. Supplying only one identity argument fails. Without either argument, local/PR/manual builds retain `0.0.0-dev` and `unreleased`. The source CI uses identity arguments for push events, then compares the executable’s actual `version` response with the validated policy result.

`scripts/build-standalone.py` now supplies the packaging step for an already checked embedded app and a reviewed native manifest. It checks the static app target/build tag, compares the complete native inventory, builds the launcher in scratch, and refuses to overwrite an output directory. It neither supplies native provenance nor fetches the native input.

[Native source inputs](native-sources.md) distinguishes verified recipe/patch collection from upstream archives and source delivery. Both build SDKs now have complete APK locks and offline replay evidence; Tesseract and the SANE helper reproduce their current native file bytes. Stable native closure/provenance packaging and source delivery remain required. Source CI runs the collector’s offline safety tests only.

The source build job also runs `npm audit --audit-level=high` against the locked
frontend build dependencies and the pinned `govulncheck` v1.9.0 against `./...`
with the `webui` tag after generating embedded assets. Both are required steps:
advisory-service failures and findings at their respective thresholds fail the
job. Go source scanning uses Linux/amd64 with CGO disabled, matching the shipped API,
and the tool's default symbol-level analysis, rather
than treating a binary scan's JSON exit status as a clean result. These live
source/build checks do not cover native C libraries or the distroless OS, prove
license compliance, or authorize publication. Local snapshot-based diagnostics
remain separate evidence. Initial hosted run
[41780461741](https://github.com/otama-homelab/scanner-kiosk/actions/runs/41780461741)
passed all four jobs for commit `99990c12…`; this does not publish artifacts.

The private GitHub remote and initial hosted source checks are established.
Branch/tag protections, final native distribution/source offer,
SBOM/security/license acceptance and publishing jobs remain pending. GitHub Free
returned HTTP403 for branch protection on this private repository; changing
visibility or paying for another plan requires the operator's choice. There is
deliberately no successful-looking empty publishing job or API-only binary advertised as the plug-and-play preview. Native payload
and release gates must be implemented for the exact input before wiring registry
or GitHub Release writes. Until then, tag pushes verify a release input; they do
not produce a published release. That source check created no release tag,
registry upload or production deployment.


## Required source status

The workflow exposes `required-checks` as the aggregate status for future branch
protection. It runs even when a dependency fails: source build/tests and deployment
contracts must both succeed. On pushes, release-ref-policy must also succeed;
for PR/manual checks that inapplicable job must be skipped. Failure, cancellation,
a missing result or an unexpected skip cannot turn the aggregate green. Select
this exact job as required after the hosted repository has observed it.

Two local contract tests cover supported events and refusal of non-success
required results. Pinned actionlint validated the workflow; shellcheck/pyflakes
were unavailable in that fixture. This adds no publishing permissions or jobs,
and does not approve native artifacts, licensing, release or deployment.
The initial hosted aggregate succeeded. Actual branch protection remains
unconfigured under the private-repository plan restriction.

## Complete internal candidate build

`candidate-build` now connects the locked native builder with
`build-application.sh`: a clean Git bundle and fresh module/npm cache produce
frontend/API, an internal compatibility onefile, and separate core/Tesseract
distroless images with verified role-native dependency closures. Go,
Node and npm versions are checked; only acquisition has an external network.
Both images are saved with identities and checksums for later acceptance and
publishing, rather than rebuilding an accepted image in a publisher.

Source checks and the full candidate job remain read-only. Internal Actions
artifacts use a commit/run-attempt address and seven-day retention; failure
diagnostics are a separate artifact. No registry alias, GitHub Release,
visibility or billing setting is changed. Hosted validation of this workflow
remains pending the existing workflow-authentication checkpoint; local
fixtures alone do not establish a clean-runner success.

## Publication decision policy

`scripts/publication-policy.py` reuses the exact-checkout/trunk/tag policy and
outputs a write-free JSON plan. It does not upload, move an alias or authorize
publication. A future writer must first verify the full staged candidate and
approval gates, then obtain complete fresh service state while holding the
single publisher lock; an old saved snapshot is not sufficient for writes.

The remote-state input has three required fields: `releases` contains GitHub
release records (`tag_name`, boolean `draft` and `prerelease`), `image_tags`
contains all relevant existing registry tags, and `preview_source_sha` is the
verified source commit of the current preview alias (or null if absent). Missing
state is rejected rather than treated as an empty registry. Existing aliases
with unknown ownership must not be reset.

Preview releases use `preview-<full commit>` and versioned images use
`sha-<full commit>`, while the executable retains its validated preview version.
Only a descendant of the current preview may advance `preview`. Stable tags
use their exact version; numeric SemVer ordering determines whether `latest`
may advance. Higher drafts reserve names but do not count as qualified stable
releases. Prereleases never advance `latest`. An existing release or versioned
image is rejected, including reruns; partial publication requires explicit
recovery rather than an overwrite. Versions exceeding registry tag limits are
rejected before a writer runs. PR/manual source-check events are rejected.

Sixteen isolated tests cover the policy and CLI, including late old runs,
prereleases, moved tags, collisions, unknown remote state and numeric ordering.
The six existing ref tests and actionlint also pass. The source workflow runs
the new contract tests with its existing read-only permissions. The material
stager, service writer/serialized workflow and actual main/tag publication are
still incomplete; this policy is not a successful-looking publishing job.

## Offline material staging

`scripts/stage-candidate.py --inputs INPUT_DIRECTORY --inventory INVENTORY.json
--output NEW_DIRECTORY` copies a declared material set and checks its byte
hashes and candidate correspondence. Run it with read-only inputs, no external
network, explicit empty environment, nonroot identity and bounded resources.
The inventory format is1 with `source_sha` and `files`: role-keyed records of
`path` (relative to inputs), `bytes` and `sha256`. Required roles and output
names are fixed in the script; optional application/frontend input archives
remain explicitly separate source/build scopes.

The stager ties the actual onefile, packed/native manifests, build/application
metadata, image archive/config/inventory, three SBOMs, source/base/deployment
records and project license together. Raw advisory reports are preserved and
verified member-by-member against recorded hashes. Input links/traversal,
missing roles, wrong source/candidate/SBOM identities and changed bytes fail.
It executes no candidate, source or archived program and repeats no unchanged
source build. Existing review receipts supply the prior semantic proofs.

The output includes a content-addressed candidate identity, `manifest.json`,
`SHA256SUMS` and a receipt written last. Existing output names are never
overwritten; handled errors remove only the directory reserved by this run.
A killed run without a final receipt is incomplete. A writer must recheck the
complete manifest/checksums and separately enforce release identity, approvals
and fresh serialized service-state policy before any upload.

The local c304a66 development candidate has28 staged materials bound by
manifest SHA256`e2e814960de4bee366215ab004fb82f87fdbb3a303b2eaf8c48ee04c7a17f505`.
All staged hashes were read back successfully. Its advisory scans completed,
but the inferred-CPE threshold is unpassed; publication approval remains false.
This is not the missing formal main/tag RC. Fifteen isolated failure/identity
tests and actionlint pass; the source workflow runs these tests read-only.
The service writer/serialized publishing workflow and actual publication
remain unfinished.

## Candidate and approval preflight

The publication policy accepts `--candidate`, `--approval` and
`--approval-sha256` together to verify a full candidate before returning the
write-free plan. The expected approval hash must come from trusted operator
state, separately from the candidate artifact or an untrusted checkout. It is
not obtained from the approval document itself. Run this parsing/validation
credential-free, network-isolated and read-only with the staging resource
bounds. Nothing here fetches a service token, uploads an asset or grants a
repository permission.

`verify_staged` reuses the original file bounds/hash checks and material
correspondence without making another copy. It requires the final receipt,
canonical manifest and exact SHA256SUMS; post-stage changes fail. Preflight
also checks an approval record bound to that exact manifest/source and fixed
repository, with explicit private/public intent. Each existing gate
(required_checks, security, license, g5, recipient_access, c1) must have
`passed:true` and a SHA256 evidence identity. This validates the authenticated
record, not the underlying human review. The service writer must verify the
live visibility/access and current serialized service state separately, with
no implicit visibility or billing change.

The candidate's version and embedded revision must match the validated
main/tag policy. A development build cannot become a release by approving its
metadata. Incomplete scans also fail preflight. Raw advisory threshold results
remain unchanged, including when an explicit security review later supplies a
triage disposition; approval never erases the original report or marks its
matcher output clean.

Nineteen staging/revalidation tests, nine approval-preflight tests, sixteen
publication-policy regressions and actionlint pass. The actual local staged
manifest e2e81496… was reverified and correctly rejected with a deliberately
unapproved record; no successful approval for that candidate was minted. The
external service writer/serialized workflow is still unfinished.

## Transfer writer implementation

`scripts/write-publication.py` now consumes the preflight plan and its trusted
SHA256, rehashes all approved uploads, and uses installed `gh` and Docker
clients without rebuilding or running the candidate. Default execution is a
service-read-only dry run; it may authenticate in a private temporary Docker
configuration but creates no release, image version or alias. The temporary
configuration is removed on exit; host credential configuration is not used.

The writer verifies live repository/package visibility against approval,
remote main ancestry and peeled tag identity, the latest GitHub Actions
`required-checks` for the source, immutable version absence and alias ownership.
It recalculates alias movement from current service state rather than using a
stale preflight alias list. Unknown authorization errors are not treated as
missing versions. A new package is supported only in the explicitly approved
private scope; public packages must already have matching visibility. The
writer never changes visibility or billing.

With `--apply`, an enabled push workflow with matching repository/ref/SHA is
required. Reserve a draft, upload every bound asset without clobbering, and
compare GitHub asset digests/sizes. Recheck source/checks before making the
source-containing release accessible. Only then load the saved candidate image,
verify its image ID, push the immutable version and verify the registry digest.
Source availability therefore precedes registry binary availability. Update
owned aliases last, verify their digests, and advance GitHub stable latest only
after registry latest succeeds.

If source publication succeeds but the image fails, the source/binary release
remains accessible without advancing aliases. Its publication metadata records
that the image transfer is incomplete; it reserves the immutable version name
but does not count as a qualified higher stable version for latest ordering.
This does not relabel the actual GitHub draft/prerelease state. Existing
versions/drafts are never overwritten on retry. Inspect and explicitly recover
a partial publication; there is no automatic deletion or rollback across the
two services.

Call the writer only from the single serialized publisher concurrency group,
with cancellation disabled and minimal write permissions confined to that job.
The trusted plan hash must come from the protected read-only preflight handoff,
not an arbitrary artifact field. Archive parsing/preflight remains credential-
free; the credentialed writer hashes the sealed files without unpacking them.
The source checks currently execute only the writer contract tests. The
serialized write job and staged-plan producer/handoff are not connected yet.

Eighteen writer tests, eighteen policy tests and nine preflight tests pass
with stateful service-boundary fakes in a network-isolated sandbox. Tests cover
source-before-image ordering, upload/push failures, digest mismatches, fresh
checks/source state, visibility, immutable collisions and delayed old previews.
Public read-only probes confirmed the installed Docker client's OCI verbose
response and GitHub's asset-digest field. These are not actual private-service
uploads or hosted publisher success. C1/G5/security/license/authentication and
formal main/tag acceptance remain required; no release or image was published.


## Protected publication plan handoff (2026-11-05)

After all existing gates pass, the credential-free publication preflight accepts
`--artifact-id ID` together with its candidate/approval inputs. Preserve the exact
JSON output and its independently reviewed SHA-256 in the protected repository
variables `SCANNER_PUBLICATION_PLAN_JSON` and
`SCANNER_PUBLICATION_PLAN_SHA256`. These are public metadata, not credentials.
`SCANNER_PUBLISH_ENABLED` remains disabled until the existing publication gates
and C1 are resolved; setting that flag alone does not authorize a candidate.

The serialized publisher imports the plan only when its protected hash and event
source match. It checks the selected immutable artifact's repository, push run,
source SHA, name and expiry before downloading. A protected plan may select an
approved earlier attempt within the same run, so a rerun does not silently switch
to newly generated materials. Another run or source is rejected. The writer then
revalidates the manifest, every upload and fresh service state as before.

No approved plan or external publication was created while implementing this
handoff. Actual recipient access, visibility/protection, tag promotion and service
writes remain 43-06.1 work after G5. The stage manifest's original advisory and
approval fields are never rewritten by importing a plan.


## Automatic development image previews

Main pushes now publish source-backed development images after all required
checks. This is separate from formal version-tag approval: G5 remains incomplete
until its real acceptance evidence exists. No development preview changes
`latest`. The publisher still uses the single serialized lock, fresh remote
ancestry/check state, immutable SHA tags, upload digest checks and alias-last
ordering. PRs and manual source checks cannot enter this publication path.

The same-run staged artifact is selected by source/run/repository identity,
including a valid earlier run attempt. Credential-free, nonroot, network-disabled
preflight revalidates the staged materials, the reviewed native manifest and the
exact advisory ID/severity/package/version records in
`scripts/preview-advisory-review.json`. Changed native bytes and unreviewed
findings require a new review. The current Go source also has to pass the
existing embedded-frontend govulncheck before `required-checks` succeeds.

Corresponding source and raw evidence become accessible in a source-only
`preview-source-<full-sha>` prerelease before the registry image. The standalone
binary and both image tarballs are excluded from those prerelease assets; their identities
remain in the staged manifest/checksums and the complete CI artifact. Image push
uses the exact accepted image ID and its OCI repository label, then verifies the
registry config/digest and package repository link for both variants. Only then may
`preview` and `tesseract-preview` move. Both aliases share a monotonic source
frontier, so a partial prior update cannot permit an older candidate to roll back
either variant. Both immutable tags use the same package: `sha-<sha>` and
`tesseract-sha-<sha>`.
The raw advisory threshold remains recorded even when its byte-bound findings
have the retained disposition; no G5 or stable security approval is fabricated.

GHCR initially creates a private package. Its owner controls public visibility
separately in package settings. Automatic publication preserves that policy and
reports actual package visibility. Formal version publishing retains its existing
protected plan and G5/security/license/access gates.
